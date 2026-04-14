# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/uring)](https://goreportcard.com/report/github.com/hayabusa-cloud/uring)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Package Go pour l'interface noyau `io_uring` sous Linux 6.18+.

Langue: [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | [日本語](./README.ja.md) | **Français**

## Vue d'ensemble

`uring` est le package de l'espace de travail qui expose l'interface noyau Linux `io_uring`. Il crée et démarre les
rings, prépare les SQE, décode les CQE, achemine l'identité de soumission via `user_data`, et fournit l'enregistrement
de buffers, les opérations multishot ainsi que les primitives de mise en place des listeners.

`uring` repose sur une conception à interface explicite : la mécanique côté noyau et les faits de complétion observables
se situent au bord de l'API, tandis que la politique et la composition relèvent des couches supérieures.

Les surfaces principales sont :

- `Uring`, le handle du ring actif et son jeu d'opérations
- `SQEContext`, l'identité de soumission acheminée dans `user_data`
- `CQEView`, la vue de complétion empruntée renvoyée par `Wait`
- la fourniture de buffers, via buffers enregistrés et groupes de buffers multi-tailles

## Installation

`uring` nécessite un noyau Linux 6.18 ou ultérieur. Vérifiez d'abord la version du noyau en cours d'exécution :

```bash
uname -r
```

Sous Debian 13, le noyau de la branche stable peut rester en deçà de ce seuil. Consultez la section sur la mise à niveau
du noyau Debian 13 ci-dessous si vous avez besoin du noyau Debian le plus récent satisfaisant l'exigence 6.18.

```bash
go get code.hybscloud.com/uring
```

### Mise à niveau du noyau Debian 13

La branche stable de Debian 13 fournit le noyau 6.12. La suite `trixie-backports` met à disposition un noyau 6.18+
empaqueté par Debian. Consultez [SETUP.md](./SETUP.md) pour la marche à suivre détaillée.

### Dépannage

La création du ring peut renvoyer `ENOMEM`, `EPERM` ou `ENOSYS` selon les limites memlock, la configuration sysctl ou le
support noyau. Les runtimes de conteneurs bloquent les appels système `io_uring` par défaut.
Consultez [SETUP.md](./SETUP.md) pour le diagnostic et la résolution.

## Cycle de vie du ring

`New` renvoie un ring non démarré. Il faut appeler `Start` avant de soumettre des opérations. `Start` enregistre les
ressources du ring et l'active ; `New`, de son côté, construit les pools de contexte de manière anticipée. Sous Linux,
`uring` présuppose la base fixe 6.18+ et ne conserve aucune branche de repli pour les noyaux antérieurs.

```go
ring, err := uring.New(func(o *uring.Options) {
    o.Entries = uring.EntriesMedium
})
if err != nil {
    return err
}

if err := ring.Start(); err != nil {
    return err
}

cqes := make([]uring.CQEView, 64)
n, err := ring.Wait(cqes)
if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
    return err
}

for i := range n {
    cqe := cqes[i]
    if cqe.Res < 0 {
        return fmt.Errorf("completion failed: op=%d fd=%d res=%d", cqe.Op(), cqe.FD(), cqe.Res)
    }
    fmt.Printf("completed op=%d on fd=%d with res=%d\n", cqe.Op(), cqe.FD(), cqe.Res)
}
```

`Wait` purge les soumissions en attente avant de récupérer les complétions. Sur un ring mono-émetteur, il émet aussi
l'appel enter vers le noyau, nécessaire pour que le deferred task work progresse une fois la SQ vidée ; l'appelant doit
sérialiser `Wait`/`enter` avec les opérations de submit-state. `iox.ErrWouldBlock` signale qu'aucune complétion n'est
observable à l'interface courante. Cette erreur est définie dans `code.hybscloud.com/iox`.

`Start` et `Stop` constituent la paire de cycle de vie du ring. `Stop` est
idempotent et rend le ring définitivement inutilisable ; on ne doit donc
l'appeler qu'après avoir drainé toutes les opérations en vol, récupéré les CQE
en attente et arrêté les abonnements multishot encore actifs.

## Types et opérations

| Type | Rôle |
|------|------|
| `Uring` | Initialisation du ring, soumission, récupération des complétions, et méthodes d'opération |
| `Options` | Entrées du ring, budget de buffers enregistrés, échelle des groupes de buffers et visibilité des complétions |
| `SQEContext` | Identité de soumission compacte stockée dans `user_data` |
| `CQEView` | Enregistrement de complétion emprunté avec accesseurs de contexte décodé |
| `ListenerOp` | Handle d'une opération de création de listener avec FD et helpers accept |
| `BundleIterator` | Itère sur les buffers consommés lors d'une réception bundle |
| `IncrementalReceiver` | Gère les réceptions incrémentales de buffer-ring (`IOU_PBUF_RING_INC`) |
| `ZCTracker` | Suit le cycle de vie à deux CQE de l'envoi zero-copy |
| `ContextPools` | Pools pour contextes de soumission indirects et étendus |
| `ZCRXReceiver` | Cycle de vie de réception zero-copy sur une file RX de NIC |
| `ZCRXConfig` | Configuration d'une instance de réception ZCRX |
| `ZCRXHandler` | Interface de rappel pour données, erreurs et arrêt ZCRX |
| `ZCRXBuffer` | Vue de réception zero-copy livrée, avec remplissage par le kernel à la libération |

Opérations :

| Domaine | Méthodes |
|---------|----------|
| Socket | `TCP4Socket`, `TCP6Socket`, `UDP4Socket`, `UDP6Socket`, `UDPLITE4Socket`, `UDPLITE6Socket`, `SCTP4Socket`, `SCTP6Socket`, `UnixSocket`, `SocketRaw`, plus variantes `*Direct` |
| Connexion | `Bind`, `Listen`, `Accept`, `AcceptDirect`, `Connect`, `Shutdown` |
| Socket I/O | `Receive`, `Send`, `RecvMsg`, `SendMsg`, `ReceiveBundle`, `ReceiveZeroCopy`, `Multicast`, `MulticastZeroCopy` |
| Multishot | `AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot`, `SubmitReceiveBundleMultishot` |
| Fichier I/O | `Read`, `Write`, `ReadV`, `WriteV`, `ReadFixed`, `WriteFixed`, `ReadvFixed`, `WritevFixed` |
| Gestion fich. | `OpenAt`, `Close`, `Sync`, `Fallocate`, `FTruncate`, `Statx`, `RenameAt`, `UnlinkAt`, `MkdirAt`, `SymlinkAt`, `LinkAt` |
| Xattr | `FGetXattr`, `FSetXattr`, `GetXattr`, `SetXattr` |
| Transfert | `Splice`, `Tee`, `Pipe`, `SyncFileRange`, `FileAdvise` |
| Timeout | `Timeout`, `TimeoutRemove`, `TimeoutUpdate`, `LinkTimeout` |
| Annulation | `AsyncCancel`, `AsyncCancelFD`, `AsyncCancelOpcode`, `AsyncCancelAny`, `AsyncCancelAll` |
| Poll | `PollAdd`, `PollRemove`, `PollUpdate`, `PollAddLevel`, `PollAddMultishot`, `PollAddMultishotLevel` |
| Async | `EpollWait`, `FutexWait`, `FutexWake`, `FutexWaitV`, `Waitid` |
| Ring msg | `MsgRing`, `MsgRingFD`, `FixedFdInstall`, `FilesUpdate` |
| Cmd | `UringCmd`, `UringCmd128`, `Nop`, `Nop128` |

`Nop128` et `UringCmd128` nécessitent un ring créé avec `Options.SQE128` ; le noyau doit en outre annoncer la prise en
charge des opcodes correspondants, faute de quoi ces méthodes renvoient `ErrNotSupported`.

`Uring.Close` soumet un `IORING_OP_CLOSE` pour un descripteur de fichier cible. Il ne s'agit pas d'une méthode de
destruction du ring.

## Transport du contexte

`SQEContext` est le jeton d'identité principal de `uring`. En mode direct, il encode l'opcode, les flags SQE,
l'identifiant de groupe de buffers et le descripteur de fichier dans une seule valeur de 64 bits.

```go
sqeCtx := uring.ForFD(fd).
    WithOp(uring.IORING_OP_RECV).
    WithBufGroup(groupID)
```

Les trois modes de contexte sont :

| Mode     | Représentation                  | Usage typique                                                    |
|----------|---------------------------------|------------------------------------------------------------------|
| Direct   | Charge utile inline sur 64 bits | Chemin courant de soumission et de récupération, zéro allocation |
| Indirect | Pointeur vers `IndirectSQE`     | Lorsque 64 bits ne suffisent pas pour le SQE complet             |
| Extended | Pointeur vers `ExtSQE`          | SQE complet plus 64 octets de données utilisateur                |

Sur le chemin courant, on part de `ForFD` ou `PackDirect` en n'ajoutant que les bits que l'on souhaite retrouver à la
complétion. `WithFlags` remplace la totalité des flags : il convient donc de calculer les unions avant l'appel.

Lorsqu'on a besoin de métadonnées contrôlées par l'appelant au-delà du layout direct sur 64 bits, on emprunte un
`ExtSQE`, on écrit dans son champ `UserData` via `Ctx*Of` ou `ViewCtx*`, puis on le ré-encode en `SQEContext`. Préférez
des charges scalaires à cet endroit. Si un overlay brut ou une vue typée y stocke des pointeurs Go, des interfaces, des
valeurs func, des slices, des strings, des maps, des chans ou des structs qui en contiennent, conservez les racines
vivantes en dehors de `UserData`, car le GC ne trace pas ces octets bruts.

```go
ext := ring.ExtSQE()
meta := uring.CtxV1Of(ext)
meta.Val1 = requestSeq

sqeCtx := uring.PackExtended(ext)
fmt.Printf("sqe context mode=%d seq=%d\n", sqeCtx.Mode(), meta.Val1)
```

`NewContextPools` renvoie des pools prêts à l'emploi. N'appelez `Reset` qu'après avoir restitué tous les contextes
empruntés et uniquement si vous souhaitez réutiliser l'ensemble de pools.

### Dispatch des complétions avec `CQEView`

`uring` n'expose pas de type dédié au contexte de complétion. Le dispatch des complétions passe par `CQEView` ; on
appelle `cqe.Context()` pour récupérer le jeton de soumission d'origine.

```go
cqes := make([]uring.CQEView, 64)

n, err := ring.Wait(cqes)
if err != nil && !errors.Is(err, iox.ErrWouldBlock) {
    return err
}

for i := 0; i < n; i++ {
    cqe := cqes[i]
    if cqe.Res < 0 {
        return fmt.Errorf("completion failed: op=%d fd=%d res=%d", cqe.Op(), cqe.FD(), cqe.Res)
    }

    switch cqe.Op() {
    case uring.IORING_OP_ACCEPT:
        fmt.Printf("accepted fd=%d\n", cqe.Res)
    case uring.IORING_OP_RECV:
        if cqe.HasBuffer() {
            fmt.Printf("buffer id=%d\n", cqe.BufID())
        }
        if cqe.Extended() {
            seq := uring.CtxV1Of(cqe.ExtSQE()).Val1
            fmt.Printf("request seq=%d\n", seq)
        }
    }
}
```

À la complétion, `CQEView` décode le mode de contexte à la demande. `CQEView`, `IndirectSQE`, `ExtSQE` et les buffers
empruntés ne doivent pas survivre au-delà de leur durée de vie documentée.

## Fourniture de buffers

`uring` propose deux stratégies de fourniture des buffers de réception :

- des buffers fournis de taille fixe via `ReadBufferSize` et `ReadBufferNum`
- des groupes de buffers multi-tailles via `MultiSizeBuffer`

Pour la plupart des systèmes, les helpers de configuration offrent un point d'entrée direct :

```go
opts := uring.OptionsForSystem(uring.MachineMemory4GB)
ring, err := uring.New(func(o *uring.Options) {
    *o = opts
})
```

On utilise `OptionsForBudget` pour partir d'un budget mémoire explicite, et `BufferConfigForBudget` pour inspecter la
répartition par niveaux retenue pour ce budget.

Les buffers enregistrés nécessitent de la mémoire épinglée. En cas d'échec de l'enregistrement de buffers volumineux,
augmentez `RLIMIT_MEMLOCK` ou réduisez le budget mémoire.

## Opérations multishot et listener

`AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot` et `SubmitReceiveBundleMultishot` soumettent des opérations socket multishot.

La politique de routage des CQE reste hors du package. La mise en place du listener progresse via `DecodeListenerCQE`,
`PrepareListenerBind`, `PrepareListenerListen` et `SetListenerReady` ; c'est l'appelant qui décide de la distribution
des complétions et de l'arrêt de la chaîne.

## Architecture de l'implémentation

L'implémentation se structure autour des couches suivantes :

1. `New` construit un ring noyau désactivé, crée les pools de contexte et détermine la stratégie de buffers.
2. `Start` enregistre les buffers et active le ring conformément à la base fixe Linux 6.18+.
3. Les méthodes d'opération publient l'intention en écrivant des SQE.
4. `Wait` purge les soumissions et renvoie des vues CQE empruntées.
5. Les couches supérieures décident de l'ordonnancement, des reprises, du parking et de l'orchestration.

De cette manière, `uring` reste focalisé sur la mécanique côté noyau tout en préservant la sémantique des complétions au
travers de l'interface.

## Patrons pour la couche applicative

`uring` expose les mécanismes tournés vers le noyau ; l'ordonnancement, les tentatives de reprise, le suivi de
connexions et l'interprétation du protocole relèvent des couches supérieures. Les patrons ci-dessous illustrent les
façons courantes de structurer cette couche supérieure.

### Boucle d'événements propriétaire du ring

En mode single-issuer (le mode par défaut), une seule goroutine sérialise toutes les opérations côté submit. Une boucle
classique soumet le travail en attente, applique un `iox.Backoff` détenu par l'appelant quand `Wait` ne signale aucun
progrès observable, puis distribue les complétions :

```go
func runLoop(ring *uring.Uring, stop <-chan struct{}) error {
    cqes := make([]uring.CQEView, 64)
    var backoff iox.Backoff
    for {
        select {
        case <-stop:
            return nil
        default:
        }

        n, err := ring.Wait(cqes)
        if errors.Is(err, iox.ErrWouldBlock) {
            backoff.Wait()
            continue
        }
        if err != nil {
            return err
        }
        if n == 0 {
            backoff.Wait()
            continue
        }

        backoff.Reset()
        for i := range n {
            dispatch(ring, cqes[i])
        }
    }
}
```

Les méthodes du ring, dont `Send`, `Receive`, `AcceptMultishot` et `Wait`, s'exécutent sur cette goroutine. Le travail
provenant d'autres goroutines entre dans la boucle via un canal ou une file lock-free ; on n'appelle pas directement les
méthodes du ring depuis l'extérieur. `iox.Backoff` reste côté appelant : on appelle `backoff.Wait()` quand `Wait`
renvoie `iox.ErrWouldBlock` ou ne récupère aucun CQE, puis `backoff.Reset()` après tout lot avec `n > 0`.

### Cycle de vie des souscriptions multishot

Une opération multishot produit un flux de CQEs jusqu'à ce que le noyau envoie un CQE final (sans `IORING_CQE_F_MORE`).
La couche framework suit les souscriptions et gère la re-soumission :

```go
handler := uring.NewMultishotSubscriber().
    OnStep(func(step uring.MultishotStep) uring.MultishotAction {
        if step.Err != nil {
            return uring.MultishotStop
        }
        connFD := iofd.FD(step.CQE.Res)
        registerConnection(connFD)
        return uring.MultishotContinue
    }).
    OnStop(func(err error, cancelled bool) {
        if !cancelled {
            resubscribeAccept()
        }
    })

_, err := ring.AcceptMultishot(acceptCtx, handler.Handler())
```

`OnMultishotStep` observe chaque complétion ; on renvoie `MultishotContinue` pour maintenir le flux ou `MultishotStop`
pour demander l'annulation. `OnMultishotStop` s'exécute une seule fois à l'état terminal. On l'utilise pour le nettoyage
et la re-souscription conditionnelle.

### État par connexion via des contextes typés

Les contextes étendus transportent les références par connexion tout au long du cycle submit → complete, sans recourir à
une table de correspondance globale :

```go
type ConnState struct {
    Addr    netip.AddrPort
    Created int64
}

ext := ring.ExtSQE()
ctx := uring.Ctx1V1Of[ConnState](ext)
ctx.Ref1 = connState
ctx.Val1 = sequenceNumber

sqeCtx := uring.PackExtended(ext)
if err := ring.Send(sqeCtx, &fd, payload); err != nil {
    ring.PutExtSQE(ext)
    return err
}
```

Au moment de la complétion, on récupère l'état par la même vue typée :

```go
ext := cqe.ExtSQE()
ctx := uring.Ctx1V1Of[ConnState](ext)
conn := ctx.Ref1
seq := ctx.Val1
ring.PutExtSQE(ext)
```

On maintient les racines de pointeurs Go actives accessibles en dehors de `UserData`. Le GC ne trace pas ces octets
bruts. Le jeu de racines sidecar rattaché à chaque slot `ExtSQE` s'en charge pour les protocoles internes multishot et
listener, mais le code framework qui place des refs typés doit les garder accessibles de manière indépendante.

### Composition de délais

`LinkTimeout` attache un délai au SQE précédent via une chaîne `IOSQE_IO_LINK`. L'opération et le timeout entrent en
concurrence : l'un aboutit, l'autre est annulé.

```go
recvCtx := uring.ForFD(fd).
    WithOp(uring.IORING_OP_RECV).
    WithBufGroup(group)

if err := ring.Receive(recvCtx, &fd, nil, uring.WithFlags(uring.IOSQE_IO_LINK)); err != nil {
    return err
}

timeoutCtx := uring.PackDirect(uring.IORING_OP_LINK_TIMEOUT, 0, 0, 0)
if err := ring.LinkTimeout(timeoutCtx, 5*time.Second); err != nil {
    return err
}
```

La couche framework gère les deux issues : une réception réussie annule le timeout, et un timeout déclenché annule la
réception. Les deux produisent des CQEs que la boucle de distribution doit observer.

## Parcours TCP courants

Les parcours les plus concis, à lire en regard des tests :

| Scénario | API principales | Référence |
|----------|-----------------|-----------|
| Serveur echo | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| Client | `TCP4Socket`, `Connect`, `Send`, `Receive` | `socket_integration_test.go` |

### Serveur echo TCP

On utilise `ListenerManager` pour que le package prépare la chaîne socket → bind → listen, puis on démarre le multishot
accept et le multishot receive sur les FD de connexion actifs.

```go
pool := uring.NewContextPools(32)
manager := uring.NewListenerManager(ring, pool)

listenerOp, err := manager.ListenTCP4(addr, 128, listenerHandler)
if err != nil {
    return err
}

acceptSub, err := listenerOp.AcceptMultishot(acceptHandler)
if err != nil {
    return err
}
defer acceptSub.Cancel()

recvCtx := uring.ForFD(clientFD).WithBufGroup(readGroup)
recvSub, err := ring.ReceiveMultishot(recvCtx, recvHandler)
if err != nil {
    return err
}
defer recvSub.Cancel()
```

`listener_example_test.go` couvre la mise en place du listener avec accept multishot, `examples/multishot_test.go`
détaille les CQE côté handler pour le multishot receive, et `examples/echo_test.go` illustre le parcours echo loopback
complet.

### Client TCP

On crée d'abord le socket, on attend la complétion `IORING_OP_SOCKET`, puis on convertit le FD renvoyé en `iofd.FD` pour
l'utiliser avec `Connect`, `Send` et `Receive`.

```go
clientCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
if err := ring.TCP4Socket(clientCtx); err != nil {
    return err
}

clientFD := iofd.NewFD(int(socketCQE.Res))

connectCtx := uring.PackDirect(uring.IORING_OP_CONNECT, 0, 0, int32(clientFD))
if err := ring.Connect(connectCtx, remoteAddr); err != nil {
    return err
}

sendCtx := uring.PackDirect(uring.IORING_OP_SEND, 0, 0, int32(clientFD))
if err := ring.Send(sendCtx, &clientFD, payload); err != nil {
    return err
}

recvCtx := uring.PackDirect(uring.IORING_OP_RECV, 0, 0, int32(clientFD))
if err := ring.Receive(recvCtx, &clientFD, buf); err != nil {
    return err
}
```

On réutilise la boucle `Wait` décrite dans la section cycle de vie du ring après chaque soumission pour observer la
complétion correspondante. Le fichier `socket_integration_test.go` couvre le flux connect/send côté client TCP.

## Réception zero-copy (ZCRX)

`ZCRXReceiver` gère la réception zero-copy depuis une file RX matérielle de NIC via `io_uring`.

`NewZCRXReceiver` nécessite un ring créé avec des CQE de 32 octets (`IORING_SETUP_CQE32`). La surface `Options` actuelle
n'expose pas ce drapeau de configuration ; par conséquent, les rings créés via le chemin standard de `New` conduisent ce
constructeur à renvoyer `ErrNotSupported`.

### Cycle de vie

1. Sur un ring créé avec des CQE de 32 octets, on crée le récepteur via `NewZCRXReceiver`. Le constructeur enregistre la
   file d'interface ZCRX, mappe la zone de remplissage et prépare le refill ring.
2. Appelez `Start` pour soumettre l'opération étendue `RECV_ZC` sur le ring.
3. Sur le chemin de dispatch des CQE, les complétions ZCRX sont acheminées vers le `ZCRXHandler` :
    - `OnData` livre un `ZCRXBuffer` pointant vers la zone mappée par la NIC. On appelle `Release` une fois le
      traitement terminé pour restituer le slot au noyau. Renvoyer `false` demande un arrêt au mieux.
    - `OnError` livre les erreurs CQE. Renvoyer `false` demande un arrêt au mieux.
   - `OnStopped` s'exécute une fois lors du retrait terminal avant que l'état ne devienne `Stopped`.
4. On appelle `Stop` pour soumettre un async cancel. Le récepteur transite par `Stopping` → `Retiring` → `Stopped`.
5. On interroge `Stopped` jusqu'à ce qu'il renvoie `true`, on arrête le ring propriétaire, puis on appelle `Close` pour
   libérer la zone mappée et le mapping du refill ring.

### Machine à états

```
Idle → Active → Stopping → Retiring → Stopped
```

`Stop` revient à l'état `Active` si la soumission d'annulation échoue. `Close` est idempotent.

### Contrat du handler

- `OnData` et `OnError` sont appelés en série depuis la goroutine de dispatch CQE.
- `Release` est mono-producteur : il ne doit être appelé que depuis la goroutine de dispatch.
- `Stop` ne doit être appelé que lorsqu'il est garanti non concurrent avec le dispatch CQE. Il s'agit d'un contrat de
  sérialisation côté appelant.

## Exemples

Les tests d'exemple dans `uring/examples/` illustrent l'API en situation concrète.

- `multishot_test.go`, accept multishot, réception multishot et arrêt d'abonnement
- `file_io_test.go`, lectures, écritures et traitement par lots
- `fixed_buffers_test.go`, buffers enregistrés et I/O sur buffers fixes
- `vectored_io_test.go`, opérations de lecture et écriture vectorisées
- `splice_tee_test.go`, transfert de données zero-copy avec splice et tee
- `zerocopy_test.go`, chemins d'envoi zero-copy et suivi des complétions
- `poll_test.go`, flux de disponibilité par poll
- `buffer_ring_test.go`, fourniture de buffer rings et groupes de buffers multi-tailles
- `context_test.go`, flux `SQEContext` direct, indirect et extended, plus accès via `CQEView`
- `echo_test.go`, parcours serveur echo TCP et ping-pong UDP
- `timeout_test.go`, opérations de timeout et linked-timeout

Au niveau du package, `listener_example_test.go` couvre la création de listener et l'accept multishot, tandis que
`socket_integration_test.go` couvre le flux client TCP connect/send.

## Notes opérationnelles

- Activez `NotifySucceed` pour obtenir un CQE visible à chaque opération réussie.
- `ring.Features` indique le nombre effectif d'entrées SQ et CQ, la largeur des slots SQE, ainsi que l'ordre des octets
  utilisé par le package pour interpréter `user_data`.
- Laissez `MultiIssuers` désactivé pour la configuration mono-émetteur par défaut (`SINGLE_ISSUER` + `DEFER_TASKRUN`),
  dans laquelle un seul chemin d'exécution de l'appelant sérialise les opérations de submit-state (`submit`, `Wait`/
  `enter`, `Stop` et resize). N'activez ce drapeau que lorsque plusieurs goroutines nécessitent une soumission
  concurrente ou un enter côté wait ; cela bascule le ring vers la configuration de soumission partagée `COOP_TASKRUN`.
- `EpollWait` exige que `timeout` vaille `0` ; utilisez `LinkTimeout` si vous avez besoin d'une échéance.
- Les vues de complétion empruntées et les contextes issus des pools doivent être libérés ou abandonnés sans délai.
- `ListenerOp.Close` ferme le FD du listener immédiatement. Si un CQE de mise en place est encore en attente, drainez-le
  d'abord, puis rappelez `Close` pour restituer le `ExtSQE` emprunté au pool.

## Support de plateforme

`uring` cible Go 1.26+ et Linux 6.18+ pour le chemin réel adossé au noyau.
La plupart des fichiers d'implémentation et des tests d'exemple portent la
directive `//go:build linux`. Les fichiers Darwin fournissent uniquement des
stubs de compilation pour la surface partagée ; les capacités propres à Linux
restent propres à Linux et ne modifient en rien la base d'exécution Linux
décrite ci-dessus.

## Licence

MIT, voir [LICENSE](./LICENSE).

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)

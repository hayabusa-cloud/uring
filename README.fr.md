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
de tampons, les opérations multishot ainsi que les primitives de mise en place des écouteurs.

`uring` repose sur une conception à interface explicite : la mécanique côté noyau et les faits de complétion observables
se situent au bord de l'API, tandis que la politique et la composition relèvent des couches supérieures. Le code
d'exécution côté appelant possède la corrélation des complétions, les tentatives de reprise et l'attente progressive, le
routage des gestionnaires et des sessions, le cycle de vie des connexions et la libération terminale des ressources.

Les surfaces principales sont :

- `Uring`, le handle du ring actif et son jeu d'opérations
- `SQEContext`, l'identité de soumission acheminée dans `user_data`
- `CQEView`, la vue de complétion empruntée renvoyée par `Wait`
- la fourniture de tampons, via tampons enregistrés et groupes de tampons multi-tailles

## Installation

`uring` nécessite un noyau Linux 6.18 ou ultérieur. Vérifiez d'abord la version du noyau en cours d'exécution :

```bash
uname -r
```

`uring` suppose la base 6.18+ et ne contient aucune branche de repli pour les noyaux plus anciens. Démarrez un noyau
pris en charge
plutôt que d'attendre des branches de compatibilité dans ce package.

Sous Debian 13, le noyau de la branche stable peut rester en deçà de ce seuil. Consultez la section sur la mise à niveau
du noyau Debian 13 ci-dessous si vous avez besoin d'un noyau Debian plus récent satisfaisant l'exigence 6.18.

```bash
go get code.hybscloud.com/uring
```

### Mise à niveau du noyau Debian 13

La branche stable de Debian 13 fournit le noyau 6.12. La suite `trixie-backports` met à disposition un noyau 6.18+
empaqueté par Debian. Consultez [SETUP.md](./SETUP.md) pour la marche à suivre détaillée.

### Dépannage

La création du ring peut renvoyer `ENOMEM`, `EPERM` ou `ENOSYS` selon les limites memlock, la configuration sysctl ou le
support noyau. Les environnements d'exécution de conteneurs bloquent les appels système `io_uring` par défaut.
Consultez [SETUP.md](./SETUP.md) pour le diagnostic et la résolution.

## Cycle de vie du ring

`New` renvoie un ring non démarré. Il faut appeler `Start` avant de soumettre des opérations. `Start` enregistre les
ressources du ring et l'active ; `New`, de son côté, construit les pools de contexte de manière anticipée. L'exemple
ci-dessous soumet une lecture de fichier, attend le CQE correspondant et utilise `iox.Classify` afin que `ErrWouldBlock`
reste un résultat sémantique d'absence de progrès, et non une défaillance.

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
defer ring.Stop()

fd := iofd.NewFD(int(file.Fd()))
buf := make([]byte, 4096)
ctx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, 0).WithFD(fd)
if err := ring.Read(ctx, buf); err != nil {
    return err
}

cqes := make([]uring.CQEView, 64)
var backoff iox.Backoff

for {
    n, err := ring.Wait(cqes)
    switch iox.Classify(err) {
    case iox.OutcomeWouldBlock:
        backoff.Wait()
        continue
    case iox.OutcomeFailure:
        return err
    }
    if n == 0 {
        backoff.Wait()
        continue
    }

    backoff.Reset()
    for i := range n {
        cqe := cqes[i]
		if cqe.Op() != uring.IORING_OP_READ || cqe.FD() != fd {
            continue
        }
        if cqe.Res < 0 {
            return fmt.Errorf("uring read failed: res=%d", cqe.Res)
        }
        handle(buf[:int(cqe.Res)])
        return nil
    }
}
```

`Wait` purge les soumissions en attente avant de récupérer les complétions. Sur un ring mono-émetteur, il émet aussi
l'entrée noyau nécessaire pour que le travail différé progresse une fois la SQ vidée ; l'appelant doit
sérialiser `Wait`/`enter` avec les opérations d'état de soumission. `iox.OutcomeWouldBlock` signale qu'aucune complétion
n'est observable à l'interface courante.

`Start` et `Stop` constituent la paire de cycle de vie du ring. `Stop` est idempotent et rend le ring définitivement
inutilisable ; on ne doit donc l'appeler qu'après avoir drainé toutes les opérations en vol, récupéré les CQE en attente
et arrêté les abonnements multishot encore actifs.

## Types et opérations

| Type                  | Rôle                                                                                                         |
|-----------------------|--------------------------------------------------------------------------------------------------------------|
| `Uring`               | Initialisation du ring, soumission, récupération des complétions, et méthodes d'opération                    |
| `Options`             | Entrées du ring, budget de tampons enregistrés, échelle des groupes de tampons et visibilité des complétions |
| `SQEContext`          | Identité de soumission compacte stockée dans `user_data`                                                     |
| `CQEView`             | Enregistrement de complétion emprunté avec accesseurs de contexte décodé                                     |
| `ListenerOp`          | Gestionnaire d'une opération de création d'écouteur avec FD et auxiliaires accept                            |
| `BundleIterator`      | Itère sur les tampons consommés lors d'une réception groupée                                                 |
| `IncrementalReceiver` | Gère les réceptions incrémentales de buffer-ring (`IOU_PBUF_RING_INC`)                                       |
| `ZCTracker`           | Suit le cycle de vie à deux CQE de l'envoi sans copie                                                        |
| `ContextPools`        | Pools pour contextes de soumission indirects et étendus                                                      |
| `ZCRXReceiver`        | Cycle de vie de réception sans copie sur une file RX de NIC                                                  |
| `ZCRXConfig`          | Configuration d'une instance de réception ZCRX                                                               |
| `ZCRXHandler`         | Interface de rappel pour données, erreurs et arrêt ZCRX                                                      |
| `ZCRXBuffer`          | Vue de réception sans copie livrée, avec remplissage par le noyau à la libération                            |

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

`SQEContext` est le jeton d'identité principal de `uring`. En mode direct, il encode l'opcode, les fanions SQE,
l'identifiant de groupe de tampons et le descripteur de fichier dans une seule valeur de 64 bits.

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
complétion. `WithFlags` remplace la totalité des fanions : il convient donc de calculer les unions avant l'appel.

Lorsqu'on a besoin de métadonnées contrôlées par l'appelant au-delà du layout direct sur 64 bits, on emprunte un
`ExtSQE`, on écrit dans son champ `UserData` via `Ctx*Of` ou `ViewCtx*`, puis on le ré-encode en `SQEContext`. Préférez
des charges scalaires à cet endroit. Si une superposition brute ou une vue typée y stocke des pointeurs Go, des
interfaces, des valeurs de fonction, des tranches, des chaînes, des tables de hachage, des canaux ou des structures qui
en contiennent, conservez les racines vivantes en dehors de `UserData`, car le GC ne trace pas ces octets bruts.

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

`uring` n'expose pas de type dédié au contexte de complétion. La distribution des complétions passe par `CQEView` ; on
appelle `cqe.Context()` pour récupérer le jeton de soumission d'origine.

```go
cqes := make([]uring.CQEView, 64)

n, err := ring.Wait(cqes)
switch iox.Classify(err) {
case iox.OutcomeWouldBlock:
    return iox.ErrWouldBlock
case iox.OutcomeFailure:
    return err
}
if n == 0 {
    return iox.ErrWouldBlock
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

À la complétion, `CQEView` décode le mode de contexte à la demande. `CQEView`, `IndirectSQE`, `ExtSQE` et les tampons
empruntés ne doivent pas survivre au-delà de leur durée de vie documentée.

## Fourniture de tampons

`uring` propose trois chemins pratiques pour les tampons. Les tampons enregistrés sont épinglés au démarrage du ring et
servent aux I/O fichier sur tampon fixe. Les anneaux de tampons fournis laissent le noyau choisir un tampon de réception
et
renvoyer son ID dans le CQE. Les réceptions groupées consomment une plage logique contiguë de tampons fournis et
l'exposent via `BundleIterator`.

- des tampons fournis de taille fixe via `ReadBufferSize` et `ReadBufferNum`
- des groupes de tampons multi-tailles via `MultiSizeBuffer`
- des tampons fixes enregistrés via `LockedBufferMem`, `RegisteredBuffer`, `ReadFixed` et `WriteFixed`

Pour la plupart des systèmes, les fonctions auxiliaires de configuration offrent un point d'entrée direct :

```go
opts := uring.OptionsForSystem(uring.MachineMemory4GB)
ring, err := uring.New(func(o *uring.Options) {
    *o = opts
})
```

On utilise `OptionsForBudget` pour partir d'un budget mémoire explicite, et `BufferConfigForBudget` pour inspecter la
répartition par niveaux retenue pour ce budget :

```go
cfg, scale := uring.BufferConfigForBudget(256 * uring.MiB)
fmt.Printf("buffer tiers=%+v scale=%d\n", cfg, scale)
```

L'I/O sur tampon fixe utilise un tampon enregistré par index. La tranche renvoyée appartient au ring ; gardez-la vivante
jusqu'à la complétion de l'opération fixe :

```go
buf := ring.RegisteredBuffer(0)
copy(buf, payload)

ctx := uring.PackDirect(uring.IORING_OP_WRITE_FIXED, 0, 0, int32(file.Fd()))
if err := ring.WriteFixed(ctx, 0, len(payload)); err != nil {
    return err
}
```

Pour recevoir sur un socket avec sélection de tampon par le noyau, passez `nil` comme tampon de réception et demandez la
classe de taille voulue. La complétion indique quel tampon a été choisi :

```go
recvCtx := uring.PackDirect(uring.IORING_OP_RECV, 0, 0, 0)

if err := ring.Receive(recvCtx, &socketFD, nil, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
    return err
}

// Plus tard, après que Wait a renvoyé le CQE correspondant :
if cqe.HasBuffer() {
    fmt.Printf("kernel selected group=%d id=%d\n", cqe.BufGroup(), cqe.BufID())
}
```

Les réceptions groupées utilisent le même stockage de tampons fournis, mais peuvent consommer plusieurs tampons avec un
seul CQE. Traitez l'itérateur, puis recyclez les slots consommés :

```go
if err := ring.ReceiveBundle(recvCtx, &socketFD, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
    return err
}

if it, ok := ring.BundleIterator(cqe, cqe.BufGroup()); ok {
    for buf := range it.All() {
        handle(buf)
    }
    it.Recycle(ring)
}
```

Les tampons enregistrés nécessitent de la mémoire épinglée. En cas d'échec de l'enregistrement de tampons volumineux,
augmentez `RLIMIT_MEMLOCK` ou réduisez le budget mémoire.

## Opérations multishot et écouteur

`AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot` et `SubmitReceiveBundleMultishot` soumettent des opérations socket multishot.

La politique de routage des CQE reste hors du package. La mise en place de l'écouteur progresse via `DecodeListenerCQE`,
`PrepareListenerBind`, `PrepareListenerListen` et `SetListenerReady` ; c'est l'appelant qui décide de la distribution
des complétions et de l'arrêt de la chaîne.

## Architecture de l'implémentation

L'implémentation se structure autour des couches suivantes :

1. `New` construit un ring noyau désactivé, crée les pools de contexte et détermine la stratégie de tampons.
2. `Start` enregistre les tampons et active le ring conformément à la base fixe Linux 6.18+.
3. Les méthodes d'opération publient l'intention en écrivant des SQE.
4. `Wait` purge les soumissions et renvoie des vues CQE empruntées.
5. Le code d'exécution côté appelant décide de l'ordonnancement, des reprises, de l'attente, du routage
   connexion/session et
   de la politique terminale des ressources.

De cette manière, `uring` reste focalisé sur la mécanique côté noyau tout en préservant la sémantique des complétions au
travers de l'interface.

## Frontière d'exécution

Les couches d'exécution au-dessus de `uring` doivent l'utiliser comme backend noyau, pas comme ordonnanceur. La
frontière
idéale est unidirectionnelle : `uring` prépare les SQE, récupère les CQE, préserve `user_data`, expose `res` et fanions
des CQE, et rapporte les faits de propriété ; le code d'exécution côté appelant corrèle ces observations avec ses
propres
jetons, applique les reprises et l'attente progressive, route les gestionnaires et sessions, regroupe les soumissions et
libère les ressources
terminales.

Un pont d'exécution peut consommer les CQE en mode Extended lorsque l'exécution abstraite a besoin des faits de
complétion.
Un environnement d'exécution par connexion peut aussi sonder directement les CQE Extended bruts lorsqu'il a besoin du
résultat CQE, des
fanions, de l'ID de tampon et du jeton encodé avant de réduire l'événement en rappels de gestionnaire.

Les couches de contexte et d'exécution abstraite au-dessus de cette frontière ne modifient pas le rôle de frontière
noyau de `uring`.

## Patrons pour la couche applicative

`uring` expose les mécanismes tournés vers le noyau ; l'ordonnancement, les tentatives de reprise, le suivi de
connexions et l'interprétation du protocole relèvent des couches supérieures. Les patrons ci-dessous décrivent la
frontière qu'un environnement d'exécution côté appelant doit préserver.

### Boucle d'événements propriétaire du ring

En mode mono-émetteur (le mode par défaut), une seule goroutine sérialise toutes les opérations côté soumission. Une
boucle
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
        switch iox.Classify(err) {
        case iox.OutcomeWouldBlock:
            backoff.Wait()
            continue
        case iox.OutcomeFailure:
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
provenant d'autres goroutines entre dans la boucle via un canal ou une file sans verrou ; on n'appelle pas directement
les
méthodes du ring depuis l'extérieur. `iox.Backoff` reste côté appelant : on appelle `backoff.Wait()` quand `Wait` se
classe comme `iox.OutcomeWouldBlock` ou ne récupère aucun CQE, puis `backoff.Reset()` après tout lot avec `n > 0`.

### Cycle de vie des souscriptions multishot

Une opération multishot produit un flux de CQEs jusqu'à ce que le noyau envoie un CQE final (sans `IORING_CQE_F_MORE`).
Le code d'exécution côté appelant suit les souscriptions et gère la re-soumission :

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

Les contextes étendus transportent les références par connexion tout au long du cycle soumission → complétion, sans
recourir à
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
d'écouteur, mais le code d'exécution appelant qui place des refs typés doit les garder accessibles de manière
indépendante.

### Composition de délais

`LinkTimeout` attache un délai au SQE précédent via une chaîne `IOSQE_IO_LINK`. L'opération et le délai entrent en
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

La couche d'exécution appelante gère les deux issues : une réception réussie annule le délai, et un délai déclenché
annule la réception. Les deux produisent des CQEs que la boucle de distribution doit observer.

## Parcours TCP courants

Les parcours les plus concis, à lire en regard des tests :

| Scénario     | API principales                                                  | Référence                                                                         |
|--------------|------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Serveur echo | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| Client       | `TCP4Socket`, `Connect`, `Send`, `Receive`                       | `socket_integration_linux_test.go`                                                |

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

`listener_example_test.go` couvre la mise en place de l'écouteur avec accept multishot, `examples/multishot_test.go`
détaille les CQE côté gestionnaire pour le multishot receive, et `examples/echo_test.go` illustre le parcours echo
loopback
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
complétion correspondante. Le fichier `socket_integration_linux_test.go` couvre le flux connect/send côté client TCP.

## Réception sans copie (ZCRX)

`ZCRXReceiver` gère la réception sans copie depuis une file RX matérielle de NIC via `io_uring`.

`NewZCRXReceiver` est prévu pour les rings créés avec des CQE de 32 octets (`IORING_SETUP_CQE32`). La surface `Options`
actuelle n'expose pas ce fanion de configuration ; par conséquent, les rings créés via le chemin standard de `New`
conduisent ce constructeur à renvoyer `ErrNotSupported`. Tant qu'un chemin de configuration CQE32 n'est pas exposé,
cette section documente le contrat de frontière du récepteur plutôt qu'une recette publique exécutable.

### Cycle de vie

1. Avec un ring compatible CQE32, on crée le récepteur via `NewZCRXReceiver`. Le constructeur enregistre la file
   d'interface ZCRX, mappe la zone de remplissage et prépare le ring de remplissage.
2. Appelez `Start` pour soumettre l'opération étendue `RECV_ZC` sur le ring.
3. Sur le chemin de distribution des CQE, les complétions ZCRX sont acheminées vers le `ZCRXHandler` :
    - `OnData` livre un `ZCRXBuffer` pointant vers la zone mappée par la NIC. On appelle `Release` une fois le
      traitement terminé pour restituer le slot au noyau. Renvoyer `false` demande un arrêt au mieux.
    - `OnError` livre les erreurs CQE. Renvoyer `false` demande un arrêt au mieux.
   - `OnStopped` s'exécute une fois lors du retrait terminal avant que l'état ne devienne `Stopped`.
4. On appelle `Stop` pour soumettre une annulation asynchrone. Le récepteur transite par `Stopping` → `Retiring` →
   `Stopped`.
5. On interroge `Stopped` jusqu'à ce qu'il renvoie `true`, on arrête le ring propriétaire, puis on appelle `Close` pour
   libérer la zone mappée et le mappage du ring de remplissage.

### Machine à états

```
Idle → Active → Stopping → Retiring → Stopped
```

`Stop` revient à l'état `Active` si la soumission d'annulation échoue. `Close` est idempotent.

### Contrat du gestionnaire

- `OnData` et `OnError` sont appelés en série depuis la goroutine de distribution CQE.
- `Release` est mono-producteur : il ne doit être appelé que depuis la goroutine de distribution.
- `Stop` ne doit être appelé que lorsqu'il est garanti non concurrent avec la distribution CQE. Il s'agit d'un contrat
  de
  sérialisation côté appelant.

## Exemples

Les tests d'exemple dans `uring/examples/` illustrent l'API en situation concrète.

- `multishot_test.go`, accept multishot, réception multishot et arrêt d'abonnement
- `file_io_test.go`, lectures, écritures et traitement par lots
- `fixed_buffers_test.go`, tampons enregistrés et I/O sur tampons fixes
- `vectored_io_test.go`, opérations de lecture et écriture vectorisées
- `splice_tee_test.go`, transfert de données sans copie avec splice et tee
- `zerocopy_test.go`, chemins d'envoi sans copie et suivi des complétions
- `poll_test.go`, flux de disponibilité par poll
- `buffer_ring_test.go`, fourniture de rings de tampons et groupes de tampons multi-tailles
- `context_test.go`, flux `SQEContext` direct, indirect et extended, plus accès via `CQEView`
- `echo_test.go`, parcours serveur echo TCP et ping-pong UDP
- `timeout_linux_test.go`, opérations de délai et de délai lié

Au niveau du package, `listener_example_test.go` couvre la création d'écouteur et l'accept multishot, tandis que
`socket_integration_linux_test.go` couvre le flux client TCP connect/send.

## Notes opérationnelles

- Activez `NotifySucceed` pour obtenir un CQE visible à chaque opération réussie.
- `ring.Features` indique le nombre effectif d'entrées SQ et CQ, la largeur des emplacements SQE, ainsi que l'ordre des
  octets
  utilisé par le package pour interpréter `user_data`.
- Laissez `MultiIssuers` désactivé pour la configuration mono-émetteur par défaut (`SINGLE_ISSUER` + `DEFER_TASKRUN`),
  dans laquelle un seul chemin d'exécution de l'appelant sérialise les opérations d'état de soumission (`submit`,
  `Wait`/
  `enter`, `Stop` et resize). N'activez ce fanion que lorsque plusieurs goroutines nécessitent une soumission
  concurrente ou une entrée côté attente ; cela bascule le ring vers la configuration de soumission partagée
  `COOP_TASKRUN`.
- `EpollWait` exige que `timeout` vaille `0` ; utilisez `LinkTimeout` si vous avez besoin d'une échéance.
- Les vues de complétion empruntées et les contextes issus des pools doivent être libérés ou abandonnés sans délai.
- `ListenerOp.Close` ferme le FD de l'écouteur immédiatement. Si un CQE de mise en place est encore en attente,
  drainez-le
  d'abord, puis rappelez `Close` pour restituer le `ExtSQE` emprunté au pool.

## Support de plateforme

`uring` cible Go 1.26+ et Linux 6.18+ pour le chemin réel adossé au noyau. La plupart des fichiers d'implémentation et
des tests d'exemple portent la directive `//go:build linux`. Les fichiers Darwin fournissent uniquement des bouchons de
compilation pour la surface partagée ; les capacités propres à Linux restent propres à Linux et ne modifient en rien la
base d'exécution Linux décrite ci-dessus.

## Licence

MIT, voir [LICENSE](./LICENSE).

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)

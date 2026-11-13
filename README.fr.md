# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/uring)](https://goreportcard.com/report/github.com/hayabusa-cloud/uring)
[![Benchmarks](https://github.com/hayabusa-cloud/uring/actions/workflows/public-benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/uring/actions/workflows/public-benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Package Go pour l'interface noyau `io_uring` sous Linux 6.18+.

Langue: [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | [日本語](./README.ja.md) | **Français**

## Vue d'ensemble

`uring` est le package Go qui expose l'interface noyau Linux `io_uring`. Il crée et démarre les rings, prépare les SQE, décode les CQE, achemine l'identité de soumission via `user_data` et fournit l'enregistrement de tampons, les opérations multishot ainsi que les primitives de mise en place des écouteurs, sans pour autant devenir un ordonnanceur.

`uring` repose sur une conception à interface explicite : la mécanique côté noyau et les faits de complétion observables se situent au bord de l'API, tandis que la politique et la composition relèvent des couches appelantes. Le code d'exécution côté appelant possède le routage des complétions, le retrait des routes, les reprises et l'attente progressive, le routage des gestionnaires et des sessions, les contrôles de sécurité, le cycle de vie des connexions et la libération terminale des ressources.

Les surfaces principales sont :

- `Uring`, le handle du ring actif et son jeu d'opérations
- `SQEContext`, l'identité de soumission acheminée dans `user_data`
- `CQEView`, le descripteur de complétion copié renvoyé par `Wait`
- la fourniture de tampons, via tampons enregistrés et groupes de tampons multi-tailles

## Installation

`uring` nécessite un noyau Linux 6.18 ou ultérieur. Vérifiez d'abord la version du noyau en cours d'exécution :

```bash
uname -r
```

`uring` suppose la base 6.18+ et ne contient aucune branche de repli pour les noyaux plus anciens. Démarrez un noyau pris en charge plutôt que d'attendre des branches de compatibilité à l'intérieur de ce package.

Sous Debian 13, le noyau de la branche stable peut rester en deçà de 6.18. Consultez [Mise à niveau du noyau Debian 13](#mise-à-niveau-du-noyau-debian-13) pour obtenir un noyau empaqueté par Debian qui satisfait l'exigence.

```bash
go get code.hybscloud.com/uring
```

### Mise à niveau du noyau Debian 13

La branche stable de Debian 13 fournit le noyau 6.12. La suite `trixie-backports` met à disposition un noyau 6.18+ empaqueté par Debian. Consultez [SETUP.md](./SETUP.md) pour la marche à suivre détaillée.

### Dépannage

La création du ring peut renvoyer `ENOMEM`, `EPERM` ou `ENOSYS` selon les limites memlock, la configuration sysctl ou le support noyau. [Le profil seccomp par défaut de Docker](https://docs.docker.com/engine/security/seccomp/) bloque les appels système `io_uring`. Consultez [SETUP.md](./SETUP.md) pour le diagnostic et la résolution.

## Guide du code appelant

Commencez par [`AGENTS.md`](./AGENTS.md) pour développer ou relire des runtimes, piles de protocoles ou services utilisant `uring`. Le [guide du code appelant](./agents/INDEX.md) couvre la soumission, les complétions, l'identité des requêtes, la propriété des tampons, les durées de vie, l'intégration des packages et la vérification.

`uring` expose les opérations du noyau et leurs résultats. Le code appelant gère le polling, l'ordonnancement, les reprises et l'attente progressive, le routage des complétions et le retrait des routes, l'état et l'analyse du protocole, les politiques d'annulation et de délai, les contrôles de sécurité et le cycle de vie du service. Lors de la composition de ces opérations, préservez le sens de `ErrWouldBlock`, `ErrMore`, `IORING_CQE_F_MORE`, des transferts de propriété et des échecs de capacité.

## Cycle de vie du ring

`New` renvoie un ring non démarré et construit immédiatement les pools de contextes. Appelez `Start` avant de soumettre des opérations pour enregistrer les ressources et activer le ring. Cet exemple soumet une lecture dans la mémoire enregistrée du ring, dont l’adresse reste stable jusqu’à `Stop`. Consommez les octets après la complétion et avant de réutiliser le tampon. `iox.IsWouldBlock` signale l’absence de progrès ; le code appelant utilise `iox.Backoff` pour attendre sans solliciter inutilement le CPU.

```go
ring, err := uring.New(func(o *uring.Options) {
	o.Entries = uring.EntriesMedium
	o.NotifySucceed = true
})
if err != nil {
	return err
}

if err := ring.Start(); err != nil {
	return err
}
defer ring.Stop()

fd := iofd.FD(file.Fd()) // Emprunté ; file conserve la responsabilité de Close.
buf := ring.RegisteredBuffer(0)
ctx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, 0).WithFD(fd)
if err := ring.Read(ctx, buf); err != nil {
	return err
}

var cqes [1]uring.CQEView // Only one request is outstanding.
var backoff iox.Backoff
for {
	n, err := ring.Wait(cqes[:])
	if err != nil && !iox.IsWouldBlock(err) {
		return err
	}
	if n == 0 {
		backoff.Wait()
		continue
	}

	backoff.Reset()
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_READ || cqe.FD() != fd {
		return fmt.Errorf("unexpected completion: op=%d fd=%d", cqe.Op(), cqe.FD())
	}
	if err := cqe.Err(); err != nil {
		return fmt.Errorf("uring read failed: %w", err)
	}
	handle(buf[:int(cqe.Res)])
	return nil
}
```

`Wait` transmet les soumissions en attente, puis récolte les complétions. Sur un ring à émetteur unique, il effectue aussi l'entrée noyau qui fait avancer le travail différé une fois la SQ vide ; le code appelant doit sérialiser `Wait`, `WaitDirect` et `WaitExtended` avec les autres opérations sur l'état de soumission. `iox.IsWouldBlock(err)` indique qu'aucune complétion n'est actuellement observable.

`Start` et `Stop` constituent la paire de cycle de vie du ring. `Stop` est idempotent et rend le ring définitivement inutilisable ; on ne doit donc l'appeler qu'après avoir drainé toutes les opérations en vol, récupéré les CQE en attente et arrêté les abonnements multishot encore actifs.

## Types et opérations

| Type                  | Rôle                                                                                                         |
|-----------------------|--------------------------------------------------------------------------------------------------------------|
| `Uring`               | Initialisation du ring, soumission, récupération des complétions, et méthodes d'opération                    |
| `Options`             | Entrées du ring, budget de tampons enregistrés, échelle des groupes de tampons et visibilité des complétions |
| `SQEContext`          | Identité de soumission compacte stockée dans `user_data`                                                     |
| `CQEView`             | Descripteur de complétion copié avec accesseurs décodés et cibles empruntées selon le mode                    |
| `ListenerOp`          | Gestionnaire d'une opération de création d'écouteur avec FD et auxiliaires accept                            |
| `BundleIterator`      | Itère sur les tampons consommés lors d'une réception groupée                                                 |
| `IncrementalReceiver` | Gère les réceptions incrémentales de buffer-ring (`IOU_PBUF_RING_INC`)                                       |
| `BufferRing`          | Anneau opaque de tampons fournis enregistré explicitement ; `Add` prépare et `Advance` publie               |
| `ZCTracker`           | Suit le cycle de vie des notifications et du repli de l'envoi sans copie                                     |
| `ContextPools`        | Pools pour contextes de soumission indirects et étendus                                                      |
| `ZCRXReceiver`        | Cycle de vie de réception sans copie sur une file RX de NIC                                                  |
| `ZCRXConfig`          | Configuration d'une instance de réception ZCRX                                                               |
| `ZCRXHandler`         | Interface de rappel pour données, erreurs et arrêt ZCRX                                                      |
| `ZCRXBuffer`          | Vue de réception sans copie, restituée par l'appelant via la file de remplissage                             |

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
| Cmd | `UringCmd`, `UringCmdInline`, `UringCmd128`, `Nop`, `Nop128` |

`UringCmdInline` copie jusqu'à 80 octets dans un SQE `IORING_OP_URING_CMD` ordinaire et requiert `Options.SQE128` ; il ne requiert pas la prise en charge de l'opcode distinct `IORING_OP_URING_CMD128`.

`WithUringCmdFlags` écrit l'union `uring_cmd_flags`. Pour `IORING_URING_CMD_FIXED`, définissez l'index du tampon enregistré avec `sqeCtx.WithBufGroup` ; pour `IORING_URING_CMD_MULTISHOT`, définissez aussi `IOSQE_BUFFER_SELECT` et utilisez le même champ de contexte comme groupe de tampons fournis. Ces deux indicateurs sont mutuellement exclusifs.

`Nop128` et `UringCmd128` nécessitent un ring créé avec `Options.SQE128` et la prise en charge par le noyau des opcodes correspondants. Sans ces deux conditions, ces méthodes renvoient `ErrNotSupported`.

`Uring.Close` soumet un `IORING_OP_CLOSE` pour un descripteur de fichier cible. Il ne s'agit pas d'une méthode de destruction du ring.

`PollAddLevel` et `PollAddMultishotLevel` renvoient toujours `ErrNotSupported` ; io_uring sous Linux n'admet pas l'indicateur de niveau pour `IORING_OP_POLL_ADD`.

## Transport du contexte

`Multicast` choisit automatiquement un envoi ordinaire ou sans copie selon la taille du message et le nombre de destinations ; `MulticastZeroCopy` utilise des seuils plus bas pour favoriser le sans-copie. Les deux exigent un contexte Direct et renvoient `(submitted, error)` : `submitted` compte le préfixe de destinations accepté. En cas de contre-pression, reprendre avec les destinations restantes. Examiner le code d’opération et les indicateurs de chaque CQE, car une reprise peut sélectionner une autre opération. Les envois ordinaires de `Multicast` suivent `NotifySucceed` ; les envois sans copie sélectionnés et les deux chemins de `MulticastZeroCopy` conservent les observations de fin et rejettent `IOSQE_CQE_SKIP_SUCCESS` explicite.

Chaque envoi accepté sur un socket de flux peut transférer moins d'octets que demandé. Gardez les CQE de succès activées pour suivre les octets des envois ordinaires, puis utilisez le résultat de données de chaque destination pour ne reprendre que le suffixe restant. Gardez les octets partagés sans copie immuables jusqu'à ce que chaque destination acceptée atteigne sa condition de libération. L'[exemple multicast](./examples/zerocopy_test.go) montre le suivi des octets transférés et de la libération.

`SQEContext` est le jeton d'identité principal de ce package. En mode direct, il encode l'opcode, les fanions SQE, l'identifiant de groupe de tampons et le descripteur de fichier dans une seule valeur de 64 bits.

```go
sqeCtx := uring.ForFD(int32(fd)).
	WithOp(uring.IORING_OP_RECV).
	WithBufGroup(groupID)
```

Les trois modes de contexte sont :

| Mode     | Représentation                  | Usage typique                                                    |
|----------|---------------------------------|------------------------------------------------------------------|
| Direct   | Charge utile inline sur 64 bits | Chemin courant de soumission et de récupération, zéro allocation |
| Indirect | Pointeur vers `IndirectSQE`     | Lorsque 64 bits ne suffisent pas pour le SQE complet             |
| Extended | Pointeur vers `ExtSQE`          | SQE complet plus 64 octets de données utilisateur                |

Sur le chemin courant, on part de `ForFD` ou `PackDirect` en n'ajoutant que les bits que l'on souhaite retrouver à la complétion. `WithFlags` remplace la totalité des fanions : il convient donc de calculer les unions avant l'appel.

Lorsqu'on a besoin de métadonnées contrôlées par l'appelant au-delà du layout direct sur 64 bits, on emprunte un `ExtSQE`, on écrit dans son champ `UserData` via `Ctx*Of` ou `ViewCtx*`, puis on le ré-encode en `SQEContext`. Préférez des charges scalaires à cet endroit. Si une superposition brute ou une vue typée y stocke des pointeurs Go, des interfaces, des valeurs de fonction, des tranches, des chaînes, des tables de hachage, des canaux ou des structures qui en contiennent, conservez les racines vivantes en dehors de `UserData`, car le GC ne trace pas ces octets bruts.

```go
ext := ring.ExtSQE()
if ext == nil {
	return iox.ErrWouldBlock
}
meta := uring.CtxV1Of(ext)
meta.Val1 = requestSeq

sqeCtx := uring.PackExtended(ext)
fmt.Printf("sqe context mode=%d seq=%d\n", sqeCtx.Mode(), meta.Val1)
ring.PutExtSQE(ext)
```

`NewContextPools` renvoie des pools prêts à l'emploi. L'épuisement du pool de contextes est indépendant de la capacité de la file de soumission. Restituez chaque contexte à son pool d'origine après avoir traité toutes les complétions de la requête et terminé tout accès au contexte emprunté. `Reset` vide les deux pools et invalide les emprunts en cours ; appelez-le uniquement après avoir arrêté tout accès aux pools et terminé les opérations utilisant leurs contextes. Après `Reset`, n'accédez plus aux anciens emprunts et ne les restituez pas.

### Dispatch des complétions avec `CQEView`

Il n'existe pas de type distinct de contexte de complétion. Utilisez `cqe.Context()` pour retrouver le jeton initial, `cqe.Err()` pour obtenir le résultat de la requête et les indicateurs pour connaître l'état des tampons et leur durée de vie. L'erreur de `Wait` décrit la récolte, pas les requêtes individuelles. Distribuez tous les CQE du lot, y compris les échecs : un retour anticipé perdrait des observations déjà retirées du ring.

```go
var cqes [64]uring.CQEView
n, err := ring.Wait(cqes[:])
if err != nil {
	return err
}
if n == 0 {
	return iox.ErrWouldBlock
}

for i := range n {
	cqe := cqes[i]
	// Res < 0 encodes -errno; flags describe buffers and request lifetime.
	if err := cqe.Err(); err != nil {
		fmt.Printf("completion failed: op=%d fd=%d: %v\n", cqe.Op(), cqe.FD(), err)
	}
	if cqe.HasBuffer() {
		fmt.Printf("buffer id=%d\n", cqe.BufID())
	}
	// Route every CQE, including failures, so its owner can release resources.
	dispatch(ring, cqe)
}
```

À la complétion, `CQEView` décode le mode de contexte à la demande. Son descripteur scalaire est copié ; les cibles des modes Indirect/Extended et les tampons empruntés ne doivent pas survivre au-delà de leur durée de vie documentée.

## Fourniture de tampons

`uring` propose trois chemins pratiques pour les tampons. Les tampons enregistrés sont épinglés au démarrage du ring et servent aux I/O fichier sur tampon fixe. Les anneaux de tampons fournis laissent le noyau choisir un tampon de réception et renvoyer son ID dans le CQE. Les réceptions groupées suivent l'ordre de publication, qui peut différer de l'ordre numérique des ID après recyclage. `Uring.BundleIterator` conserve cet ordre. Le constructeur autonome `NewBundleIterator` exige des ID consécutifs modulo la taille du ring.

- des tampons fournis de taille fixe via `ReadBufferSize` et `ReadBufferNum`
- des groupes de tampons multi-tailles via `MultiSizeBuffer`
- des tampons fixes enregistrés via `LockedBufferMem`, `RegisteredBuffer`, `ReadFixed` et `WriteFixed`

Pour la plupart des systèmes, les fonctions auxiliaires de configuration constituent le point d'entrée le plus direct :

```go
opts := uring.OptionsForSystem(uring.MachineMemory4GB)
ring, err := uring.New(func(o *uring.Options) {
	*o = opts
})
```

On utilise `OptionsForBudget` pour partir d'un budget mémoire explicite, et `BufferConfigForBudget` pour inspecter la répartition par niveaux retenue pour ce budget :

```go
cfg, scale := uring.BufferConfigForBudget(256 * uring.MiB)
fmt.Printf("buffer tiers=%+v scale=%d\n", cfg, scale)
```

Les anneaux incrémentaux explicites sont initialisés vides. Après `Start`, choisissez un ID de groupe inutilisé par les groupes de tampons fournis configurés, préparez chaque tranche du stockage, puis publiez le lot :

```go
const (
	groupID = 60000
	entries = 8
	bufSize = 4096
)

backing := make([]byte, entries*bufSize)
bufRing, err := ring.RegisterBufRingIncremental(entries, groupID)
if err != nil {
	return err
}
for i := range entries {
	buf := backing[i*bufSize : (i+1)*bufSize]
	if err := bufRing.Add(buf, uint16(i), i); err != nil {
		return err
	}
}
if err := bufRing.Advance(entries); err != nil {
	return err
}

pool := uring.NewContextPools(entries)
receiver := uring.NewIncrementalReceiver(ring, pool, groupID, bufSize, backing, entries)
```

`BufferRing.Add` prépare les entrées ; `BufferRing.Advance` les publie et transfère leur propriété au noyau. Gardez `backing` accessible. Traitez les CQE de ce récepteur une seule fois, en série et dans l'ordre de la CQ ; le récepteur conserve un décalage indépendant pour chaque identifiant de tampon.

`IORING_CQE_F_BUF_MORE` indique que le noyau conserve le tampon sélectionné même après la fin de la requête. Ne le recyclez qu'après une CQE indiquant `IORING_CQE_F_BUFFER` sans `IORING_CQE_F_BUF_MORE` et la fin de tout accès emprunté. Un résultat nul ou négatif peut aussi rendre un tampon. `Uring.Stop` assure la destruction du buffer-ring.

L'I/O sur tampon fixe utilise un tampon enregistré par index. La tranche renvoyée appartient au ring ; gardez-la vivante jusqu'à la complétion de l'opération fixe :

```go
buf := ring.RegisteredBuffer(0)
copy(buf, payload)

fd := iofd.FD(file.Fd()) // Emprunté ; file conserve la responsabilité de Close.
ctx := uring.PackDirect(uring.IORING_OP_WRITE_FIXED, 0, 0, 0).WithFD(fd)
if err := ring.WriteFixed(ctx, 0, len(payload)); err != nil {
	return err
}
```

Pour recevoir sur un socket avec sélection de tampon par le noyau, passez `nil` comme tampon de réception et demandez la classe de taille voulue. La complétion indique quel tampon a été choisi :

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

Les réceptions groupées utilisent le même stockage de tampons fournis, mais peuvent consommer plusieurs tampons avec un seul CQE. Traitez l'itérateur, puis recyclez les slots consommés. Appelez `Recycle` exactement une fois pour l'ensemble des copies d'un itérateur ; après son retour, ne lisez plus les tampons produits :

```go
if err := ring.ReceiveBundle(recvCtx, &socketFD, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
	return err
}

if it, ok := ring.BundleIterator(cqe, cqe.BufGroup()); ok {
	for buf := range it.All() {
		handle(buf)
	}
	if err := it.Recycle(ring); err != nil {
		return err
	}
}
```

Les tampons enregistrés nécessitent de la mémoire épinglée. En cas d'échec de l'enregistrement de tampons volumineux, augmentez `RLIMIT_MEMLOCK` ou réduisez le budget mémoire.

## Opérations multishot et écouteur

`AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot` et `SubmitReceiveBundleMultishot` soumettent des opérations socket multishot.

Le routage des CQE reste à la charge de l'appelant. `ListenerOp.HandleCQE` fait progresser SOCKET → BIND → LISTEN et invoque les callbacks de configuration. `DecodeListenerCQE`, `PrepareListenerBind`, `PrepareListenerListen` et `SetListenerReady` restent disponibles pour une progression manuelle. Consultez le [guide du listener](agents/recipes/listener.md).

## Architecture de l'implémentation

L'implémentation se structure autour des couches suivantes :

1. `New` construit un ring noyau désactivé, crée les pools de contexte et détermine la stratégie de tampons.
2. `Start` enregistre les tampons et active le ring conformément à la base fixe Linux 6.18+.
3. Les méthodes d'opération publient l'intention en écrivant des SQE.
4. `Wait` purge les soumissions et renvoie des descripteurs CQE copiés.
5. Le code d'exécution côté appelant décide de l'ordonnancement, des reprises, de l'attente, du routage connexion/session, du retrait des routes, des contrôles de sécurité et de la politique terminale des ressources.

De cette manière, `code.hybscloud.com/uring` reste focalisé sur la mécanique côté noyau tout en préservant la sémantique des complétions au travers de l'interface.

## Frontière d'exécution

Les couches d'exécution au-dessus de `code.hybscloud.com/uring` doivent l'utiliser comme backend noyau, pas comme ordonnanceur. La frontière est unidirectionnelle : `code.hybscloud.com/uring` prépare les SQE, récupère les CQE, préserve `user_data`, expose `res` et fanions des CQE, et rapporte les faits de propriété ; le code d'exécution côté appelant corrèle ces observations avec ses propres jetons, applique les reprises et l'attente progressive, achemine les gestionnaires et sessions, regroupe les soumissions, retire les routes et libère les ressources terminales.

Un pont d'exécution peut consommer les CQE en mode Extended lorsque l'exécution abstraite a besoin des faits de complétion. Un environnement d'exécution par connexion peut aussi sonder directement les CQE Extended bruts lorsqu'il a besoin du résultat CQE, des fanions, de l'ID de tampon et du jeton encodé avant de réduire l'événement en rappels de gestionnaire.

Les couches de contexte et d'exécution abstraite au-dessus de cette frontière ne modifient pas le rôle de frontière noyau de `code.hybscloud.com/uring`.

## Patrons pour la couche applicative

`code.hybscloud.com/uring` expose les mécanismes tournés vers le noyau ; l'ordonnancement, les tentatives de reprise, le suivi de connexions et l'interprétation du protocole relèvent des couches appelantes situées au-dessus de lui. Les patrons ci-dessous décrivent la frontière qu'un environnement d'exécution côté appelant doit préserver.

### Boucle d'événements propriétaire du ring

Dans le mode par défaut, migrable entre tâches, la synchronisation partagée de l'état de soumission permet à une goroutine de changer de thread du système. Une boucle propriétaire du ring peut néanmoins sérialiser toutes les opérations : elle soumet le travail en attente, applique un `iox.Backoff` détenu par l'appelant quand `Wait` ne signale aucun progrès observable, puis distribue les complétions :

```go
func runLoop(ring *uring.Uring, stop <-chan struct{}) error {
	cqes := make([]uring.CQEView, 64)
	var backoff iox.Backoff
	for {
		select {
		case <-stop:
			// The caller must still cancel and drain live requests before ring.Stop.
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

Les méthodes du ring, dont `Send`, `Receive`, `AcceptMultishot` et `Wait`, s'exécutent sur cette goroutine. Le travail provenant d'autres goroutines doit entrer dans la boucle par une boîte aux lettres de travail détenue par l'appelant, comme `code.hybscloud.com/lfq`, et non en appelant directement les méthodes du ring. `iox.Backoff` reste côté appelant : on appelle `backoff.Wait()` quand `Wait` est classé comme `iox.OutcomeWouldBlock` ou ne récupère aucun CQE, puis `backoff.Reset()` après tout lot avec `n > 0`.

### Cycle de vie des souscriptions multishot

Une opération multishot produit un flux de CQEs jusqu'à ce que le noyau envoie un CQE final (sans `IORING_CQE_F_MORE`). Le code d'exécution côté appelant achemine chaque CQE par la souscription renvoyée avant de passer au reste du répartiteur :

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

sub, err := ring.AcceptMultishot(acceptCtx, handler.Handler())
if err != nil {
	return err
}

// Dispatch dans la même boucle de complétion sérialisée. Si le code appelant
// conserve des CQE copiées après cette boucle, il doit garder son propre état de routage.
for i := range n {
	if sub.HandleCQE(cqes[i]) {
		continue
	}
	dispatch(ring, cqes[i])
}
```

Chaque rappel `OnStep` observe un `MultishotStep` : renvoyez `MultishotContinue` pour maintenir le flux actif ou `MultishotStop` pour demander l'annulation. Si les rappels restent activés jusqu'à l'observation terminale, `OnStop` s'exécute au plus une fois avec l'erreur finale et l'indicateur `cancelled` ; sur le pas lui-même, `step.Cancelled` distingue à la frontière le verdict spécifique `-ECANCELED` du noyau des autres échecs. Ces rappels correspondent à `MultishotHandler.OnMultishotStep` et `MultishotHandler.OnMultishotStop` ; vous pouvez aussi implémenter cette interface directement. `HandleCQE` sert au dispatch immédiat dans la boucle de complétion sérialisée de l'appelant ; si le code appelant conserve des CQE copiées après cette boucle, il doit garder son propre état de routage et rejeter les observations de souscriptions déjà retirées. Sur les rings mono-émetteur explicites (`MultiIssuers=false`), appelez `Cancel` / `Unsubscribe` depuis le propriétaire épinglé à un thread du système et sérialisez-les avec les opérations de soumission, `Wait`, `WaitDirect`, `WaitExtended`, `Stop` et `ResizeRings`. Les rings `MultiIssuers` par défaut sérialisent les SQE d'annulation par le chemin de soumission partagé.

Transmettez chaque CQE d'annulation soumise au `HandleCQE` de la même souscription ou à son gestionnaire étendu, même après `SubscriptionStopped` ou `OnStop`. Une soumission échouée ou un appel sans effet ne crée aucune nouvelle obligation de CQE d'annulation. La souscription possède les deux contextes et ne rend l'original à son pool qu'après le traitement de la CQE terminale originale, le dispatch d'annulation et la fin de sa soumission. Ne rendez pas ces contextes manuellement. L'[exemple de réception](./examples/multishot_test.go) montre la fin naturelle et l'annulation demandée par le gestionnaire.

### État par connexion via des contextes typés

Les contextes étendus transportent les références par connexion tout au long du cycle soumission → complétion, sans recourir à une table de correspondance globale :

```go
type ConnState struct {
	Addr    netip.AddrPort
	Created int64
}

ext := ring.ExtSQE()
if ext == nil {
	return iox.ErrWouldBlock
}
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

On maintient les racines de pointeurs Go actives accessibles en dehors de `UserData`. Le GC ne trace pas ces octets bruts. Le jeu de racines sidecar rattaché à chaque slot `ExtSQE` s'en charge pour les protocoles internes multishot et d'écouteur, mais le code d'exécution appelant qui place des références typées doit les garder accessibles de manière indépendante.

### Composition de délais

`LinkTimeout` attache une échéance au SQE précédent via une chaîne `IOSQE_IO_LINK`. À expiration, il tente d'annuler l'opération liée, qui peut déjà être terminée. Examinez les deux résultats de CQE ; l'expiration ne prouve pas que l'opération a été annulée.

```go
recvCtx := uring.ForFD(int32(fd)).
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

Soumettez les SQE liés de façon contiguë, en sérialisant la paire avec les autres appels de soumission et d'attente. Gardez les tampons et le contexte de l'opération jusqu'à sa propre complétion, et traitez les deux CQE avant la fermeture.

## Parcours TCP courants

Les parcours les plus concis, à lire en regard des tests :

| Scénario     | API principales                                                  | Référence                                                                         |
|--------------|------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Serveur echo | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| Client       | `TCP4Socket`, `Connect`, `Send`, `Receive`                       | `socket_integration_linux_test.go`                                                |

### Serveur echo TCP

`ListenerManager` soumet la requête SOCKET initiale. Transmettez chaque CQE de configuration à `listenerOp.HandleCQE` pour invoquer les callbacks et soumettre BIND et LISTEN automatiquement. En cas de retour `iox.ErrWouldBlock`, faites progresser l'anneau, puis réessayez le SQE préparé avec `ring.SubmitExtended(uring.PackExtended(listenerOp.Ext()))` ; ne traitez pas de nouveau le CQE. Commencez à accepter les connexions après confirmation du succès par `OnListening`.

```go
// OnListening has reported successful setup for listenerOp.
acceptSub, err := listenerOp.AcceptMultishot(acceptHandler)
if err != nil {
	return err
}

recvCtx := uring.ForFD(int32(clientFD)).WithBufGroup(readGroup)
recvSub, err := ring.ReceiveMultishot(recvCtx, recvHandler)
if err != nil {
	return err
}
```

Pour arrêter une souscription, appelez `Cancel`, puis poursuivez le dispatch jusqu'au traitement de sa CQE terminale et de la CQE d'annulation, si celle-ci a été soumise. Drainez les deux souscriptions avant de fermer leurs descripteurs ou d'arrêter le ring. [L'exemple d'écouteur](./listener_example_test.go) montre la configuration et la fermeture complètes ; les exemples [multishot](./examples/multishot_test.go) et [echo](./examples/echo_test.go) couvrent le flux de connexion.

### Client TCP

On crée d'abord le socket, on attend la complétion `IORING_OP_SOCKET`, puis on convertit le FD renvoyé en `iofd.FD` pour l'utiliser avec `Connect`, `Send` et `Receive`.

```go
clientCtx := uring.PackDirect(uring.IORING_OP_SOCKET, 0, 0, 0)
if err := ring.TCP4Socket(clientCtx); err != nil {
	return err
}

// After Wait returns the matching SOCKET CQE:
if err := socketCQE.Err(); err != nil {
	return err
}
clientFD := iofd.NewFD(int(socketCQE.Res))

connectCtx := uring.PackDirect(uring.IORING_OP_CONNECT, 0, 0, int32(clientFD))
if err := ring.Connect(connectCtx, remoteAddr); err != nil {
	return err
}

// Wait for a successful CONNECT CQE before sending.
sendCtx := uring.PackDirect(uring.IORING_OP_SEND, 0, 0, int32(clientFD))
if err := ring.Send(sendCtx, &clientFD, payload); err != nil {
	return err
}

// Check the SEND CQE and retry only any unsent suffix.
recvCtx := uring.PackDirect(uring.IORING_OP_RECV, 0, 0, int32(clientFD))
if err := ring.Receive(recvCtx, &clientFD, buf); err != nil {
	return err
}
// Wait for the RECV CQE before reading or reusing buf.
```

Après chaque soumission, utilisez la boucle `Wait` de la section sur le cycle de vie du ring pour observer la complétion correspondante avant l'opération suivante qui en dépend. Conservez les données jusqu'à la complétion et ne renvoyez que le suffixe restant après un envoi partiel. [socket_test.go](./examples/socket_test.go) montre la séquence complète du client ; [echo_test.go](./examples/echo_test.go) ajoute la réception et la réponse.

## Réception sans copie (ZCRX)

`ZCRXReceiver` gère la réception sans copie depuis une file RX matérielle de NIC via `io_uring`.

`NewZCRXReceiver` est prévu pour les rings créés avec des CQE de 32 octets (`IORING_SETUP_CQE32`). La surface `Options` actuelle n'expose pas ce fanion de configuration ; par conséquent, ce constructeur renvoie `ErrNotSupported` pour les rings créés via le chemin standard de `New`. Tant qu'un chemin de configuration CQE32 n'est pas exposé, cette section documente le contrat de frontière du récepteur plutôt qu'une recette publique exécutable.

### Cycle de vie

1. Avec un ring compatible CQE32, on crée le récepteur via `NewZCRXReceiver`. Le constructeur enregistre la file d'interface ZCRX, mappe la zone de réception et initialise un ring de remplissage vide. Linux fournit les buffers de réception initiaux ; le ring de remplissage restitue les buffers livrés.
2. Appelez `Start` pour soumettre l'opération étendue `RECV_ZC` sur le ring.
3. Sur le chemin de distribution des CQE, les complétions ZCRX sont acheminées vers le `ZCRXHandler` :
    - `OnData` livre un `ZCRXBuffer` pointant vers la zone mappée par la NIC. Appelez `Release` après traitement ; la publication réussie dans la file de remplissage restitue le slot au noyau. Si la méthode renvoie `iox.ErrWouldBlock`, conservez le tampon et réessayez la libération plus tard sur le chemin des CQE. Un tampon EOF de longueur nulle n'a pas d'identité de remplissage et se libère sans publier d'entrée. Renvoyer `false` demande un arrêt au mieux.
    - `OnError` livre les erreurs CQE. Renvoyer `false` demande un arrêt au mieux.
    - `OnStopped` s'exécute une fois lors du retrait terminal avant que l'état ne devienne `Stopped`.
4. On appelle `Stop` pour soumettre une annulation asynchrone. Le récepteur transite par `Stopping` → `Retiring` → `Stopped`.
5. On interroge `Stopped` jusqu'à ce qu'il renvoie `true`, on arrête le ring propriétaire, puis on appelle `Close` pour libérer la zone mappée et le mappage du ring de remplissage.

### Machine à états

```
Idle → Active → Stopping → Retiring → Stopped
```

`Stop` revient à l'état `Active` si la soumission d'annulation échoue. `Close` est idempotent.

### Contrat du gestionnaire

- `OnData` et `OnError` sont appelés en série depuis la goroutine de distribution CQE.
- `Release` est mono-producteur : il ne doit être appelé que depuis la goroutine de distribution.
- `Stop` ne doit être appelé que lorsqu'il est garanti non concurrent avec la distribution CQE. Il s'agit d'un contrat de sérialisation côté appelant.

## Exemples

Les tests exécutables de `examples/` enseignent initialisation → soumission → complétion → libération par des appels directs à l'API publique. Commencez par les contextes et les E/S de fichiers, puis étudiez les règles de propriété dans les exemples de sockets et de multishot.

- [context_test.go](./examples/context_test.go): contextes Direct, Indirect et Extended ; décodage des complétions et restitution des contextes empruntés
- [completion_test.go](./examples/completion_test.go): erreurs de soumission et de complétion, identification des requêtes et récolte de lots mêlant succès et échecs
- [file_io_test.go](./examples/file_io_test.go): lectures et écritures de fichiers, lecture par blocs jusqu’à EOF et soumissions par lots
- [file_lifecycle_test.go](./examples/file_lifecycle_test.go): ouvrir, écrire, synchroniser et fermer un fichier au rythme des complétions
- [vectored_io_test.go](./examples/vectored_io_test.go): lectures et écritures scatter/gather
- [fixed_buffers_test.go](./examples/fixed_buffers_test.go): tampons enregistrés et E/S à tampons fixes
- [buffer_ring_test.go](./examples/buffer_ring_test.go): réceptions groupées et incrémentales, recyclage des tampons
- [socket_test.go](./examples/socket_test.go): création du socket client, complétion de la connexion et envoi du suffixe restant
- [echo_test.go](./examples/echo_test.go): écho TCP, E/S partielles de flux et datagrammes UDP
- [multishot_test.go](./examples/multishot_test.go): complétions répétées d'accept/receive et récolte après annulation
- [zerocopy_test.go](./examples/zerocopy_test.go): envois zero-copy, sélection automatique de multicast et suivi distinct du résultat et de la libération
- [poll_test.go](./examples/poll_test.go): observation de disponibilité et annulation de poll
- [timeout_linux_test.go](./examples/timeout_linux_test.go): expiration, annulation et timeouts liés
- [splice_tee_test.go](./examples/splice_tee_test.go): transferts splice et tee guidés par les complétions

À la racine du package, [listener_example_test.go](./listener_example_test.go) montre la création automatique SOCKET → BIND → LISTEN et multishot accept ; [socket_integration_linux_test.go](./socket_integration_linux_test.go) couvre TCP connect/send.

## Notes opérationnelles

- Activez `NotifySucceed` pour conserver par défaut les CQE des opérations réussies de type écriture. Les flags explicites `IOSQE_CQE_SKIP_SUCCESS` restent applicables.
- `ring.Features` indique le nombre effectif d'entrées SQ et CQ, la largeur des emplacements SQE, ainsi que l'ordre des octets utilisé par le package pour interpréter `user_data`.
- Conservez la configuration par défaut `MultiIssuers=true`, migrable entre tâches (`COOP_TASKRUN`), pour l'exécution Go ordinaire et les appels concurrents sur l'état de soumission. Utilisez `MultiIssuers=false` uniquement si le propriétaire appelle `runtime.LockOSThread` avant `New`, reste épinglé jusqu'à `Stop` et sérialise `submit`, `Wait`, `WaitDirect`, `WaitExtended`, `Stop` et `ResizeRings` ; ce chemin rapide explicite demande `SINGLE_ISSUER` + `DEFER_TASKRUN`.
- `EpollWait` exige que `timeout` vaille `0` ; utilisez `LinkTimeout` si vous avez besoin d'une échéance.
- Restituez les contextes au pool seulement après le traitement de toutes les complétions de la requête et la fin des accès empruntés.
- `ListenerOp.Close` ferme le FD de l'écouteur immédiatement. Si un CQE de mise en place est encore en attente, drainez-le d'abord, puis rappelez `Close` pour restituer le `ExtSQE` emprunté au pool.

## Support de plateforme

`uring` cible Go 1.26+ et Linux 6.18+ pour le chemin réel adossé au noyau. La plupart des fichiers d'implémentation et des tests d'exemple portent la directive `//go:build linux` ; les fichiers Darwin fournissent uniquement des bouchons de compilation pour la surface partagée, et les capacités propres à Linux restent propres à Linux sans modifier en rien la base d'exécution Linux décrite ci-dessus.

## Licence

MIT, voir [LICENSE](./LICENSE).

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)

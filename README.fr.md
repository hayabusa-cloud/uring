# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Benchmarks](https://github.com/hayabusa-cloud/uring/actions/workflows/benchmarks.yml/badge.svg)](https://github.com/hayabusa-cloud/uring/actions/workflows/benchmarks.yml)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Package Go pour l'interface noyau `io_uring` sous Linux 6.18+.

Langue: [English](./README.md) | [简体中文](./README.zh-CN.md) | [Español](./README.es.md) | [日本語](./README.ja.md) | **Français**

## Vue d'ensemble

`uring` est le package Go qui expose l'interface noyau Linux `io_uring`. Il crée et démarre les rings, prépare les SQE, décode les CQE, achemine l'identité de soumission via `user_data` et fournit l'enregistrement de tampons, les opérations multishot ainsi que les primitives de mise en place des écouteurs, sans pour autant devenir un ordonnanceur.

`uring` repose sur une conception à interface explicite : la mécanique côté noyau et les faits de complétion observables se situent au bord de l'API, tandis que la politique et la composition relèvent des couches appelantes. Le code d'exécution côté appelant possède le routage des complétions, le retrait des routes, les reprises et l'attente progressive, le routage des gestionnaires et des sessions, les contrôles de sécurité, le cycle de vie des connexions et la libération terminale des ressources.

Les surfaces principales sont :

- `Uring`, le handle du ring actif et son jeu d'opérations
- `SQEContext`, le contexte de soumission transmis dans `user_data`
- `CQEView`, le descripteur de complétion copié renvoyé par `Wait`
- la fourniture de tampons, via tampons enregistrés et groupes de tampons multi-tailles

## Installation

La compilation de `uring` nécessite Go 1.27 ou une version ultérieure. Pour utiliser `io_uring` dans le noyau, il faut Linux 6.18 ou une version ultérieure. Vérifiez d’abord la version du noyau en cours d’exécution.

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

L’[exemple d’E/S sur fichier](./examples/file_io_test.go#L30) montre comment piloter directement un ring de `Start` à `Stop`. Consultez le [guide du code appelant](./agents/INDEX.md) pour une opération précise et les [consignes du package](./AGENTS.md) pour les règles de propriété et de vérification.

Une soumission par lot peut accepter une partie des requêtes, puis renvoyer `iox.ErrWouldBlock` pour les suivantes. Dans la CQE correspondante, `Res` donne le résultat de l’opération et `IORING_CQE_F_MORE` indique qu’une autre CQE peut suivre pour la même requête. L’interprétation de ce drapeau comme `iox.ErrMore` dépend du protocole de l’appelant. Le noyau peut avoir effectué l’opération avant de signaler une erreur. Par exemple, la CQE source de `MsgRingFD` peut signaler `-EOVERFLOW` après l’installation du fichier dans le ring cible. [Reprise après un transfert de fichier](./agents/boundary/resources.md#inter-ring-file-transfers)

## Cycle de vie du ring

`New` crée un ring non démarré et ses pools de contextes. Avec la valeur par défaut `MultiIssuers=false`, gardez la goroutine propriétaire attachée à un même thread du système depuis avant `Start` jusqu'à la fin de `Stop`. Appelez `Start` pour enregistrer les ressources et autoriser les soumissions.

Cet exemple lit les données dans un tampon enregistré appartenant au ring, dont l'adresse reste stable jusqu'à `Stop`. Consommez les octets après la complétion et avant de réutiliser le tampon. `iox.IsWouldBlock` signale l'absence de progrès ; utilisez un `iox.Backoff` du côté appelant pour attendre sans solliciter inutilement le CPU.

Cet exemple termine le processus avec `log.Fatal` en cas d'erreur irrécupérable. Un service doit conserver les requêtes en cours et traiter toutes leurs complétions avant de détruire le ring.

```go
runtime.LockOSThread()
defer runtime.UnlockOSThread()

ring, err := uring.New(func(o *uring.Options) {
	o.Entries = uring.EntriesMedium
	o.NotifySucceed = true
})
if err != nil {
	log.Fatal(err)
}

if err := ring.Start(); err != nil {
	log.Fatal(err)
}

fd := iofd.FD(file.Fd()) // Emprunté ; file conserve la responsabilité de Close.
buf := ring.RegisteredBuffer(0)
ctx := uring.PackDirect(uring.IORING_OP_READ, 0, 0, 0).WithFD(fd)
if err := ring.Read(ctx, buf); err != nil {
	log.Fatal(err)
}

var cqes [1]uring.CQEView // Only one request is outstanding.
var backoff iox.Backoff
for {
	n, err := ring.Wait(cqes[:])
	if err != nil && !iox.IsWouldBlock(err) {
		log.Fatal(err)
	}
	if n == 0 {
		backoff.Wait()
		continue
	}

	backoff.Reset()
	break
}

cqe := cqes[0]
if cqe.Op() != uring.IORING_OP_READ || cqe.FD() != fd {
	log.Fatalf("unexpected completion: op=%d fd=%d", cqe.Op(), cqe.FD())
}
runtime.KeepAlive(file) // Gardez le propriétaire du FD en vie jusqu’à la fin de la lecture.
completionErr := cqe.Err()
if completionErr == nil {
	handle(buf[:int(cqe.Res)])
}
if err := ring.Stop(); err != nil {
	log.Fatal(err)
}
if completionErr != nil {
	log.Fatal(completionErr)
}
```

`Wait` transmet les soumissions en attente, puis récolte les complétions. Sur un ring à émetteur unique, il effectue aussi l'entrée noyau qui fait avancer le travail différé une fois la SQ vide ; le code appelant doit sérialiser `Wait`, `WaitDirect` et `WaitExtended` avec les autres opérations sur l'état de soumission. `iox.IsWouldBlock(err)` indique qu'aucune complétion n'est actuellement observable.

`Start` et `Stop` constituent la paire de cycle de vie du ring. `Stop` est idempotent et rend le ring définitivement inutilisable ; on ne doit donc l'appeler qu'après avoir drainé toutes les opérations en vol, récupéré les CQE en attente et arrêté les abonnements multishot encore actifs.

## Types et opérations

| Type                  | Rôle                                                                                                         |
|-----------------------|--------------------------------------------------------------------------------------------------------------|
| `Uring`               | Initialisation du ring, soumission, récupération des complétions, et méthodes d'opération                    |
| `Options`             | Entrées du ring, budget de tampons enregistrés, échelle des groupes de tampons et visibilité des complétions |
| `SQEContext` | Contexte de soumission encodé dans `user_data` |
| `CQEView`             | Descripteur de complétion copié avec accesseurs décodés et cibles empruntées selon le mode                    |
| `ListenerOp`          | Gestionnaire d'une opération de création d'écouteur avec FD et auxiliaires accept                            |
| `BundleIterator`      | Itère sur les tampons consommés lors d'une réception groupée                                                 |
| `BufferRing`          | Anneau opaque de tampons fournis enregistré explicitement ; `Add` prépare et `Advance` publie               |
| `IncrementalReceiver` | Gère les réceptions incrémentales de buffer-ring (`IOU_PBUF_RING_INC`)                                       |
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
| Socket I/O | `Receive`, `Send`, `RecvMsg`, `SendMsg`, `SendMsgHeader`, `ReceiveBundle`, `ReceiveZeroCopy`, `Multicast`, `MulticastZeroCopy` |
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

`PollAddLevel` et `PollAddMultishotLevel` renvoient toujours `ErrNotSupported` ; io_uring sous Linux n'admet pas l'indicateur de niveau pour `IORING_OP_POLL_ADD`.

`Uring.Close` soumet un `IORING_OP_CLOSE` pour un descripteur de fichier cible. Il ne s'agit pas d'une méthode de destruction du ring.

## Transport du contexte

`Multicast` choisit automatiquement un envoi ordinaire ou sans copie selon la taille du message et le nombre de destinations ; `MulticastZeroCopy` utilise des seuils plus bas pour favoriser le sans-copie. Les deux exigent un contexte Direct et renvoient `(submitted, error)` : `submitted` compte le préfixe de destinations accepté. En cas de contre-pression, reprendre avec les destinations restantes. Examiner le code d’opération et les indicateurs de chaque CQE, car une reprise peut sélectionner une autre opération. Les envois ordinaires de `Multicast` suivent `NotifySucceed` ; les envois sans copie sélectionnés et les deux chemins de `MulticastZeroCopy` conservent les observations de fin et rejettent `IOSQE_CQE_SKIP_SUCCESS` explicite.

Après un envoi partiel sur un socket de flux, reprenez uniquement les octets restant à envoyer. Activez les CQE de succès pour suivre le nombre d’octets des envois ordinaires. Gardez les données partagées intactes jusqu’à leur libération par tous les envois acceptés. Un envoi ordinaire les libère à sa complétion. Pour `SEND_ZC`, attendez une CQE de données finale sans `IORING_CQE_F_MORE`, ou à la fois la CQE de données et sa notification. [Exemple d’envoi à plusieurs destinations](./examples/zerocopy_test.go)

En mode Direct, `SQEContext` encode l’opcode, les drapeaux de la SQE, l’identifiant du groupe de tampons et le descripteur de fichier sur 64 bits. Deux envois en cours dont ces champs sont identiques ont le même `user_data`. Pour attribuer chaque résultat à un envoi précis, sérialisez ces envois ou soumettez-les séparément avec des contextes distincts qui restent valides.

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

Quand `Wait` renvoie `iox.ErrWouldBlock`, aucune CQE n’est prête. En cas de succès, traitez les `n` CQE, y compris celles des requêtes en échec. Pour une requête locale, `cqe.Context()` renvoie son jeton de soumission. Utilisez `cqe.Err()` pour interpréter le résultat d’une opération. Dans une notification d’envoi sans copie, `Res` peut contenir des indicateurs d’utilisation. Les indicateurs de la CQE renseignent sur la durée de vie de la requête et des tampons.

L’expéditeur et le destinataire réservent `messageID` à `MsgRing`. Sur l’anneau de cet exemple, tous les autres identifiants correspondent à des requêtes locales. Vérifiez d’abord `messageID` et transmettez au gestionnaire de messages la valeur signée de `Res` fournie par l’expéditeur, même si elle est négative.

```go
var cqes [64]uring.CQEView
n, err := ring.Wait(cqes[:])
if err != nil {
	return err
}

for i := range n {
	cqe := cqes[i]
	if cqe.Context().Raw() == uint64(messageID) {
		handleMessage(cqe.Res)
		continue
	}
	if !cqe.IsNotification() && cqe.Res < 0 {
		fmt.Printf("completion failed: op=%d fd=%d: %v\n", cqe.Op(), cqe.FD(), cqe.Err())
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
runtime.LockOSThread()
defer runtime.UnlockOSThread()

ring, err := uring.New(func(o *uring.Options) {
	*o = opts
})
if err != nil {
	return err
}
defer ring.Stop()
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

`WriteFixed` écrit les octets d’un tampon enregistré désigné par son indice. Gardez le fichier ouvert et les octets intacts jusqu’à la complétion. Si la CQE détermine le moment où le tampon est réutilisable, activez `NotifySucceed` et laissez `IOSQE_CQE_SKIP_SUCCESS` désactivé.

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

Une réception groupée peut consommer plusieurs tampons fournis dans une seule CQE. Sur un socket de flux comme dans cet exemple, le noyau peut rendre un slot sans données en fin de flux ou en cas d’erreur. Terminez la lecture des données, puis appelez `Recycle` une seule fois pour l’ensemble des copies de l’itérateur afin de rendre tous les slots consommés.

```go
if err := ring.ReceiveBundle(recvCtx, &socketFD, uring.WithReadBufferSize(uring.BufferSizeSmall)); err != nil {
	return err
}

if it, ok := ring.BundleIterator(cqe, cqe.BufGroup()); ok {
	if it.TotalBytes() > 0 {
		for buf := range it.All() {
			handle(buf)
		}
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

Attachez la goroutine propriétaire du ring par défaut à un thread du système avant `Start` et gardez-la sur ce thread jusqu’à la fin de `Stop`. Une fois les requêtes soumises, le propriétaire appelle `Wait` pour recueillir leurs résultats.

```go
cqes := make([]uring.CQEView, 64)
var backoff iox.Backoff
for {
	select {
	case <-stop:
		// L’appelant annule et draine les requêtes en cours avant d’arrêter le ring.
		return nil
	default:
	}

	n, err := ring.Wait(cqes)
	switch iox.Classify(err) {
	case iox.OutcomeOK:
		backoff.Reset()
		for i := range n {
			dispatch(ring, cqes[i])
		}
	case iox.OutcomeWouldBlock:
		backoff.Wait()
	case iox.OutcomeFailure:
		return err
	}
}
```

Même si `Wait` réussit, le lot peut contenir une CQE pour une opération qui a échoué. Distribuez les `n` entrées. Si des requêtes sont à soumettre à nouveau, examinez-les entre deux appels à `Wait`, même lorsque la CQ est vide. [Réarmer un accept géré](agents/recipes/stream.md#rearm-a-managed-accept)

### Cycle de vie des souscriptions multishot

Une opération multishot produit un flux de CQEs jusqu'à ce que le noyau envoie un CQE final (sans `IORING_CQE_F_MORE`). Le code d'exécution côté appelant achemine chaque CQE par la souscription renvoyée avant de passer au reste du répartiteur :

```go
accepting := true
handler := uring.NewMultishotSubscriber().
	OnStep(func(step uring.MultishotStep) uring.MultishotAction {
		if step.Err != nil {
			return uring.MultishotStop
		}
		connFD := iofd.FD(step.CQE.Res)
		if accepting {
			registerConnection(connFD)
		} else if err := connFD.Close(); err != nil {
			log.Print(err)
		}
		return uring.MultishotContinue
	}).
	OnStop(func(err error, cancelled bool) {
		if accepting && !cancelled {
			queueAcceptRetry(err)
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

`queueAcceptRetry` enregistre le résultat final pour le propriétaire du ring. Pendant `OnStop`, l’ancienne souscription peut encore détenir le dernier contexte Extended. Après le traitement des CQE, le propriétaire décide si le résultat et l’état du socket d’écoute justifient un nouvel accept. Si `AcceptMultishot` renvoie `iox.ErrWouldBlock`, la tentative reste en attente. [Réarmer un accept géré](agents/recipes/stream.md#rearm-a-managed-accept)

Chaque rappel `OnStep` observe un `MultishotStep` : renvoyez `MultishotContinue` pour maintenir le flux actif ou `MultishotStop` pour demander l'annulation. Si les rappels restent activés jusqu'à l'observation terminale, `OnStop` s'exécute au plus une fois avec l'erreur finale et l'indicateur `cancelled` ; sur le pas lui-même, `step.Cancelled` distingue à la frontière le verdict spécifique `-ECANCELED` du noyau des autres échecs. Ces rappels correspondent à `MultishotHandler.OnMultishotStep` et `MultishotHandler.OnMultishotStop` ; vous pouvez aussi implémenter cette interface directement. `HandleCQE` sert au dispatch immédiat dans la boucle de complétion sérialisée de l'appelant ; si le code appelant conserve des CQE copiées après cette boucle, il doit garder son propre état de routage et rejeter les observations de souscriptions déjà retirées. Sur les rings mono-émetteur par défaut (`MultiIssuers=false`), appelez `Cancel` / `Unsubscribe` depuis le propriétaire épinglé à un thread du système et sérialisez-les avec les opérations de soumission, `Wait`, `WaitDirect`, `WaitExtended`, `Stop` et `ResizeRings`. Les rings avec `MultiIssuers=true` sérialisent les SQE d'annulation par le chemin de soumission partagé.

Transmettez chaque CQE d'annulation soumise au `HandleCQE` de la même souscription ou à son gestionnaire étendu, même après `SubscriptionStopped` ou `OnStop`. Une soumission échouée ou un appel sans effet ne crée aucune nouvelle obligation de CQE d'annulation. La souscription possède les deux contextes et ne rend l'original à son pool qu'après le traitement de la CQE terminale originale, le dispatch d'annulation et la fin de sa soumission. Ne rendez pas ces contextes manuellement. L'[exemple de réception](./examples/multishot_test.go) montre la fin naturelle et l'annulation demandée par le gestionnaire.

Pour cesser d’admettre des connexions, le propriétaire de la boucle fixe `accepting = false` avant d’appeler `sub.Cancel()`. Le gestionnaire de pas ferme alors les sockets acceptés pendant l’annulation. Après `Unsubscribe`, le répartiteur prend en charge chaque socket accepté dont le rappel a été supprimé. [Sockets acceptés](agents/boundary/resources.md#accepted-sockets)

### État par connexion via des contextes typés

Un contexte Extended peut conserver une référence à l’état de la connexion pendant un envoi. Cet exemple nécessite `NotifySucceed` activé et `IOSQE_CQE_SKIP_SUCCESS` désactivé pour qu’un `Send` réussi produise la CQE qui permet de rendre le contexte au pool.

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

À la réception de la CQE `SEND` correspondante, copiez la référence à la connexion et le numéro de séquence avant de rendre le contexte au pool.

```go
ext := cqe.ExtSQE()
ctx := uring.Ctx1V1Of[ConnState](ext)
conn := ctx.Ref1
seq := ctx.Val1
ring.PutExtSQE(ext)
```

On maintient les racines de pointeurs Go actives accessibles en dehors de `UserData`. Le GC ne trace pas ces octets bruts. Le jeu de racines sidecar rattaché à chaque slot `ExtSQE` s'en charge pour les protocoles internes multishot et d'écouteur, mais le code d'exécution appelant qui place des références typées doit les garder accessibles de manière indépendante.

### Composition de délais

`LinkTimeout` fixe une échéance pour la SQE précédente marquée `IOSQE_IO_LINK`. Sur un ring démarré et géré par un seul propriétaire, vérifiez la capacité puis soumettez les deux SQE à la suite, avant toute autre soumission ou tout appel à `Wait`. Le nombre renvoyé ci-dessous indique combien de requêtes restent à traiter en complétion. Utilisez un tampon de réception non vide et gardez son adresse stable jusqu’à la CQE de réception.

```go
func queueReceiveWithTimeout(ring *uring.Uring, fd iofd.FD, buf []byte, d time.Duration) (int, error) {
	if ring.SQAvailable() < 2 {
		return 0, iox.ErrWouldBlock
	}
	recvCtx := uring.PackDirect(uring.IORING_OP_RECV, uring.IOSQE_IO_LINK, 0, int32(fd))
	if err := ring.ReceiveFD(recvCtx, fd, buf); err != nil {
		return 0, err
	}
	timeoutCtx := uring.PackDirect(uring.IORING_OP_LINK_TIMEOUT, 0, 0, 0)
	if err := ring.LinkTimeout(timeoutCtx, d); err != nil {
		return 1, err
	}
	return 2, nil
}
```

Si `iox.ErrWouldBlock` est renvoyé sans aucune admission, faites progresser le ring avant de réessayer la paire. Si seule la réception est en file, transmettez-la au noyau avant toute autre soumission et conservez `buf` jusqu’à sa complétion. Si les deux requêtes sont en file, relevez les deux CQE. À l’échéance, le noyau tente d’annuler la réception. Sa propre CQE en donne le résultat. [Exemple de délai lié](./examples/timeout_linux_test.go#L259)

## Parcours TCP courants

Les parcours les plus concis, à lire en regard des tests :

| Scénario     | API principales                                                  | Référence                                                                         |
|--------------|------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| Serveur echo | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| Client       | `TCP4Socket`, `Connect`, `Send`, `Receive`                       | `socket_integration_linux_test.go`                                                |

### Serveur echo TCP

`ListenerManager` soumet la requête SOCKET initiale. Transmettez chaque CQE de configuration à `listenerOp.HandleCQE` pour invoquer les callbacks et soumettre BIND et LISTEN automatiquement. En cas de retour `iox.ErrWouldBlock`, faites progresser l'anneau, puis réessayez le SQE préparé avec `ring.SubmitExtended(uring.PackExtended(listenerOp.Ext()))` ; ne traitez pas de nouveau le CQE. Commencez à accepter les connexions après confirmation du succès par `OnListening`.

```go
acceptSub, err := listenerOp.AcceptMultishot(acceptHandler)
if err != nil {
	return err
}
```

Conservez `acceptSub` dans la boucle qui distribue les CQE du socket d’écoute. Pour chaque connexion acceptée, son propriétaire conserve `iofd.FD(step.CQE.Res)` dans `clientFD`. Si la soumission de la réception renvoie `iox.ErrWouldBlock`, gardez ce FD et réessayez uniquement la réception. Fermez le FD si vous abandonnez la connexion.

```go
recvCtx := uring.ForFD(int32(clientFD)).WithBufGroup(readGroup)
recvSub, err := ring.ReceiveMultishot(recvCtx, recvHandler)
if err != nil {
	return err
}
```

Pour arrêter une souscription, appelez `Cancel`, puis poursuivez le dispatch jusqu'au traitement de sa CQE terminale et de la CQE d'annulation, si celle-ci a été soumise. Drainez chaque souscription admise avant de fermer son descripteur ou d’arrêter le ring. [L'exemple d'écouteur](./listener_example_test.go) montre la configuration et la fermeture complètes ; les exemples [multishot](./examples/multishot_test.go) et [echo](./examples/echo_test.go) couvrent le flux de connexion.

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

Pour le `Send` de cette séquence, activez `NotifySucceed` et laissez `IOSQE_CQE_SKIP_SUCCESS` désactivé. Relevez la CQE correspondante avec `Wait` avant chaque opération qui dépend de son résultat. Conservez les données jusqu’à la complétion et, après un envoi partiel, reprenez uniquement les octets restants. [Client TCP](./examples/socket_test.go) · [Exemples d’écho](./examples/echo_test.go)

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
- La configuration par défaut `MultiIssuers=false` demande `SINGLE_ISSUER` + `DEFER_TASKRUN` et évite la synchronisation des soumissions partagées. Le propriétaire doit appeler `runtime.LockOSThread` avant `Start`, rester attaché au même thread jusqu'à `Stop` et sérialiser les soumissions, les attentes, le redimensionnement et la libération du ring. Choisissez explicitement `MultiIssuers=true` (`COOP_TASKRUN`) pour partager les soumissions ou permettre la migration entre threads du système.
- `EpollWait` exige que `timeout` vaille `0` ; utilisez `LinkTimeout` si vous avez besoin d'une échéance.
- Restituez les contextes au pool seulement après le traitement de toutes les complétions de la requête et la fin des accès empruntés.
- `ListenerOp.Close` ferme le FD de l'écouteur immédiatement. Si un CQE de mise en place est encore en attente, drainez-le d'abord, puis rappelez `Close` pour restituer le `ExtSQE` emprunté au pool.

## Support de plateforme

L’implémentation qui utilise le noyau nécessite Go 1.27+ et Linux 6.18+. La version Darwin inclut un simulateur en espace utilisateur pour une partie de l’API commune. L’intégration continue compile les cibles Darwin et vérifie le comportement du noyau sur Linux natif.

## Licence

MIT, voir [LICENSE](./LICENSE).

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)

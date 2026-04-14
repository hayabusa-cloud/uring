# uring

[![Go Reference](https://pkg.go.dev/badge/code.hybscloud.com/uring.svg)](https://pkg.go.dev/code.hybscloud.com/uring)
[![Go Report Card](https://goreportcard.com/badge/github.com/hayabusa-cloud/uring)](https://goreportcard.com/report/github.com/hayabusa-cloud/uring)
[![Codecov](https://codecov.io/gh/hayabusa-cloud/uring/graph/badge.svg)](https://codecov.io/gh/hayabusa-cloud/uring)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Paquete Go que expone la interfaz de `io_uring` frente al kernel en Linux 6.18+.

Idioma: [English](./README.md) | [简体中文](./README.zh-CN.md) | **Español** | [日本語](./README.ja.md) | [Français](./README.fr.md)

## Descripción general

`uring` es el paquete del workspace que expone la interfaz de `io_uring` frente al kernel de Linux. Se encarga de crear
y arrancar rings, preparar SQE, decodificar CQE, transportar la identidad de envío a través de `user_data`, y ofrecer
registro de buffers, operaciones multishot y primitivas de configuración de listeners.

El diseño sigue un principio de frontera explícita: la mecánica orientada al kernel y los hechos observables de
completado permanecen en el borde de la API, mientras que la política y la composición quedan por encima de esa
frontera.

Las superficies principales son:

- `Uring`, el handle del ring activo y su conjunto de operaciones
- `SQEContext`, la identidad de envío transportada en `user_data`
- `CQEView`, la vista prestada de completado que devuelve `Wait`
- provisión de buffers mediante buffers registrados y grupos de buffers de varios tamaños

## Instalación

`uring` requiere Linux 6.18 o posterior. Compruebe primero la versión del kernel en ejecución:

```bash
uname -r
```

En Debian 13, la rama estable del kernel puede estar aún por debajo de esa línea base. Consulte la sección de
actualización de kernel en Debian 13 si necesita instalar el kernel más reciente empaquetado por Debian que cumpla el
requisito de 6.18.

```bash
go get code.hybscloud.com/uring
```

### Actualización de kernel en Debian 13

La rama estable de Debian 13 incluye el kernel 6.12. La suite `trixie-backports` proporciona un kernel 6.18+ empaquetado
por Debian. Consulte [SETUP.md](./SETUP.md) para las instrucciones paso a paso.

### Resolución de problemas

La creación del ring puede devolver `ENOMEM`, `EPERM` o `ENOSYS` según los límites de memlock, la configuración de
sysctl o el soporte del kernel. Los runtimes de contenedores bloquean las llamadas al sistema de `io_uring` por defecto.
Consulte [SETUP.md](./SETUP.md) para el diagnóstico y la resolución.

## Ciclo de vida del ring

`New` devuelve un ring sin iniciar. Antes de enviar operaciones es necesario llamar a `Start`. `Start` registra los
recursos del ring y lo habilita; `New`, por su parte, construye los pools de contexto de forma anticipada. En Linux,
`uring` asume la línea base fija de 6.18+ y no mantiene ramas de fallback para versiones anteriores.

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

`Wait` vacía los envíos pendientes antes de recoger completados. En rings de emisor único, también realiza el enter del
kernel necesario para que el deferred task work avance una vez vaciada la SQ; el llamador debe serializar `Wait`/`enter`
con las operaciones de submit-state. `iox.ErrWouldBlock` indica que no hay ningún completado observable en el límite
actual. Este error está definido en `code.hybscloud.com/iox`.

`Start` y `Stop` forman el par de ciclo de vida del ring. `Stop` es
idempotente y deja el ring permanentemente inutilizable, por lo que solo
debe llamarse tras drenar todas las operaciones en vuelo, recoger los CQE
pendientes y detener las suscripciones multishot activas.

## Tipos y operaciones

| Tipo | Papel |
|------|-------|
| `Uring` | Inicialización del anillo, envío, recolección de completados y métodos de operación |
| `Options` | Entradas del anillo, presupuesto de buffers registrados, escala de grupos de buffers y visibilidad de completados |
| `SQEContext` | Identidad compacta de envío almacenada en `user_data` |
| `CQEView` | Registro prestado de completado con accesores para contexto decodificado |
| `ListenerOp` | Handle de una operación de creación de listener con FD y helpers de accept |
| `BundleIterator` | Itera sobre buffers consumidos en una recepción bundle |
| `IncrementalReceiver` | Gestiona recepciones incrementales de buffer-ring (`IOU_PBUF_RING_INC`) |
| `ZCTracker` | Rastrea el ciclo de vida de dos CQEs del envío zero-copy |
| `ContextPools` | Pools para contextos de envío indirectos y extendidos |
| `ZCRXReceiver` | Ciclo de vida de recepción zero-copy sobre una cola RX de NIC |
| `ZCRXConfig` | Configuración de una instancia de recepción ZCRX |
| `ZCRXHandler` | Interfaz de callback para datos, errores y cierre ZCRX |
| `ZCRXBuffer` | Vista de recepción zero-copy entregada, con reposición del kernel al liberar |

Operaciones:

| Área | Métodos |
|------|---------|
| Socket | `TCP4Socket`, `TCP6Socket`, `UDP4Socket`, `UDP6Socket`, `UDPLITE4Socket`, `UDPLITE6Socket`, `SCTP4Socket`, `SCTP6Socket`, `UnixSocket`, `SocketRaw`, más variantes `*Direct` |
| Conexión | `Bind`, `Listen`, `Accept`, `AcceptDirect`, `Connect`, `Shutdown` |
| Socket I/O | `Receive`, `Send`, `RecvMsg`, `SendMsg`, `ReceiveBundle`, `ReceiveZeroCopy`, `Multicast`, `MulticastZeroCopy` |
| Multishot | `AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot`, `SubmitReceiveBundleMultishot` |
| Archivo I/O | `Read`, `Write`, `ReadV`, `WriteV`, `ReadFixed`, `WriteFixed`, `ReadvFixed`, `WritevFixed` |
| Gestión arch. | `OpenAt`, `Close`, `Sync`, `Fallocate`, `FTruncate`, `Statx`, `RenameAt`, `UnlinkAt`, `MkdirAt`, `SymlinkAt`, `LinkAt` |
| Xattr | `FGetXattr`, `FSetXattr`, `GetXattr`, `SetXattr` |
| Transferencia | `Splice`, `Tee`, `Pipe`, `SyncFileRange`, `FileAdvise` |
| Timeout | `Timeout`, `TimeoutRemove`, `TimeoutUpdate`, `LinkTimeout` |
| Cancelación | `AsyncCancel`, `AsyncCancelFD`, `AsyncCancelOpcode`, `AsyncCancelAny`, `AsyncCancelAll` |
| Poll | `PollAdd`, `PollRemove`, `PollUpdate`, `PollAddLevel`, `PollAddMultishot`, `PollAddMultishotLevel` |
| Async | `EpollWait`, `FutexWait`, `FutexWake`, `FutexWaitV`, `Waitid` |
| Ring msg | `MsgRing`, `MsgRingFD`, `FixedFdInstall`, `FilesUpdate` |
| Cmd | `UringCmd`, `UringCmd128`, `Nop`, `Nop128` |

`Nop128` y `UringCmd128` requieren un ring creado con `Options.SQE128`, y el kernel debe anunciar soporte para los
opcodes correspondientes. De lo contrario, devuelven `ErrNotSupported`.

`Uring.Close` envía `IORING_OP_CLOSE` sobre un descriptor de archivo destino. No es un método de desmontaje del ring.

## Transporte de contexto

`SQEContext` es el token de identidad principal en `uring`. En modo directo, empaqueta el opcode, las flags del SQE, el
identificador de grupo de buffers y el descriptor de archivo en un único valor de 64 bits.

```go
sqeCtx := uring.ForFD(fd).
    WithOp(uring.IORING_OP_RECV).
    WithBufGroup(groupID)
```

Los tres modos de contexto son:

| Modo | Representación | Uso típico |
|------|----------------|-----------|
| Direct | Carga inline de 64 bits | Ruta común de envío y recolección, sin asignaciones |
| Indirect | Puntero a `IndirectSQE` | Cuando 64 bits no bastan para el SQE completo |
| Extended | Puntero a `ExtSQE` | SQE completo más 64 bytes de datos de usuario |

En la ruta habitual, parta de `ForFD` o `PackDirect` y añada solo los bits que desee volver a observar tras el
completado. `WithFlags` reemplaza el conjunto completo de flags, por lo que conviene calcular la unión antes de
invocarlo.

Cuando se necesiten metadatos del llamador que no caben en el layout directo de 64 bits, tome prestado un `ExtSQE`,
escriba en su `UserData` mediante `Ctx*Of` o `ViewCtx*`, y vuelva a empaquetarlo como `SQEContext`. Es preferible usar
payloads escalares. Si un overlay raw o una vista tipada almacena punteros de Go, interfaces, valores func, slices,
strings, maps, chans o structs que los contengan, mantenga las raíces vivas fuera de `UserData`, ya que el GC no rastrea
esos bytes raw.

```go
ext := ring.ExtSQE()
meta := uring.CtxV1Of(ext)
meta.Val1 = requestSeq

sqeCtx := uring.PackExtended(ext)
fmt.Printf("sqe context mode=%d seq=%d\n", sqeCtx.Mode(), meta.Val1)
```

`NewContextPools` devuelve pools listos para usar. Llame a `Reset` solo después de haber devuelto todos los contextos
prestados y cuando desee reutilizar el conjunto de pools.

### Despacho de completados con `CQEView`

`uring` no expone un tipo de contexto de completado separado. Todo el despacho de completados pasa por `CQEView`;
invoque `cqe.Context()` cuando necesite recuperar el token de envío original.

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

Al completarse la operación, `CQEView` decodifica el modo de contexto correspondiente bajo demanda. `CQEView`,
`IndirectSQE`, `ExtSQE` y los buffers prestados no deben sobrevivir más allá de su tiempo de vida documentado.

## Provisión de buffers

`uring` ofrece dos estrategias de buffers de recepción:

- buffers provistos de tamaño fijo mediante `ReadBufferSize` y `ReadBufferNum`
- grupos de buffers de varios tamaños mediante `MultiSizeBuffer`

En la mayoría de los sistemas, los helpers de configuración ofrecen un punto de partida directo:

```go
opts := uring.OptionsForSystem(uring.MachineMemory4GB)
ring, err := uring.New(func(o *uring.Options) {
    *o = opts
})
```

Use `OptionsForBudget` para partir de un presupuesto de memoria explícito, y `BufferConfigForBudget` para inspeccionar
la distribución por niveles elegida para dicho presupuesto.

Los buffers registrados requieren memoria fijada (pinned). Si el registro de buffers grandes falla, aumente
`RLIMIT_MEMLOCK` o reduzca el presupuesto.

## Operaciones multishot y de listener

`AcceptMultishot`, `ReceiveMultishot`, `SubmitAcceptMultishot`, `SubmitAcceptDirectMultishot`, `SubmitReceiveMultishot`
y `SubmitReceiveBundleMultishot` envían operaciones de socket multishot.

`uring` deja fuera del paquete la política de enrutamiento de CQE. La configuración del listener avanza a través de
`DecodeListenerCQE`, `PrepareListenerBind`, `PrepareListenerListen` y `SetListenerReady`; es el llamador quien decide
cómo se despachan los completados y cuándo se detiene la cadena.

## Arquitectura de implementación

La frontera de implementación se define así:

1. `New` construye un ring del kernel deshabilitado, crea los pools de contexto y elige la estrategia de buffers.
2. `Start` registra los buffers y habilita el ring para la línea base fija de Linux 6.18+.
3. Los métodos de operación declaran intención escribiendo SQE.
4. `Wait` vacía los envíos y devuelve observaciones prestadas de CQE.
5. Las capas superiores deciden planificación, reintentos, parking y orquestación.

De este modo, `uring` se mantiene centrado en la mecánica frente al kernel y preserva el significado de los completados
a través de la frontera.

## Patrones para la capa de aplicación

`uring` expone los mecanismos orientados al kernel; la planificación, los reintentos, el seguimiento de conexiones y la
interpretación del protocolo corresponden a las capas superiores. Los patrones siguientes muestran formas habituales de
estructurar esa capa superior.

### Bucle de eventos propietario del ring

En modo single-issuer (el predeterminado), una goroutine serializa todas las operaciones de submit. Un bucle típico
emite trabajo pendiente, aplica un `iox.Backoff` propiedad del llamador cuando `Wait` no informa progreso observable y
despacha las finalizaciones:

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

Todos los métodos del ring, incluidos `Send`, `Receive`, `AcceptMultishot` y `Wait`, se ejecutan en esta goroutine. El
trabajo procedente de otras goroutines entra en el bucle a través de un canal o una cola lock-free; no se deben invocar
los métodos del ring directamente. `iox.Backoff` sigue siendo propiedad del llamador: use `backoff.Wait()` cuando `Wait`
devuelva `iox.ErrWouldBlock` o no recoja ningún CQE, y `backoff.Reset()` tras cualquier lote con `n > 0`.

### Ciclo de vida de suscripciones multishot

Una operación multishot genera un flujo de CQEs hasta que el kernel envía uno final (sin `IORING_CQE_F_MORE`). La capa
de framework rastrea las suscripciones y gestiona la reemisión:

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
OnStop(func (err error, cancelled bool) {
if !cancelled {
resubscribeAccept()
}
})

_, err := ring.AcceptMultishot(acceptCtx, handler.Handler())
```

`OnMultishotStep` observa cada finalización; devuelva `MultishotContinue` para mantener el flujo o `MultishotStop` para
solicitar la cancelación. `OnMultishotStop` se ejecuta una vez en el estado terminal. Úselo para limpieza y
resuscripción condicional.

### Estado por conexión con contextos tipados

Los contextos extendidos transportan referencias por conexión a lo largo del ciclo completo submit → complete, sin
necesidad de una tabla de búsqueda global:

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

En el momento de la finalización, se recupera el estado a través de la misma vista tipada:

```go
ext := cqe.ExtSQE()
ctx := uring.Ctx1V1Of[ConnState](ext)
conn := ctx.Ref1
seq := ctx.Val1
ring.PutExtSQE(ext)
```

Mantenga las raíces de punteros Go activas accesibles fuera de `UserData`. El GC no rastrea esos bytes crudos. El
conjunto de raíces sidecar adjunto a cada slot `ExtSQE` se encarga de esto para los protocolos internos multishot y
listener, pero el código de framework que coloca refs tipados debe mantenerlos accesibles de forma independiente.

### Composición de plazos

`LinkTimeout` adjunta un plazo al SQE anterior a través de una cadena `IOSQE_IO_LINK`. La operación y el timeout
compiten: exactamente uno se completa y el otro se cancela.

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

La capa de framework maneja ambos resultados: una recepción exitosa cancela el timeout, y un timeout disparado cancela
la recepción. Ambos producen CQEs que el bucle de despacho debe observar.

## Patrones de uso en TCP

Los siguientes son los flujos más cortos, pensados para leer junto con los tests:

| Escenario | API principales | Referencia |
|-----------|-----------------|------------|
| Servidor echo | `ListenerManager`, `AcceptMultishot`, `ReceiveMultishot`, `Send` | `listener_example_test.go`, `examples/multishot_test.go`, `examples/echo_test.go` |
| Cliente | `TCP4Socket`, `Connect`, `Send`, `Receive` | `socket_integration_test.go` |

### Servidor echo TCP

Use `ListenerManager` para que el paquete prepare la cadena socket → bind → listen; a continuación, arranque multishot
accept y multishot receive sobre los FD de conexión activos.

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

`listener_example_test.go` cubre la preparación del listener y el accept multishot, `examples/multishot_test.go` muestra
los CQE del lado del handler en multishot receive, y `examples/echo_test.go` ilustra el flujo echo completo sobre
loopback.

### Cliente TCP

Cree el socket, espere el completado de `IORING_OP_SOCKET` y convierta el FD devuelto en un `iofd.FD` para usarlo con
`Connect`, `Send` y `Receive`.

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

Reutilice el bucle `Wait` de la sección de ciclo de vida del ring tras cada envío para observar el completado
correspondiente. El archivo `socket_integration_test.go` cubre el flujo de connect/send.

## Recepción zero-copy (ZCRX)

`ZCRXReceiver` gestiona la recepción zero-copy desde una cola RX de hardware de NIC mediante `io_uring`.

`NewZCRXReceiver` requiere un ring creado con CQE de 32 bytes (`IORING_SETUP_CQE32`). La superficie actual de `Options`
no expone ese flag de configuración, de modo que los rings creados por la ruta estándar de `New` provocan que este
constructor devuelva `ErrNotSupported`.

### Ciclo de vida

1. Sobre un ring con CQE de 32 bytes, cree el receptor con `NewZCRXReceiver`. El constructor registra la cola de
   interfaz ZCRX, mapea el área de refill y prepara el refill ring.
2. Llame a `Start` para enviar la operación extendida `RECV_ZC` en el ring.
3. En la ruta de despacho de CQE, los completados ZCRX se enrutan al `ZCRXHandler`:
   - `OnData` entrega un `ZCRXBuffer` que apunta al área mapeada por la NIC. Llame a `Release` al terminar para reponer
     el slot ante el kernel. Devuelva `false` para solicitar una parada de mejor esfuerzo.
   - `OnError` entrega errores de CQE. Devuelva `false` para solicitar una parada de mejor esfuerzo.
   - `OnStopped` se ejecuta una vez durante la retirada terminal, antes de que el estado pase a `Stopped`.
4. Llame a `Stop` para enviar un async cancel. El receptor transita por `Stopping` → `Retiring` → `Stopped`.
5. Consulte `Stopped` hasta que devuelva `true`, detenga el ring propietario y llame entonces a `Close` para liberar el
   área mapeada y el mapeo del refill ring.

### Máquina de estados

```
Idle → Active → Stopping → Retiring → Stopped
```

`Stop` revierte a `Active` si el envío de cancelación falla. `Close` es idempotente.

### Contrato del handler

- `OnData` y `OnError` se invocan en serie desde el goroutine de despacho de CQE.
- `Release` es de productor único; llámelo exclusivamente desde el goroutine de despacho.
- `Stop` solo debe llamarse cuando se garantice que no hay concurrencia con el despacho de CQE. Se trata de un contrato
  de serialización del lado del llamador.

## Ejemplos

Los tests de ejemplo en `uring/examples/` ilustran la API en la práctica.

- `multishot_test.go`, accept multishot, receive multishot y parada de suscripciones
- `file_io_test.go`, lecturas, escrituras y batching de archivos
- `fixed_buffers_test.go`, buffers registrados e I/O con buffers fijos
- `vectored_io_test.go`, operaciones de lectura y escritura vectorizadas
- `splice_tee_test.go`, transferencia de datos zero-copy con splice y tee
- `zerocopy_test.go`, rutas de envío zero-copy y seguimiento de completados
- `poll_test.go`, flujos de preparación basados en poll
- `buffer_ring_test.go`, provisión de buffer rings y grupos de buffers de varios tamaños
- `context_test.go`, flujos `SQEContext` direct, indirect y extended, con acceso desde `CQEView`
- `echo_test.go`, flujos de servidor echo TCP y UDP ping-pong
- `timeout_test.go`, operaciones de timeout y linked-timeout

A nivel de paquete, `listener_example_test.go` cubre la creación de listeners con accept multishot, y
`socket_integration_test.go` cubre el flujo del cliente TCP de connect/send.

## Notas operativas

- Active `NotifySucceed` cuando necesite un CQE visible por cada operación exitosa.
- `ring.Features` informa de las entradas reales de SQ y CQ, el ancho de la ranura SQE y el orden de bytes que usa este
  paquete al interpretar `user_data`.
- Deje `MultiIssuers` desactivado en la configuración predeterminada de emisor único (`SINGLE_ISSUER` +
  `DEFER_TASKRUN`), en la que una sola ruta de ejecución del llamador serializa las operaciones de submit-state (
  `submit`, `Wait`/`enter`, `Stop` y resize). Actívelo solo cuando varios goroutines necesiten envío concurrente o enter
  del lado wait; esto conmuta el ring a la configuración de envío compartido con `COOP_TASKRUN`.
- `EpollWait` requiere que `timeout` sea `0`; use `LinkTimeout` cuando necesite un plazo.
- Las vistas prestadas de completado y los contextos en pool deben liberarse o descartarse con prontitud.
- `ListenerOp.Close` cierra el FD del listener de inmediato. Si aún hay un CQE de configuración pendiente, drene ese CQE
  y vuelva a llamar a `Close` para devolver el `ExtSQE` prestado al pool.

## Soporte de plataforma

`uring` apunta a Go 1.26+ y Linux 6.18+ en la ruta real respaldada por el
kernel. La mayoría de los archivos de implementación y tests de ejemplo
están protegidos con `//go:build linux`. Los archivos de Darwin proporcionan
solo stubs de compilación para la superficie compartida; las capacidades
exclusivas de Linux siguen siendo exclusivas de Linux y no alteran la línea
base de ejecución en Linux descrita arriba.

## Licencia

MIT, vea [LICENSE](./LICENSE).

©2026 [Hayabusa Cloud Co., Ltd.](https://code.hybscloud.com/)

# Package Integration

The Linux `uring` package imports seven stack modules. Its module file also requires `framer` for the framed TCP example. Callers choose their own execution and protocol packages. [Package dependencies](../../doc.go) · [Module requirements](../../go.mod) · [Network examples](../../examples/echo_test.go)

| Package | Owns |
|---------|------|
| `uring` | SQE/CQE mechanics, packed request identity, capability exposure, and kernel-facing lifetime facts |
| `iox` | Go I/O interfaces, outcome classification and adaptive backoff. The caller interprets a zero count using the transport and the submitted receive. [Outcome classification](https://github.com/hayabusa-cloud/iox/blob/v0.4.0/semantics.go#L11-L24) · [Datagram receives](protocols.md#datagram-receives) |
| `zcall` | Syscall ABI and errno results |
| `iofd`, `sock` | Descriptor lifecycle, socket and address types |
| `iobuf` | Buffer storage, views, pool ownership, and recycling |
| `spin` | Spin, yield, and lock primitives |
| `dwcas` | Release barriers used to publish ring entries |
| `framer` | Frame boundaries over byte streams in the example |
| `lfq`, `atomix` | Bounded queues and atomic primitives available to callers implementing a handoff |
| `kont`, `cove`, `takt`, `sess` | Optional suspension, context, execution and endpoint mechanisms. [Runtime roles](../runtime/INDEX.md) |
| `tempo` | Evaluates temporal contracts over caller-supplied events. The caller supplies any timing observations, drives the ring and releases resources. [Receipt policy](../runtime/tempo.md#track-a-write-receipt) · [Target and cancellation events](../recipes/lifecycle.md#request-and-observe) |

## Compose Through Public Contracts

At queue handoff, identify which caller owns the route, parser state, retry cadence and lifecycle. Follow the ring's issuer and CQ-consumer ownership rules across the handoff. [Ring setup](encoding.md#setup-and-capabilities) · [CQ claims](sqe-cqe.md#batch-and-hot-path-obligations)

Choose the smallest context that identifies the request. Keep typed roots for pointer-bearing raw overlays and preserve any borrowed fields needed after handoff. [Completion retention](sqe-cqe.md#completion-retention)

Check the adapter's generated code, escapes, allocations and synchronization against equivalent direct code. [Cost checks](../verify.md#cost)

## Waiting and Backoff

| Wait source | Mechanism and contract |
|-------------|------------------------|
| Caller I/O | Service pending caller work and poll into a nonempty destination. Use `iox.Backoff.Wait()` when the turn advances no caller work and reaps zero CQEs. On progress from either source, call `Reset()` before dispatch. |
| Socket retry | Leave it inside `sock`; no public `sock.Backoff` exists |
| Local contention | Prefer `spin.Wait` or `spin` locks; reread state after failed CAS and preserve pause-before-yield retries |

Contention handling stays with the shared operation. Check ownership, publication order, and concurrency together; caller retry policy does not replace that check. A surrounding lock changes the premises and adds contention cost. Correctness tests alone establish no equivalent CPU or latency cost.

Choose cadence for the progress source. Neither mechanism guarantees progress or fairness, or prevents runtime preemption.

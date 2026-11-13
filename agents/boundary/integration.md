# Package Integration

Use each package's own contracts and mechanisms. Make value and ownership transfers explicit; importing a type does not transfer policy responsibility.

| Package | Owns |
|---------|------|
| `uring` | SQE/CQE mechanics, packed request identity, capability exposure, and kernel-facing lifetime facts |
| `iox` | Operation outcomes, stream/datagram contracts, progress classification, and backoff mechanisms |
| `zcall` | Syscall ABI and errno results |
| `iofd`, `sock` | Descriptor lifecycle, socket and address types |
| `iobuf` | Buffer storage, views, pool ownership, and recycling |
| `spin` | Spin, yield, and lock primitives |
| `lfq`, `atomix`, `dwcas` | Queue handoff and the atomic mechanisms used to implement it |
| `framer` | Frame boundaries and codec operations |
| `kont`, `cove`, `takt`, `sess` | Caller suspension, context, execution, and protocol mechanisms |
| `tempo` | Pure temporal residuals and event observations; no clocks, scheduling or resource release |
| `urex` | Concrete event loops, routes, admission and lifecycle policy above `uring` |

## Compose Through Public Contracts

Read each fact owner's current source. The caller owns queue handoff, routes, parser state, retry cadence, and lifecycle policy. A queue transfers work; it establishes neither issuer affinity nor CQ-consumer ownership.

Choose the smallest sufficient context: Direct carries inline facts; Indirect/Extended borrow caller-allocated or pooled targets. Retain those targets and copy needed facts before mutation, reuse, or release. Pointer-bearing raw overlays require external typed roots.

Check the adapter's generated code, escapes, allocations, and synchronization. Formal equivalence alone establishes no runtime-cost equivalence.

## Waiting and Backoff

| Wait source | Mechanism and contract |
|-------------|------------------------|
| Caller I/O | Prefer `iox.Backoff`: `Wait()` after successful empty ring `Wait` or `iox.ErrWouldBlock`; `Reset()` after reaping `n > 0`, before dispatch |
| Socket retry | Leave it inside `sock`; no public `sock.Backoff` exists |
| Local contention | Prefer `spin.Wait` or `spin` locks; reread state after failed CAS and preserve pause-before-yield retries |

Contention handling stays with the shared operation. Check ownership, publication order, and concurrency together; caller retry policy does not replace that check. A surrounding lock changes the premises and adds contention cost. Correctness tests alone establish no equivalent CPU or latency cost.

Choose cadence for the progress source. Neither mechanism guarantees progress or fairness, or prevents runtime preemption.

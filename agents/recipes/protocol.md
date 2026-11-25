# Protocol Composition

Turn matched I/O into protocol steps while preserving backpressure and lifetimes. Optional roles: [sess](../runtime/sess.md), [cove](../runtime/cove.md), [kont](../runtime/kont.md).

## Establish the Protocol

Declare compatible peer actions, payload ownership, choices, recursion, and termination. Give each endpoint and parser one owner. Select framing where byte streams need message boundaries. Keep network I/O, parsing, local transport, and service lifecycle distinct.

`sess` provides bounded local SPSC transport, not an automatic `uring` binding. Define which completion enables which action, which facts it carries, and who retains pending data.

## Drive One Action

1. Match the request and copy required context facts. For a successful ordinary stream `RECV` into `buf` with flags zero, consume `buf[:Res]`. For a partial stream write, keep the unsent suffix.
2. For [framed input](../boundary/protocols.md#stream-framing), keep one decoder per ordered stream. At each `framer.Decoder.Decode`, advance by `consumed` and handle the returned chunk even on `ErrMore`. Accumulate payload until `Chunk.End` for a whole-message endpoint. Copy borrowed bytes or retain their backing through the last use. [Decoder contract](https://github.com/hayabusa-cloud/framer/blob/v0.1.7/decoder.go#L59-L75)
3. Check the current route, buffer ownership, descriptor, protocol phase and shutdown state as needed before each endpoint attempt.
4. On local `ErrWouldBlock`, keep the same pending action, payload and unread input suffix. Retry the action after peer progress. A successful `sess.Advance` consumes the suspension. Keep its successor and continue with the unread bytes. [Session step](https://github.com/hayabusa-cloud/sess/blob/v0.1.7/step.go#L17-L36)

Use `cove` transition and step-index checks when the property requires them, preserving the [strict pattern's premises](../runtime/cove.md#strict-verification-pattern). Failed guards, invalid transitions, insufficient fuel, and unfinished traces require caller handling, not invented kernel errors.

## Bound Backpressure and Failure

Name finite queues and their progress sources; bound payload retention under slow peers/services. Protocol compatibility does not supply scheduling or peer progress.

Handle peer failure and paired-execution errors before interpreting unresolved results. Endpoint close, parsed frames, and continuation disposal leave outstanding I/O independent. Follow [lifecycle](lifecycle.md) before releasing descriptors/buffers.

## Validate

[Network examples](../../examples/echo_test.go) cover TCP/UDP and framed broadcast, not `sess`/`cove` integration proofs. Validate adapters against selected module revisions and [runtime contracts](../runtime/INDEX.md).

Check a frame split across receives, several frames in one receive, an empty frame, and EOF inside a header or body. Fill the endpoint between two frames and check that the peer receives each message once and in order. Also check context changes during suspension, peer failure and close with outstanding I/O. Name the scheduler, fairness and peer assumptions for progress claims.

# Protocol Composition

Turn matched I/O into protocol steps while preserving backpressure and lifetimes. Optional roles: [sess](../runtime/sess.md), [cove](../runtime/cove.md), [kont](../runtime/kont.md).

## Establish the Protocol

Declare compatible peer actions, payload ownership, choices, recursion, and termination. Give each endpoint and parser one owner. Select framing where byte streams need message boundaries. Keep network I/O, parsing, local transport, and service lifecycle distinct.

`sess` provides bounded local SPSC transport, not an automatic `uring` binding. Define which completion enables which action, which facts it carries, and who retains pending data.

## Drive One Action

1. Match the request and copy required context facts.
2. Consume reported progress, including partial results; retain parser input/output suffixes.
3. Check current route generation, buffer ownership, descriptor state, protocol phase, and shutdown epoch as needed. Retaining context performs no automatic checks.
4. Attempt the selected endpoint action. On local `ErrWouldBlock`, retain that unconsumed action and payload; drive the peer or wait. Local transport does not admit `ErrMore`.
5. On success, retain the successor and transfer/release only resources consumed by the step.

Use `cove` transition and step-index checks when the property requires them, preserving the [strict pattern's premises](../runtime/cove.md#strict-verification-pattern). Failed guards, invalid transitions, insufficient fuel, and unfinished traces require caller handling, not invented kernel errors.

## Bound Backpressure and Failure

Name finite queues and their progress sources; bound payload retention under slow peers/services. Protocol compatibility does not supply scheduling or peer progress.

Handle peer failure and paired-execution errors before interpreting unresolved results. Endpoint close, parsed frames, and continuation disposal leave outstanding I/O independent. Follow [lifecycle](lifecycle.md) before releasing descriptors/buffers.

## Validate

[Network examples](../../examples/echo_test.go) cover TCP/UDP and framed broadcast, not `sess`/`cove` integration proofs. Validate adapters against selected module revisions and [runtime contracts](../runtime/INDEX.md).

Check would-block without replay, partial frames, context changes during suspension, peer failure, exhausted capacity, and close with outstanding I/O. Name scheduler, fairness, peer, and failure assumptions for progress claims.

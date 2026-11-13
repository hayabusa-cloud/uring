# Protocol Boundary

The caller interprets `uring` observations as protocol events and owns parsing, routes, endpoint transitions, retry, timeout, and service policy.

## Map Completions to Events

Copy result, flags, identity, buffer metadata, and needed context facts while valid. Match the current request instance before constructing an event. Check these facts independently:

| Fact | Establish |
|------|-----------|
| Result | Whether this step succeeded |
| Request liveness | Whether another observation can follow |
| Release | Which resources may be reused |
| Protocol | Which transition is permitted |

I/O success alone establishes neither peer processing nor protocol completion.

## Handle One Event

A handler consumes one event, may update caller parser/protocol state, and returns control. An explicit caller loop owns future events.

Keep zero-progress behavior explicit: `ErrWouldBlock` does not advance the protocol; `MORE` does not make failure successful. Define how kernel observations enable endpoint operations under their own transport contracts.

## Go Handlers

For allocation-free dispatch, build tables before the hot loop. Capture durable facts or caller-owned state; copy needed Indirect/Extended fields before target mutation, reuse, or release. Retaining a closure preserves neither old target fields nor storage lifetime.

Use framing contracts for message boundaries and subscriptions for multishot work. A one-shot continuation cannot be resumed repeatedly.

# Protocol Boundary

The caller interprets `uring` observations as protocol events and owns parsing, routes, endpoint transitions, retry, timeout, and service policy.

## Map Completions to Events

Match the completion's origin before choosing a protocol event. Copy the flags, buffer metadata and context fields the event needs while they are valid. [Completion origin](sqe-cqe.md#completion-origin) · [Request identity](sqe-cqe.md#request-identity)

| Fact | Establish |
|------|-----------|
| Result | The value carried by this source. A send notification or registration tag can release a resource without supplying stream bytes. |
| Request liveness | Whether another observation can follow |
| Release | Which resources may be reused |
| Protocol | Which transition is permitted |

Peer processing requires evidence from the protocol.

## Stream Framing

Feed the decoder bytes in stream order. A caller can do this by decoding each ordinary receive before issuing the next on that socket. A frame can span several receives, and one receive can contain several frames. `IORING_CQE_F_MORE` describes the request. The decoder identifies frame boundaries. A whole-message endpoint advances when it accepts the completed payload. [Linux I/O ordering](https://man7.org/linux/man-pages/man7/io_uring.7.html) · [Protocol recipe](../recipes/protocol.md#drive-one-action)

For an ordinary positive-capacity stream receive with flags zero, a zero-byte result signals orderly EOF. Process all preceding bytes before calling `framer.Decoder.Finish`. It returns nil at a frame boundary and `io.ErrUnexpectedEOF` inside a header or body. An encoded empty frame delivers one message. [Decoder and EOF contract](https://github.com/hayabusa-cloud/framer/blob/v0.1.7/decoder.go#L136-L152)

## Handle One Event

A handler consumes one event, may update caller parser/protocol state, and returns control. An explicit caller loop owns future events.

On endpoint `ErrWouldBlock`, retain the pending action for the next attempt. Interpret a failed request with `MORE` under the [completion contract](sqe-cqe.md#observation).

## Go Handlers

For allocation-free dispatch, build tables before the hot loop. Capture durable facts or caller-owned state; copy needed Indirect/Extended fields before target mutation, reuse, or release. Retaining a closure preserves neither old target fields nor storage lifetime.

Use a subscription for multishot work. Each one-shot continuation can be resumed once.

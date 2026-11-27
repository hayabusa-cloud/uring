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

For an ordinary stream receive, process a short positive result even with `MSG_WAITALL`. With the local read side open, a zero result signals EOF when the request asked for at least one byte. A zero-length request can return zero while data is queued. [Receive shape](encoding.md#receive-shape) · [Linux recv(2)](https://man7.org/linux/man-pages/man2/recv.2.html#RETURN_VALUE)

At EOF, process all preceding bytes before calling `framer.Decoder.Finish`. A live decoder returns nil at a frame boundary and `io.ErrUnexpectedEOF` inside a header or body. An encoded empty frame delivers one message. [Decoder and EOF contract](https://github.com/hayabusa-cloud/framer/blob/v0.1.7/decoder.go#L136-L152)

## Datagram Receives

An ordinary UDP receive consumes one datagram when receive coalescing (`UDP_GRO`) is disabled. Given a three-byte buffer, `ReceiveFD` can return the same count and bytes for `ABC` and `ABCD`. Linux discards the excess byte. When completeness matters and the payload can exceed the buffer, use one-shot `RecvMsg` and check `Msghdr.Flags & MSG_TRUNC` after a matched success. Read `MSG_CTRUNC` separately if the protocol needs ancillary data. With room for payload and the read side open, a zero result delivers an empty datagram. [Linux udp(7)](https://man7.org/linux/man-pages/man7/udp.7.html) · [Linux recvmsg(2)](https://man7.org/linux/man-pages/man2/recvmsg.2.html)

With `ReceiveMultishot` and ordinary provided buffers, that empty datagram can end the native receive. A matched CQE with `Res == 0` and `F_MORE` clear still delivers one message, even when `F_BUFFER` is clear. Check the buffer flags for any slot to return. `MultishotContinue` applies only while `F_MORE` is set. To keep reading after the final callback, submit a new receive. [Linux multishot RECV](https://github.com/torvalds/linux/blob/v6.18/io_uring/net.c#L1226-L1236) · [Callback actions](../../multishot.go#L22-L28) · [Provided buffers](resources.md#provided-buffers)

When reusing a `RecvMsg` header, set `Namelen` and `Controllen` to the capacities offered for this receive. After completion, `Namelen` reports the source address's actual length, which can exceed the space offered. Read only the bytes that fit. `Controllen` reports the control bytes returned. Reusing a returned zero offers no space for the next packet's metadata, even when the backing array is large enough. Finish using or copy the returned metadata before rearming the header. [RecvMsg](../../interface_linux.go#L955) · [Linux socket copyback](https://github.com/torvalds/linux/blob/v6.18/net/socket.c)

## Handle One Event

A handler consumes one event, may update caller parser/protocol state, and returns control. An explicit caller loop owns future events.

On endpoint `ErrWouldBlock`, retain the pending action for the next attempt. Interpret a failed request with `MORE` under the [completion contract](sqe-cqe.md#observation).

## Go Handlers

For allocation-free dispatch, build tables before the hot loop. Capture durable facts or caller-owned state; copy needed Indirect/Extended fields before target mutation, reuse, or release. Retaining a closure preserves neither old target fields nor storage lifetime.

Keep a live route for each multishot request through its terminal CQE. A direct loop can handle repeated replies, or a subscription can own their dispatch. Resume each one-shot continuation at most once. [Caller roles](../runtime/INDEX.md)

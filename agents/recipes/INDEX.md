# Recipes

Each recipe follows a caller operation through admission, progress, failure and release.

| Recipe | Goal |
|--------|------|
| [One-shot](oneshot.md) | Submit once, handle SQ pressure and deliver the matched result |
| [Listener](listener.md) | Advance socket, bind, and listen from CQEs |
| [Stream](stream.md) | Dispatch events and retire a subscription |
| [Buffers](buffers.md) | Publish provided buffers, handle incremental or zero-copy I/O, and account for sends to multiple targets |
| [Protocol](protocol.md) | Deliver framed messages or a [write receipt](protocol.md#deliver-a-write-receipt) under endpoint backpressure |
| [Lifecycle](lifecycle.md) | [Track poll and cancellation replies](lifecycle.md#request-and-observe), [update or remove a timer](lifecycle.md#update-or-remove-a-timer) and release each resource after its final use |

Use the recipe's validation cases and linked executable examples for the path you changed. Record feature skips under [verification](../verify.md#evidence).

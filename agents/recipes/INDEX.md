# Recipes

Choose an operation shape and only the runtime roles it needs. Recipes describe caller policy using existing APIs.

| Recipe | Goal |
|--------|------|
| [One-shot](oneshot.md) | Admit and complete once |
| [Listener](listener.md) | Advance socket, bind, and listen from CQEs |
| [Stream](stream.md) | Dispatch events and retire a subscription |
| [Buffers](buffers.md) | Retain storage and release correctly |
| [Protocol](protocol.md) | Advance endpoint actions from I/O facts |
| [Lifecycle](lifecycle.md) | [Track poll and cancellation replies](lifecycle.md#request-and-observe), [update or remove a timer](lifecycle.md#update-or-remove-a-timer) and release each resource after its final use |

Read [core contracts](../core.md), then the recipe's retained state, pending and failure paths, release conditions, and validation cases. Finish with [verification](../verify.md). Linked examples are executable tests, not proofs of every variant; run relevant cases and report skips.

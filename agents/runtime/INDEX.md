# Runtime Roles

<a id="caller-runtime-packages"></a>

The caller sets admission, scheduling and cancellation policy, dispatches each CQE to its owner and releases acquired resources.

| Caller decision | Package | Next action |
|-----------------|---------|-------------|
| How do I continue from one matched reply? | [kont](kont.md) | Resume the pending suspension once and drive its successor, if any. |
| Which action follows from events so far? | [tempo](tempo.md) | Keep each `Step(event)` successor. Read `Observe()` before publishing a receipt or deciding which terminal reply is still due. |
| May I submit or deliver now? | [cove](cove.md) | Check the original route and current grant at each guarded offer. Recheck a pending delivery after a wait. |
| Which request or route owns this reply? | [takt](takt.md) | Match one-shot tokens with `Loop`. Route repeated events with `SubscriptionLoop` using the original token and generation. |
| Can this endpoint take its next action? | [sess](sess.md) | Try the send, receive or choice. On `iox.ErrWouldBlock`, keep its suspension for retry after peer progress. |

Follow one socket write through receipt delivery. Check each contract at the module revision your caller uses. [Write-receipt example](../recipes/protocol.md#deliver-a-write-receipt) · [Package ownership](../boundary/integration.md) · [Source requirements](../verify.md#read-sources)

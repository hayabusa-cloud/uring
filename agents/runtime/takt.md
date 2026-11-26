# Execution and Routing with takt

## Use When

Use `takt` for dispatch, asynchronous one-shot execution, or repeated stream routing. This card follows v0.3.0. In v0.2.0, every backend submission error discarded the reached suspension, including would-block. [v0.2.0 submission](https://github.com/hayabusa-cloud/takt/blob/v0.2.0/loop.go#L195-L200)

## Owns

`takt` advances suspended computations, tracks pending requests and active routes, and stores completions. The backend assigns their identities. Kernel `user_data` can serve as a one-shot token when it meets the [request identity contract](../boundary/sqe-cqe.md#request-identity).

## Inputs and Preconditions

Serialize loop methods. Give each live one-shot submission a unique token. Copy borrowed context fields needed by the reply before recycling their target. Retain referenced payloads through their last use.

## Choose the Execution Surface

| Surface | Contract |
|---------|----------|
| `Step` | Evaluate to a result or the first suspension |
| `Advance`, `AdvanceSuspension` | A nil dispatch error or `ErrMore` resumes the suspension. Would-block or failure returns it unchanged. The dispatcher accounts for any partial progress. [Manual dispatch](https://github.com/hayabusa-cloud/takt/blob/v0.3.0/step.go#L27-L36) |
| `Exec` conveniences | Caller-selected loop with retry/backoff |
| `Loop` | Reserve a slot before evaluating the computation. Return a completed result or queue the reached suspension for `Poll` |
| `SubscriptionLoop` | Each event carries the original route's `RouteID`, including its token and generation |

## Outputs and Ownership

### Drive a Deferred Request

| Where the offer stopped | Who retries |
|-------------------------|-------------|
| A full loop refuses `Submit` or `SubmitExpr` with `iox.ErrWouldBlock` | The caller can resubmit when a slot opens. The loop ran no part of the computation. |
| `Loop.SubmitSuspension` returns an error | The caller keeps the original suspension. On would-block, retry that handle when a slot opens. A nil error transfers it to the loop. |
| `Backend.Submit` returns `iox.ErrWouldBlock` during `Loop.Poll` | The loop keeps the reached suspension. Its driver calls `Poll` again to retry the offer. |

With only deferred work, a healthy loop can report `Pending() > 0` and `Ready() == false`. Arrange a backend wake or retry deadline before parking the ring owner. [Local admission](https://github.com/hayabusa-cloud/takt/blob/v0.3.0/loop.go#L75-L125) · [Readiness and polling](https://github.com/hayabusa-cloud/takt/blob/v0.3.0/loop.go#L135-L148)

### Completion Results

During `Loop.Poll`, a backend submission error means the backend accepted no work for that offer. Submission errors other than would-block abort the loop. Return a valid completion batch with a nil poll error before reporting any subsequent poll failure.

For a successful operation or ordinary operation error, `Completion.ResumeValue()` must supply the suspension's reply, including any operation error. Report completion-level would-block for retry only after the old request has retired without a reply or hidden partial progress. Completion `ErrMore` fails the one-shot loop. [Backend contract](https://github.com/hayabusa-cloud/takt/blob/v0.3.0/backend.go#L63-L87)

`SubscriptionLoop` uses `More` to retain or retire a route independently of its event error.

| Result storage | Lifetime |
|----------------|----------|
| `Loop.Poll` | Borrowed until the next `Poll`, `Run` or `Drain` on that loop |
| `SubscriptionLoop.Poll` | Borrowed until the next `Poll` or `Drain` on that subscription loop |
| `Loop.Run` | Owned result slice, possibly partial alongside failure |

Copy retained elements before the borrow expires. Copies share any referenced payload. On fatal failure, completed results can precede the stored error. [One-shot poll](https://github.com/hayabusa-cloud/takt/blob/v0.3.0/loop.go#L143-L155) · [Subscription poll](https://github.com/hayabusa-cloud/takt/blob/v0.3.0/subscription.go#L92-L100)

### Drain and Ownership

`Loop.Drain` discards suspensions and releases its completion slab. `SubscriptionLoop.Drain` requests cancellation of active routes, skips already-canceling routes, and retires routes. Neither waits for kernel retirement or establishes successful cancellation. Preserve backend dispatch for late required original/cancellation CQEs. [Lifecycle](../recipes/lifecycle.md)

If admission must survive would-block, choose a surface that retains the suspended operation. [Admission](../recipes/oneshot.md#admission)

## Cost Obligations

Count maps, slabs, result storage, decoding, synchronization, first use, and pool misses. Give each route one owner; compare the selected path with equivalent direct code.

## Sources

Read the step, loop, subscription, backend and completion-memory contracts in the selected version. [v0.3.0 loop](https://github.com/hayabusa-cloud/takt/blob/v0.3.0/loop.go#L75-L155) · [v0.3.0 API](https://pkg.go.dev/code.hybscloud.com/takt@v0.3.0) · [Revision requirements](../verify.md#read-sources)

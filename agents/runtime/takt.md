# Execution and Routing with takt

## Use When

The caller needs dispatch, asynchronous one-shot execution, or repeated stream routing. The contracts below describe `takt` v0.2.0. Check submission and drain behavior against the version your caller selects.

## Owns

`takt` advances suspended computations and manages pending tokens, route generations, and completion storage. The backend maps identities to requests; tokens need not equal kernel `user_data`.

## Inputs and Preconditions

Serialize loop methods. Keep identities unique while live. When building `Completion.Value` from a borrowed context, copy the facts the suspension needs before the context is recycled. Retain any referenced payload through resumption.

## Choose the Execution Surface

| Surface | Contract |
|---------|----------|
| `Step` | Evaluate to a result or the first suspension |
| `Advance`, `AdvanceSuspension` | Dispatch the pending operation. `nil` or `ErrMore` resumes it. Would-block or failure returns the same suspension |
| `Exec` conveniences | Caller-selected loop with retry/backoff |
| `Loop` | One-shot submit/poll; every `Backend.Submit` error discards the suspension, including would-block |
| `SubscriptionLoop` | Repeated events, correlated by token and generation |

## Outputs and Ownership

### Completion Results

Poll status differs from per-completion results. The backend forbids positive `n` with poll error; adapters must preserve observations while satisfying that rule.

`Loop` resumes success and failure with `Completion.Value`. On a would-block completion, it resubmits the unconsumed operation and tracks the token returned by the backend. A failed resubmission discards the suspension. Completion `ErrMore` fails the one-shot loop. `SubscriptionLoop` uses `More` to retain or retire a route independently of its event error.

| Result storage | Lifetime |
|----------------|----------|
| `Loop.Poll`, `SubscriptionLoop.Poll` | Borrowed; copy retained elements before later poll or disposal |
| `Loop.Run` | Owned result slice, possibly partial alongside failure |

`Loop.Run` can invalidate storage borrowed from an earlier `Loop.Poll`. Copies do not extend referenced payload lifetimes. Fatal failure discards pending suspensions; ready results may precede the fatal error reported on the next poll.

### Drain and Ownership

`Loop.Drain` discards suspensions and releases its completion slab. `SubscriptionLoop.Drain` requests cancellation of active routes, skips already-canceling routes, and retires routes. Neither waits for kernel retirement or establishes successful cancellation. Preserve backend dispatch for late required original/cancellation CQEs. [Lifecycle](../recipes/lifecycle.md)

If admission must survive would-block, choose a surface that retains the suspended operation. [Admission](../recipes/oneshot.md#admission)

## Cost Obligations

Count maps, slabs, result storage, decoding, synchronization, first use, and pool misses. Give each route one owner; compare the selected path with equivalent direct code.

## Sources

Read `step.go`, `loop.go`, `subscription.go`, the backend interfaces, options and completion-memory code in the selected `takt` version. [v0.2.0 submission](https://github.com/hayabusa-cloud/takt/blob/v0.2.0/loop.go#L195-L200) · [v0.2.0 API](https://pkg.go.dev/code.hybscloud.com/takt@v0.2.0) · [Revision requirements](../verify.md#read-sources)

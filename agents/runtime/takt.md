# Execution and Routing with takt

## Use When

The caller needs dispatch, asynchronous one-shot execution, or repeated stream routing. Choose by admission and completion contract.

## Owns

`takt` advances suspended computations and manages pending tokens, route generations, and completion storage. The backend maps identities to requests; tokens need not equal kernel `user_data`.

## Inputs and Preconditions

Serialize loop methods. Keep identities unique while live and values valid through resumption. Copy borrowed context facts before invalidation; `Completion.ResumeValue()` and inline decoders cannot safely read recycled targets.

## Choose the Execution Surface

| Surface | Contract |
|---------|----------|
| `Step`, `Advance`, `AdvanceSuspension` | Caller cadence; dispatch nil/`ErrMore` consumes the suspension and returns its successor; would-block/failure retains it |
| `Exec` conveniences | Caller-selected loop with retry/backoff |
| `Loop` | One-shot submit/poll; every `Backend.Submit` error discards the suspension, including would-block |
| `SubscriptionLoop` | Repeated events, correlated by token and generation |

## Outputs and Ownership

### Completion Results

Poll status differs from per-completion results. The backend forbids positive `n` with poll error; adapters must preserve observations while satisfying that rule.

`Loop` resumes success/failure using `ResumeValue()`. Completion would-block re-arms the unconsumed operation under a fresh token; failed re-arm discards it. Completion `ErrMore` poisons the one-shot loop. `SubscriptionLoop` uses `More` to retain/retire routes independently of event error.

| Result storage | Lifetime |
|----------------|----------|
| `Loop.Poll`, `SubscriptionLoop.Poll` | Borrowed; copy retained elements before later poll or disposal |
| `Loop.Run` | Owned result slice, possibly partial alongside failure |

`Loop.Run` can invalidate storage borrowed from an earlier `Loop.Poll`. Copies do not extend referenced payload lifetimes. Fatal failure discards pending suspensions; ready results may precede the fatal error reported on the next poll.

### Drain and Ownership

`Loop.Drain` discards suspensions and releases its completion slab. `SubscriptionLoop.Drain` requests cancellation of active routes, skips already-canceling routes, and retires routes. Neither waits for kernel retirement or establishes successful cancellation. Preserve backend dispatch for late required original/cancellation CQEs. [Lifecycle](../recipes/lifecycle.md)

Use `Loop` only when its submit-error disposal fits. Retryable admission needs bounded caller state retaining the exact pending step; replaying a computation prefix can duplicate effects. [Admission](../recipes/oneshot.md#admission)

## Cost Obligations

Count maps, slabs, result storage, decoding, synchronization, first use, and pool misses. Give each route one owner; compare the selected path with equivalent direct code.

## Sources

Read `code.hybscloud.com/takt`: `step.go`, `loop.go`, `subscription.go`, backend interfaces, options, and completion-memory code. [Public API](https://pkg.go.dev/code.hybscloud.com/takt) · [Revision requirements](../verify.md#read-sources)

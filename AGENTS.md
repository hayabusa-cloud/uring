# uring

<a id="systems-programmability-with-uring"></a>
<a id="codehybscloudcomuring-boundary-guide"></a>
<a id="purpose"></a>

Use `uring` to submit Linux `io_uring` operations and inspect their completions. Start with the [README](README.md) and [file I/O examples](examples/file_io_test.go); use the [guide](agents/INDEX.md) for operation-specific contracts.

<a id="boundary-placement"></a>

| Owner | Responsibility |
|-------|----------------|
| Linux | ABI and request execution |
| `uring` | Ring mechanics, encoding, completions, identity, capabilities, kernel-facing lifetimes |
| Caller | Admission policy, scheduling, retry, routing, parsing, protocol, cancellation, shutdown |

<a id="outcome-discipline"></a>

## iox outcome algebra

Switch on the `iox.Outcome` returned by `iox.Classify(err)`. It distinguishes `nil` (OK), `iox.ErrWouldBlock`, `iox.ErrMore` and failure. The strict outcome lattice has OK at the bottom and Failure at the top. The two signals are incomparable. Their join is `WouldBlock ⊔ More = Failure` and their meet is `WouldBlock ⊓ More = OK`. Join is associative, commutative and idempotent. Use it to summarize independent controls under a strict policy. Retain the count and original error. [iox classifier](https://github.com/hayabusa-cloud/iox/blob/v0.4.0/semantics.go)

### Execution calculus for iox and takt

On classified outcomes, `Semantic = {WouldBlock, More}` and `Progress = {OK, More}`. Their union is `NonFailure` and their intersection is `{More}`. More can accompany a zero-byte result. If an error matches both sentinels, `Classify` selects WouldBlock.

| Boundary | Rule |
|----------|------|
| Ring submission | If `Multicast` returns `(k, iox.ErrWouldBlock)`, it admitted the first `k` targets. Retry only the remaining targets. [Multicast as one operation](agents/runtime/takt.md#submit-multicast-as-one-operation) |
| Manual dispatch | `takt.Step` evaluates a `kont` expression to a value or suspension. `takt.Advance` resumes once on OK or More and returns the same suspension on WouldBlock or Failure. The dispatcher accounts for partial progress. [Manual stepping](https://github.com/hayabusa-cloud/takt/blob/v0.3.0/step.go#L22-L46) |
| One-shot proactor | `takt.Loop` matches replies by token. A `Backend.Submit` error means the whole offer was refused. On WouldBlock, a later `Poll` reoffers the same operation. A More completion yields `takt.ErrUnsupportedMultishot`. |
| Multishot route | `takt.SubscriptionLoop` matches token and generation. Map `F_MORE` to route liveness and handle any final value or error. [Execution and routing](agents/runtime/takt.md) |

<a id="ownership-and-lifecycle"></a>

## Ring ownership

- Submission success establishes admission. Preserve any accepted batch prefix. Match each reaped CQE to its source before interpreting the result and flags. Account for an admitted operation's effects before retrying. Set `NotifySucceed` when write-like success CQEs are required and leave explicit success suppression clear.
- Retain request identity, typed Go roots and stable addresses through each resource's final use. Copied CQEs may still borrow context or payload storage. Follow the [release conditions](agents/boundary/resources.md) before recycling either.
- Resume each continuation at most once. A multishot subscription handles repeated CQEs and owns its pooled contexts. Dispatch both original and queued cancellation completions. Drain admitted work, reap outstanding CQEs and quiesce subscriptions before serialized `Stop`.
- Default rings use one owner (`MultiIssuers=false`): lock it to one OS thread before `Start` through `Stop`; serialize submission, waits, resize and teardown. Select `MultiIssuers=true` for shared submission or OS-thread migration.
- Preserve contention retries and `spin` backoff. For caller I/O waits, use `iox.Backoff` after no progress and reset it after reaping completions. See [waiting](agents/boundary/integration.md#waiting-and-backoff).

<a id="required-reading"></a>
<a id="protocol-and-runtime-integration"></a>

Choose optional [runtime roles](agents/runtime/INDEX.md) for continuation, context or protocol needs; the caller retains scheduling and lifecycle policy.

<a id="workflow"></a>
<a id="references"></a>

After [environment setup](SETUP.md), run `make all` on native Linux 6.18+ with Go 1.27+ for vet and race tests. Follow [verification](agents/verify.md) for capability requirements, affected examples and comparison with equivalent direct code. [Focused proofs](proofs/README.md) document model assumptions and limits; ordinary Go builds do not require Lean. Use [primary sources](agents/references.md) for kernel contracts.

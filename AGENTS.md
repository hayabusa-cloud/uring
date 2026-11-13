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
<a id="ownership-and-lifecycle"></a>

- Submission success establishes admission. Inspect completion results, counts and flags separately; dispatch every reaped CQE and retry only unaccepted work. Set `NotifySucceed` when write-like success CQEs are required; explicit SQE suppression still applies.
- Retain request identity, typed Go roots and stable addresses through each resource's final use. Copied CQEs may still borrow context or payload storage. Follow the [release conditions](agents/boundary/resources.md) before recycling either.
- Resume continuations at most once; use subscriptions for repeated events. Cancellation, callbacks and continuation disposal leave separate drain obligations. Drain requests, reap CQEs and quiesce subscriptions before serialized `Stop`; it does not drain for you.
- Prefer one ring owner and bounded state. With `MultiIssuers=false`, lock that owner to one OS thread before `New` through `Stop`; serialize submission, waits, resize and teardown.
- Preserve contention retries and `spin` backoff. For caller I/O waits, use `iox.Backoff` after no progress and reset it after reaping completions. See [waiting](agents/boundary/integration.md#waiting-and-backoff).

<a id="required-reading"></a>
<a id="protocol-and-runtime-integration"></a>

Choose optional [runtime roles](agents/runtime/INDEX.md) for continuation, context or protocol needs; the caller retains scheduling and lifecycle policy.

<a id="workflow"></a>
<a id="references"></a>

After [environment setup](SETUP.md), run `make all` on native Linux 6.18+ with Go 1.26+ for vet and race tests. Follow [verification](agents/verify.md) for capability requirements, affected examples and comparison with equivalent direct code. [Focused proofs](proofs/README.md) document model assumptions and limits; ordinary Go builds do not require Lean. Use [primary sources](agents/references.md) for kernel contracts.

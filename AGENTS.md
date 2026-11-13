# Systems Programmability with uring

<a id="codehybscloudcomuring-boundary-guide"></a>
<a id="purpose"></a>

Compose explicit contracts into direct execution. Establish correctness and zero abstraction overhead against equivalent direct code.

<a id="boundary-placement"></a>

| Owner | Responsibility |
|-------|----------------|
| Linux | ABI and request execution |
| `uring` | Ring mechanics, encoding, completions, identity, capabilities, kernel-facing lifetimes |
| Caller | Admission policy, scheduling, retry, routing, parsing, protocol, cancellation, shutdown |

<a id="outcome-discipline"></a>
<a id="ownership-and-lifecycle"></a>

- Separate admission, result, liveness, release, and protocol progress; preserve counts and flags.
- Retain identity, typed roots, and stable addresses through final use. Copied CQEs may still borrow storage.
- Resume continuations at most once; use subscriptions for repeated events. Cancellation, callbacks, and disposal leave independent drain obligations.
- Prefer one ring owner and bounded state. Preserve contention retries and `spin` backoff; caller I/O waits use `iox.Backoff`. Go serialization does not establish Linux task affinity.

<a id="required-reading"></a>
<a id="protocol-and-runtime-integration"></a>

Use [task selection](agents/INDEX.md) for contracts, recipes, runtime roles, and required sources; [README](README.md) for API orientation.

<a id="workflow"></a>
<a id="references"></a>

Follow [verification](agents/verify.md). Preserve contract assumptions and distinguish source, test, proof, and measurement evidence. [Focused proofs](proofs/README.md) state their scope and source mapping; ordinary Go builds do not require Lean. Use [primary sources](agents/references.md) for external claims.

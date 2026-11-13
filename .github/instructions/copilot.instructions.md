---
applyTo: "**"
---

# Review uring

`code.hybscloud.com/uring` is a Go 1.26+ boundary to Linux io_uring, targeting Linux 6.18+ with explicit capability checks. Review changes for concrete correctness, API, ownership, and performance consequences. Include code, examples, tests, and guidance when the change affects their contract.

## Evidence and Findings

Report actionable defects supported by the changed code and current contracts. Give the location, triggering condition, consequence, and a concise correction. Distinguish confirmed findings from assumptions; do not invent runtime evidence from compilation or a skipped test. Avoid style-only comments and speculative defenses for documented caller violations.

When implementation, tests, GoDoc, or README disagree, identify the exact conflict and check the owning API or kernel source. Tests and prose are evidence, not automatic authority. Preserve unresolved uncertainty rather than silently selecting the most convenient interpretation.

## Ownership and Outcomes

- `uring` owns SQE/CQE mechanics, request identity, capability facts and kernel-facing lifetimes. Retry cadence, scheduling, routes, parsing, protocol state, cancellation policy and final teardown timing belong to callers.
- Admission, operation result, request liveness, resource release and protocol progress are distinct. Preserve positive counts, flags and errors. `iox.ErrWouldBlock` and `iox.ErrMore` are control signals; `MORE` does not erase a negative operation result.
- Direct context stores facts inline. Indirect borrows a 64-byte base SQE; Extended adds 64 bytes of raw caller data. Neither borrowed target is kept immutable by copying its CQE descriptor. Copy needed facts before target mutation, reuse or release. Raw address fields and overlays are not Go roots; retain typed referents separately.
- Preserve the documented ownership model. Go serialization does not prove Linux task affinity. Explicit single-issuer mode requires a pinned owner from before `New` through `Stop`. Keep failed-claim retries and `spin` backoff wherever contention remains.
- `Add` stages provided buffers; matching `Advance` transfers ownership. Incremental offsets are per ID, CQ dispatch is serialized, and `BUF_MORE` retains the buffer. SEND_ZC notification release and ZCRX refill ownership are separate protocols.
- Drain accepted work and required original/cancellation CQEs before final teardown. `Close` and `Stop` are caller-serialized lifecycle operations. Do not add hot-path locks or lifecycle machinery solely to support contract-violating callers.

## Cost and Platform

Keep hot paths allocation-free under their stated contract. Inspect captures, interface boxing, copies, pool misses, synchronization and waits; neither `make` nor `defer` alone establishes heap allocation. Compare with direct code that implements the same semantics.

Linux boundary syscalls use `code.hybscloud.com/zcall`. Darwin files are compile stubs and may use standard-library syscall simulation; they establish no Linux runtime behavior. Capability probes and wrapper support are distinct, and unsupported hardware tests must remain visible as skips or explicit exclusions.

For guide changes, follow [verification](../../agents/verify.md#guide-checks), preserving contracts, assumptions and incoming links. For proof claims, inspect the [model and source mapping](../../proofs/README.md); a model theorem does not establish Go or kernel correctness.

# Verify a Caller Contract

State the operation, caller decision, retained resources and release evidence. Check the smallest complete contract, then implement and validate it.

## Read Sources

Start with [core contracts](core.md) and the selected [task](INDEX.md). Read the current `iox` outcome contract and the complete relevant implementation and consumers in each [owning package](boundary/integration.md). Inspect the exact kernel ABI and capability path when the change depends on them.

Record module versions or commits, relevant local changes, compiler/flags, architecture and kernel capabilities. Reuse evidence only for unchanged sources under the same assumptions. Resolve dependency sources through their published modules.

## Design and Review

Keep kernel observations separate from caller policy. Identify input domains before narrowing, one owner per resource, borrowed intervals, accepted prefixes, pending state and final release conditions. Preserve positive progress, failures, request liveness and protocol state independently.

Simplify state storage, retry and lifecycle transitions only when the resulting contract remains valid. Preserve failed-CAS retries and contention backoff unless ownership, publication, lock scope and measured cost justify a change. Serialization alone is insufficient.

Check the implementation against the chosen contract, including failed guards, exhausted capacity, unavailable features, partial admission, cancellation races and teardown. The [encoding](boundary/encoding.md), [resource](boundary/resources.md) and selected recipe supply concrete cases.

## Evidence

| Artifact | Required checks |
|----------|-----------------|
| Go | Format, build, static checks, behavior/races and applicable escape/allocation checks |
| Example | Compile against selected APIs; exercise visible ownership and control flow |
| Guide | Source correspondence, assumptions, links and consistency |
| Formal claim | Reviewed specification, model, theorem, assumptions, proof build and transitive axioms |

Run Linux behavior on native Linux; cross-builds establish compatibility. Record commands, results, skips and unresolved claims. Source inspection, finite tests, proved models, implementation proofs and measurements are distinct evidence.

## Race and Waiting Rule

Cancellation and timeout race with completion. Preserve observations and drain obligations; the caller owns retry and final teardown. Use [appropriate waits](boundary/integration.md#waiting-and-backoff): `iox.Backoff` for caller I/O, `spin` for contention. Reset caller backoff after progress.

## Cost

Compare equivalent direct code under identical admission, results/errors, context, ownership, cancellation, backpressure and release contracts. Inspect emitted code, heap escapes, allocation, copies, retained state, dispatch, synchronization, syscalls and waits.

Measure initialization, first use, pool miss, steady state, cancellation and teardown as applicable. Repeat samples; state hardware, workload, compiler, noise and tolerances. Include CPU/completion, throughput and latency tails where relevant. Generics, static functions and pooling alone establish no zero-overhead result.

## Guide Checks

When reorganizing the guide, preserve every contract, assumption, formal statement, source reference and useful anchor. Record the previous revision so removed material remains traceable. Document the prerequisites and commands needed to reproduce each check.

Read every page listed under [guide review](INDEX.md#review-the-guide). Check local links and anchors, and verify technical claims against the cited sources.

Format and compile runnable examples with the selected dependencies (`go test -c ./examples`); run affected native cases. Compilation and feature skips establish no lifetime guarantee. Label pseudocode.

For formal claims, use the [proof project](../proofs/README.md). Check the theorem statement and source mapping, not just a successful build. Document remaining implementation, kernel and environmental obligations.

## Before Submitting Changes

Rerun affected checks after corrections. Finish when the required checks pass within the declared scope, and state what remains unproved or unmeasured.

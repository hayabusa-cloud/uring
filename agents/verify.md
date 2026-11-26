# Verify a Caller Contract

Verify the selected operation from admission through final release. Use the evidence appropriate to the claim.

## Read Sources

Read the `iox` outcome contract at the selected revision and trace the operation through its implementation and consumers in each owning package. For kernel-dependent behavior, inspect the applicable ABI, implementation and capability path. [Package ownership](boundary/integration.md) · [Primary sources](references.md)

Record module versions or commits, local changes, compiler flags, architecture and kernel capabilities. Resolve dependencies through their published modules. Reuse earlier results only for the same sources and assumptions.

## Design and Review

Identify input domains before narrowing, accepted prefixes, pending work, borrowed intervals, owners and release conditions. Trace positive progress, failures, request liveness and protocol state independently. [Core contracts](core.md)

Simplify state storage, retry and lifecycle transitions only when the resulting contract remains valid. Preserve failed-CAS retries and contention backoff unless ownership, publication, lock scope and measured cost justify a change. Serialization alone is insufficient.

Check the implementation against the chosen contract, including failed guards, exhausted capacity, unavailable features, partial admission, cancellation races and teardown. The [encoding](boundary/encoding.md), [resource](boundary/resources.md) and selected recipe supply concrete cases.

## Evidence

| Artifact | Required checks |
|----------|-----------------|
| Go | Format, build, static checks, behavior/races and applicable escape/allocation checks |
| Example | Compile against selected APIs; exercise visible ownership and control flow |
| Guide | Check the task routes, anchors, source correspondence and assumptions under [guide checks](#guide-checks) |
| Formal claim | Reviewed specification, model, theorem, assumptions, proof build and transitive axioms |

For runtime changes, `make all` runs vet and race tests on native Linux 6.18+ with Go 1.27+. Cross-builds check compilation. Record commands, results, skips and unresolved claims. Distinguish source inspection, finite tests, model proofs, implementation proofs and measurements. [Setup](../SETUP.md) · [Makefile](../Makefile)

## Race and Waiting Rule

Cancellation and timeout race with completion. Preserve observations and drain obligations; the caller owns retry and final teardown. Use [appropriate waits](boundary/integration.md#waiting-and-backoff): `iox.Backoff` for caller I/O, `spin` for contention. Reset caller backoff after progress.

## Cost

Compare equivalent direct code under identical admission, results/errors, context, ownership, cancellation, backpressure and release contracts. Inspect emitted code, heap escapes, allocation, copies, retained state, dispatch, synchronization, syscalls and waits.

Measure initialization, first use, pool miss, steady state, cancellation and teardown as applicable. Repeat samples; state hardware, workload, compiler, noise and tolerances. Include CPU/completion, throughput and latency tails where relevant. Generics, static functions and pooling alone establish no zero-overhead result.

## Guide Checks

For a reorganization, record the starting revision and local edits. Follow each changed route from the [task index](INDEX.md#choose-a-task) to its contract or recipe. Check each section's inventory and local links. Follow existing fragments to confirm that their landing text still answers the original question or links directly to the answer.

Compare the result with the starting guide for preserved contracts, assumptions, formal statements and source references. Check technical claims against the owning implementation and document the prerequisites and commands for each check.

Format and compile runnable examples with the selected dependencies (`go test -c ./examples`); run affected native cases. Compilation and feature skips establish no lifetime guarantee. Label pseudocode.

For formal claims, check the theorem statement, assumptions and source mapping. Record outstanding implementation, kernel and environment obligations. [Proof project](../proofs/README.md)

## Before Submitting Changes

Rerun affected checks after corrections. Finish when the required checks pass within the declared scope, and state what remains unproved or unmeasured.

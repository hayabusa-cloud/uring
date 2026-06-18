# Saved Lift Index

This directory stores topic-specific lift records for work above or beyond `code.hybscloud.com/uring`. A lift records the checked boundary shape to preserve when generating, reviewing, or repairing caller-side code.

When the user does not specify another location, save new or updated lift records under `path("uring/agents/lift")`. Keep each file focused on one clear topic and add it to this index.

## Files

- [uring/agents/lift/boundary-core.md](boundary-core.md): saved lift for the core boundary facts plus SQE encoding, CQE observation, outcome decode, release decode, and zero-copy notification handling.
- [uring/agents/lift/resources-lifecycle.md](resources-lifecycle.md): saved lift for ownership epochs, pending kernel references, release, recycle, close, stop, and cancellation-adjacent lifecycle checks, together with fixed, provided, and selected buffers and zero-copy buffer carriers.
- [uring/agents/lift/protocols-runtime.md](protocols-runtime.md): saved lift for multishot frontiers, subscription-style observations, protocol boundaries, stream-route liveness, and the caller-owned carriers from `code.hybscloud.com/kont`, `code.hybscloud.com/cove`, `code.hybscloud.com/takt`, and `code.hybscloud.com/sess` around boundary observations.
- [uring/agents/lift/policy-separation.md](policy-separation.md): saved lift for separating caller-owned retry, backoff, route, parser, scheduler, cancellation, timeout, and service policy from `code.hybscloud.com/uring` boundary mechanics.

## Save Rule

After `formalize`, `typecheck`, `reduce`, `prove`, or `lift` completes for a task, update the relevant lift file or add a new topic-specific file here. The `lift` stage abstracts the public Go surface back into the checked boundary notation, and its completed result is the saved checkpoint for that abstraction. The saved lift should preserve the boundary facts, caller-owned policy separation, obligation checks, decisions, verification evidence, and remaining assumptions for that topic.

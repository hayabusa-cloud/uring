# Boundary Topic Index

Use these files when the task touches concrete `code.hybscloud.com/uring` boundary facts: SQEs, CQEs, resources, buffers, protocol frontiers, or package integration points. These files decide what may cross the package boundary and what must stay in caller-owned code.

Read only the topics required by the task, unless the work is a broad guide edit or broad caller-side generation. In that case, read every file in this directory.

## Files

- [uring/agents/boundary/sqe-cqe.md](sqe-cqe.md): SQE encoding, CQE observation, `user_data` identity, operation decode, release decode, and zero-copy notification separation.
- [uring/agents/boundary/resources.md](resources.md): ownership epochs (pending, terminal, release, recycle, close, stop, and cancellation-adjacent states) together with fixed, provided, and selected buffers, zero-copy send and receive carriers, and buffer recycle gates.
- [uring/agents/boundary/integration.md](integration.md): integration with `code.hybscloud.com/iox`, `code.hybscloud.com/zcall`, `code.hybscloud.com/iofd`, `code.hybscloud.com/sock`, `code.hybscloud.com/iobuf`, `code.hybscloud.com/spin`, `code.hybscloud.com/lfq`, `code.hybscloud.com/framer`, `code.hybscloud.com/cove`, `code.hybscloud.com/kont`, `code.hybscloud.com/sess`, and `code.hybscloud.com/takt`.
- [uring/agents/boundary/protocols.md](protocols.md): how protocol stacks and runtimes should use `code.hybscloud.com/uring` as a shallow kernel boundary while keeping parser, route, retry, and session policy in the caller layer, and how Go handlers and closures above the boundary stay shallow.

The boundary files should preserve three separations: kernel facts versus caller policy, operation outcome versus release evidence, and owned resources versus borrowed observations.

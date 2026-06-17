# Workflow Index

Use these files to move from task intake to a checked result above or beyond `code.hybscloud.com/uring`. They explain when to formalize, when to check obligations, when to compile back to Go, and when to save the lift.

Read the workflow files in this order:

1. [uring/agents/workflow/boundary-gates.md](boundary-gates.md)
2. [uring/agents/workflow/task-checklists.md](task-checklists.md)
3. [uring/agents/workflow/proof-obligations.md](proof-obligations.md)
4. [uring/agents/workflow/task-loop.md](task-loop.md)

## Files

- [uring/agents/workflow/boundary-gates.md](boundary-gates.md): confirms which facts may cross the `code.hybscloud.com/uring` boundary and which decisions must stay in the caller layer.
- [uring/agents/workflow/task-checklists.md](task-checklists.md): maps common task shapes to the guide files needed for the task.
- [uring/agents/workflow/proof-obligations.md](proof-obligations.md): defines the checks for outcome separation, release evidence, ownership, lifecycle, and no hidden caller policy.
- [uring/agents/workflow/task-loop.md](task-loop.md): gives the staged loop for analysis, formalization, type checking, reduction, proof, compilation, lift saving, and verification.

For design work, start from the caller goal, formalize the boundary action, discharge the obligations, compile the boundary step, save the lift, and verify the result.

For review work, lift the existing Go surface first, check it against the formalization and obligation list, apply any correction, save the updated lift, and verify correspondence.

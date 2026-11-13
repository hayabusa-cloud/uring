# Runtime Roles

<a id="caller-runtime-packages"></a>

Select roles by obligation. They compose without a fixed pipeline or transfer of caller policy into `uring`.

| Need | Guide | Owns |
|------|------|------|
| Suspend and resume once | [kont](kont.md) | Affine continuation and successor |
| Carry and check context | [cove](cove.md) | Context, requirement checks, and permitted transitions |
| Dispatch/correlate observations | [takt](takt.md) | Execution, tokens, completion storage, stream routes |
| Enforce protocol steps | [sess](sess.md) | Endpoint actions and bounded local transport |

The caller owns cadence, admission, cancellation, and retirement policy. Follow each guide's preconditions and [source requirements](../verify.md#read-sources). The owning packages define their semantics; these guides describe integration requirements without claiming a proof of the combined system.

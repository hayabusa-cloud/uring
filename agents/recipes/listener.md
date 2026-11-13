# Listener Setup

Use `ListenerManager.ListenTCP4`, `ListenTCP6`, or `ListenUnix` to submit SOCKET, then route its completions to `ListenerOp.HandleCQE`:

```text
SOCKET → OnSocketCreated → BIND → OnBound → LISTEN → OnListening
```

For TCP, an empty IP selects the respective IPv4/IPv6 wildcard address, and port zero lets the kernel choose a port. A nil TCP address means wildcard IP and port zero. The caller's address is not modified.

The helper stores the socket FD, invokes each callback, and attempts the next submission once, reusing one pooled context throughout setup. The caller owns CQ polling, routing, retry cadence, and cleanup. Check SOCKET, BIND, and LISTEN support before starting.

| `HandleCQE` result | Meaning and caller action |
|-------------------|---------------------------|
| `false, nil` | Unrelated CQE; route it elsewhere. |
| `true, nil` | CQE consumed; `OnListening` is the readiness signal. |
| `true, iox.ErrWouldBlock` | CQE consumed, next SQE prepared but not admitted. Drive the ring, then retry `ring.SubmitExtended(uring.PackExtended(op.Ext()))`. |
| `true, other error` | CQE consumed; successor submission failed. Handle the admission error and close when drained. |

Retry only the prepared submission. Replaying the CQE repeats its callback and transition. Kernel errors, including EAGAIN, go to `OnError` and stop setup; the returned error reports only submission failure. If the initial `Listen*` call fails, no operation is returned and no setup request was admitted.

Serialize dispatch, retry, and close. Callbacks must not reenter dispatch or modify the prepared SQE. A false callback result or a callback's `Close` stops advancement and leaves cleanup with the caller. Keep the context and sockaddr alive through their pending requests. Use caller-owned `iox.Backoff` when waiting makes no progress.

After an early `Close`, dispatch the pending setup CQE to retire it and close any late socket result; callbacks are suppressed. Call `Close` again after draining to return the context. Drain accept and other FD users before final close.

For manual control, use `DecodeListenerCQE`, `SetFD`, the `PrepareListener*` helpers, and `SetListenerReady`. Consume each CQE exactly once through manual decoding or `HandleCQE`.

[Listener example](../../listener_example_test.go) demonstrates setup through accept cancellation and drain. [Regression tests](../../listener_auto_test.go) cover IPv4/IPv6/Unix, wildcard inputs, callback stops, errors, routing, close, and full-SQ retry; [manual tests](../../listener_linux_test.go) cover the primitives. Review [resource lifetimes](../boundary/resources.md) and follow the [verification workflow](../workflow/task-loop.md).

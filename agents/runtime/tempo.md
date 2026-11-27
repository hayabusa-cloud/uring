# Event History with tempo

## Use When

Use `tempo` when the next caller action depends on earlier events. A completed write can have a receipt ready while permission to publish it changes. A multishot poll and its cancellation can also finish in either order. [Cancellation example](../recipes/lifecycle.md#request-and-observe)

## Owns

A residual is a value representing the state after the events observed so far. `Step(event)` returns its successor. `Observe()` reads the current decision inputs. The caller obtains and orders events, matches CQEs, drives I/O and owns resources. [Residual contract](https://github.com/hayabusa-cloud/tempo/blob/v0.1.1/residual.go#L7-L21)

## Inputs and Preconditions

Give each policy an owner and an initial state. Feed it events for the same command or route generation. Copy the facts the policy needs while the request context is valid. Keep live descriptors, borrowed buffers and pending suspensions with their owners. [Request identity](../boundary/sqe-cqe.md#request-identity)

## Track a Write Receipt

Two matched send CQEs report 2 bytes each for `PI` and `NG`. The pending `sess.Send` carries a receipt for four bytes written. The latest grant or revocation determines permission to publish. Once shutdown begins, publication stays closed.

```go
type receiptEvent uint8

const (
	grantReceipt receiptEvent = iota + 1
	revokeReceipt
	stopReceipts
)

type receiptGrant bool

func (g receiptGrant) Step(e receiptEvent) receiptGrant {
	switch e {
	case grantReceipt:
		return true
	case revokeReceipt:
		return false
	}
	return g
}

func (g receiptGrant) Observe() bool { return bool(g) }

type receiptStopped bool

func (s receiptStopped) Step(e receiptEvent) receiptStopped { return s || e == stopReceipts }
func (s receiptStopped) Observe() bool                      { return bool(s) }
```

Initialize the policy before the first event and keep each successor. The owner serializes events with enqueue attempts. Here it observes revocation before a queue slot opens. [Guarded enqueue helper](../recipes/protocol.md#deliver-a-write-receipt)

```go
policy := tempo.Both(receiptGrant(false), receiptStopped(false))
policy = policy.Step(grantReceipt)
policy = policy.Step(revokeReceipt)
next, attempted, err := tryReceipt(ep, pending, policy.Observe())
```

## Outputs and Ownership

After revocation, `cove.CheckSuspension` refuses delivery and the helper returns `(pending, false, nil)`. A later grant permits another attempt with the same send unless `stopReceipts` has occurred. A successful `sess.Advance` enqueues the receipt and consumes the send. The socket write already accounts for all four bytes. [Receipt outcomes](../recipes/protocol.md#deliver-a-write-receipt)

## Cost Obligations

Keep one current product per independently governed receipt or route. `Both` steps both components on the same event. Compare that work with maintaining the same two facts directly.

## Sources

The example uses `tempo` v0.1.1. [Product contract](https://github.com/hayabusa-cloud/tempo/blob/v0.1.1/product.go#L13-L45) · [Context checks](cove.md#outputs-and-ownership)

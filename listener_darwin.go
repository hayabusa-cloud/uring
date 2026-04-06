// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"errors"
	"net"
	"sync/atomic"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/zcall"
)

// ListenerState tracks which stage of socket→bind→listen has completed (darwin stub).
type ListenerState uint8

const (
	ListenerStateInit ListenerState = iota
	ListenerStateSocket
	ListenerStateBind
	ListenerStateListen
	ListenerStateReady
	ListenerStateFailed
)

// ListenerHandler receives completion events during listener creation (darwin stub).
type ListenerHandler interface {
	OnSocketCreated(fd iofd.FD) bool
	OnBound() bool
	OnListening()
	OnError(op uint8, err error)
}

// listenerTag is the sentinel written at byte 0 of every listener context (darwin stub).
const listenerTag uint8 = 0xAC

// listenerCtxData stores listener facts in ExtSQE.UserData (darwin stub).
type listenerCtxData struct {
	Tag         uint8
	State       ListenerState
	Domain      uint16
	Backlog     int32
	SockaddrLen uint16
	Sockaddr    [30]byte
}

var _ [40 - unsafe.Sizeof(listenerCtxData{})]struct{}

// Unlike Ctx0-based helpers, listener CQEs are decoded explicitly via
// DecodeListenerCQE, so UserData[0:8] does not store a Handler function pointer.
type listenerCtx struct {
	Data listenerCtxData // 40 bytes
}

var _ [40 - unsafe.Sizeof(listenerCtx{})]struct{}

// ListenerStep is a decoded fact from a listener-related CQE (darwin stub).
type ListenerStep struct {
	Op      uint8
	FD      iofd.FD
	State   ListenerState
	Err     error
	Ext     *ExtSQE
	Handler ListenerHandler
}

// DecodeListenerCQE decodes a CQE into a ListenerStep fact (darwin stub).
func DecodeListenerCQE(cqe CQEView) (ListenerStep, bool) {
	return ListenerStep{}, false
}

// PrepareListenerSocket fills ext's SQE for IORING_OP_SOCKET (darwin stub).
func PrepareListenerSocket(ext *ExtSQE, domain, sockType, proto int, sa Sockaddr, backlog int, handler ListenerHandler) error {
	return ErrNotSupported
}

// PrepareListenerBind fills ext's SQE for IORING_OP_BIND (darwin stub).
func PrepareListenerBind(ext *ExtSQE, fd iofd.FD) {}

// PrepareListenerListen fills ext's SQE for IORING_OP_LISTEN (darwin stub).
func PrepareListenerListen(ext *ExtSQE, fd iofd.FD) {}

// SetListenerReady marks the listener context as ready (darwin stub).
func SetListenerReady(ext *ExtSQE) {}

// ListenerManager provides convenience methods for listener creation (darwin stub).
type ListenerManager struct {
	ring *Uring
	pool *ContextPools
}

// NewListenerManager creates a listener manager (darwin stub).
func NewListenerManager(ring *Uring, pool *ContextPools) *ListenerManager {
	return &ListenerManager{ring: ring, pool: pool}
}

// Ring returns the underlying Uring instance.
func (m *ListenerManager) Ring() *Uring { return m.ring }

// Pool returns the context pool.
func (m *ListenerManager) Pool() *ContextPools { return m.pool }

// ListenTCP4 creates a TCP IPv4 listener (darwin stub).
func (m *ListenerManager) ListenTCP4(addr *net.TCPAddr, backlog int, handler ListenerHandler) (*ListenerOp, error) {
	return nil, ErrNotSupported
}

// ListenTCP6 creates a TCP IPv6 listener (darwin stub).
func (m *ListenerManager) ListenTCP6(addr *net.TCPAddr, backlog int, handler ListenerHandler) (*ListenerOp, error) {
	return nil, ErrNotSupported
}

// ListenUnix creates a Unix domain socket listener (darwin stub).
func (m *ListenerManager) ListenUnix(addr *net.UnixAddr, backlog int, handler ListenerHandler) (*ListenerOp, error) {
	return nil, ErrNotSupported
}

// ListenerOp is a handle to a listener creation operation (darwin stub).
type ListenerOp struct {
	ring    *Uring
	ext     *ExtSQE
	fd      atomic.Int32
	pending atomic.Uint32
	closed  atomic.Bool
	pool    *ContextPools
}

// FD returns the listener socket file descriptor (darwin stub).
//
//go:nosplit
func (op *ListenerOp) FD() iofd.FD {
	return iofd.FD(op.fd.Load())
}

// SetFD stores the socket FD obtained from SOCKET completion (darwin stub).
func (op *ListenerOp) SetFD(fd iofd.FD) {
	raw := int32(fd)
	if raw >= 0 && op.closed.Load() {
		zcall.Close(uintptr(raw))
		return
	}
	op.fd.Store(raw)
}

// Ext returns the ExtSQE (darwin stub).
func (op *ListenerOp) Ext() *ExtSQE {
	return op.ext
}

func (op *ListenerOp) closeFD() {
	fd := op.fd.Swap(-1)
	if fd >= 0 {
		zcall.Close(uintptr(fd))
	}
}

// Close releases resources (darwin stub).
func (op *ListenerOp) Close() {
	op.closed.Store(true)
	op.closeFD()
	if op.pending.Load() != 0 {
		return
	}
	if op.ext != nil && op.pool != nil {
		op.pool.PutExtended(op.ext)
		op.ext = nil
	}
}

// AcceptMultishot starts accepting connections on this listener (darwin stub).
func (op *ListenerOp) AcceptMultishot(handler MultishotHandler) (*MultishotSubscription, error) {
	return nil, ErrNotSupported
}

// ErrNotReady indicates the listener is not yet ready for accept.
var ErrNotReady = errors.New("listener not ready")

// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"net"
	"sync/atomic"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/zcall"
)

// Listener state and context for compact SQE/CQE encoding.

// ListenerState tracks which stage of socket→bind→listen has completed.
type ListenerState uint8

const (
	ListenerStateInit   ListenerState = iota // Initial state
	ListenerStateSocket                      // Socket creation completed
	ListenerStateBind                        // Bind completed
	ListenerStateListen                      // Listen completed
	ListenerStateReady                       // Listener is ready for accept
	ListenerStateFailed                      // Operation failed
)

// ListenerHandler receives completion events during listener setup.
// The bool-return callbacks are authoritative control-flow hooks: returning true
// advances to the next setup phase, while false aborts before submitting it.
type ListenerHandler interface {
	// OnSocketCreated reports that socket creation succeeded. Return true to
	// continue listener setup or false to abort it.
	OnSocketCreated(fd iofd.FD) bool
	// OnBound reports that bind succeeded. Return true to continue listener
	// setup or false to abort it.
	OnBound() bool
	// OnListening reports that the socket is now listening.
	OnListening()
	// OnError reports a terminal failure for the current setup stage.
	OnError(op uint8, err error)
}

// listenerTag is the sentinel written at byte 0 of every listener context.
// DecodeListenerCQE checks this before accepting a CQE, preventing
// non-listener extended completions from being misrouted.
const listenerTag uint8 = 0xAC

// listenerCtxData stores listener facts in ExtSQE.UserData.
type listenerCtxData struct {
	Tag         uint8         // sentinel (must equal listenerTag)
	State       ListenerState // 1 byte
	Domain      uint16        // 2 bytes
	Backlog     int32         // 4 bytes
	SockaddrLen uint16        // 2 bytes
	Sockaddr    [30]byte      // 30 bytes, fits sockaddr_in (16) and sockaddr_in6 (28)
}

var _ [40 - unsafe.Sizeof(listenerCtxData{})]struct{}

// listenerCtx is the raw scalar context packed into ExtSQE.UserData.
// Unlike Ctx0-based helpers, listener CQEs are decoded explicitly via
// DecodeListenerCQE, so UserData[0:8] does not store a Handler function pointer.
type listenerCtx struct {
	Data listenerCtxData // 40 bytes
}

var _ [40 - unsafe.Sizeof(listenerCtx{})]struct{}

// ListenerStep is the decoded result of a listener-related CQE.
// The caller decides what happens next based on this value.
type ListenerStep struct {
	Op      uint8         // Kernel op that completed (IORING_OP_SOCKET, _BIND, _LISTEN)
	FD      iofd.FD       // Socket FD (meaningful after SOCKET completes)
	State   ListenerState // State at the time of this completion
	Err     error         // Non-nil if the operation failed
	Ext     *ExtSQE       // The ExtSQE carrying this listener context
	Handler ListenerHandler
}

// DecodeListenerCQE decodes a CQE into a ListenerStep.
// Returns (step, true) if the CQE belongs to a listener operation,
// or (zero, false) if it does not.
func DecodeListenerCQE(cqe CQEView) (ListenerStep, bool) {
	if !cqe.Extended() {
		return ListenerStep{}, false
	}
	ext := cqe.ExtSQE()
	ctx := asListenerCtx(ext)
	if ctx.Data.Tag != listenerTag {
		return ListenerStep{}, false
	}
	if !listenerOpcode(ext.SQE.opcode) {
		return ListenerStep{}, false
	}
	handler := listenerGetHandler(ext)
	if handler == nil {
		return ListenerStep{}, false
	}

	step := ListenerStep{
		Op:      ext.SQE.opcode,
		FD:      iofd.FD(ext.SQE.fd),
		Ext:     ext,
		State:   ctx.Data.State,
		Handler: handler,
	}
	if cqe.Res < 0 {
		step.Err = errFromErrno(uintptr(-cqe.Res))
		step.State = ListenerStateFailed
	} else if step.Op == IORING_OP_SOCKET {
		step.FD = iofd.FD(cqe.Res)
	}
	listenerTrackCompletion(ext, step.Op)
	return step, true
}

// Stateless SQE preparation: fill fields, no submit, no policy.

// PrepareListenerSocket fills ext's SQE for IORING_OP_SOCKET and stores
// the sockaddr + backlog for subsequent stages. Small sockaddrs stay inline
// in ext.UserData; oversized ones stay anchored in the pooled sidecar.
// After calling this, submit with ring.SubmitExtended(PackExtended(ext)).
//
// ext must be a pool-borrowed slot obtained from [ContextPools.Extended].
// Passing a non-pooled ExtSQE is undefined behavior: the sidecar anchors
// live past the end of a standalone ExtSQE object.
//
// A nil handler is normalized to [NoopListenerHandler].
func PrepareListenerSocket(ext *ExtSQE, domain, sockType, proto int, sa Sockaddr, backlog int, handler ListenerHandler) error {
	ctx := listenerInitCtx(ext, handler)
	ctx.Data.State = ListenerStateSocket
	ctx.Data.Domain = uint16(domain)
	ctx.Data.Backlog = int32(backlog)

	if err := listenerSetSockaddr(ext, ctx, sa); err != nil {
		return err
	}

	ext.SQE = ioUringSqe{}
	ext.SQE.opcode = IORING_OP_SOCKET
	ext.SQE.fd = int32(domain)
	ext.SQE.off = uint64(sockType)
	ext.SQE.len = uint32(proto)
	return nil
}

// PrepareListenerBind fills ext's SQE for IORING_OP_BIND using the sockaddr
// stored from PrepareListenerSocket. fd is the socket from SOCKET completion.
func PrepareListenerBind(ext *ExtSQE, fd iofd.FD) {
	ctx := asListenerCtx(ext)
	ctx.Data.State = ListenerStateBind

	ext.SQE = ioUringSqe{}
	ext.SQE.opcode = IORING_OP_BIND
	ext.SQE.fd = int32(fd)

	addrPtr, addrLen := listenerSockaddrRaw(ext, ctx)
	ext.SQE.addr = uint64(uintptr(addrPtr))
	ext.SQE.off = uint64(addrLen)
}

// PrepareListenerListen fills ext's SQE for IORING_OP_LISTEN.
// fd is the bound socket, backlog from the stored context.
func PrepareListenerListen(ext *ExtSQE, fd iofd.FD) {
	ctx := asListenerCtx(ext)
	ctx.Data.State = ListenerStateListen
	extAnchors(ext).sockaddr = nil

	ext.SQE = ioUringSqe{}
	ext.SQE.opcode = IORING_OP_LISTEN
	ext.SQE.fd = int32(fd)
	ext.SQE.len = uint32(ctx.Data.Backlog)
}

// SetListenerReady marks the listener context as ready.
// Call after LISTEN completes successfully.
func SetListenerReady(ext *ExtSQE) {
	ctx := asListenerCtx(ext)
	ctx.Data.State = ListenerStateReady
}

// ListenerManager is a thin convenience for initial SOCKET submission.

// ListenerManager provides convenience methods for starting async listener
// creation. It prepares the initial SOCKET SQE and submits it.
// The caller is responsible for CQE routing and chain advancement
// (bind→listen) using DecodeListenerCQE + Prepare helpers.
type ListenerManager struct {
	ring *Uring
	pool *ContextPools
}

// NewListenerManager creates a listener manager.
func NewListenerManager(ring *Uring, pool *ContextPools) *ListenerManager {
	return &ListenerManager{
		ring: ring,
		pool: pool,
	}
}

// Ring returns the underlying Uring instance.
func (m *ListenerManager) Ring() *Uring { return m.ring }

// Pool returns the context pool.
func (m *ListenerManager) Pool() *ContextPools { return m.pool }

// ListenTCP4 prepares and submits the initial SOCKET for TCP IPv4.
// The caller must decode subsequent CQEs via DecodeListenerCQE and
// advance the chain with PrepareListenerBind/PrepareListenerListen.
func (m *ListenerManager) ListenTCP4(addr *net.TCPAddr, backlog int, handler ListenerHandler) (*ListenerOp, error) {
	sa := sock.TCPAddrToSockaddr(addr)
	return m.startSocket(AF_INET, SOCK_STREAM|SOCK_CLOEXEC, IPPROTO_TCP, sa, backlog, handler)
}

// ListenTCP6 prepares and submits the initial SOCKET for TCP IPv6.
func (m *ListenerManager) ListenTCP6(addr *net.TCPAddr, backlog int, handler ListenerHandler) (*ListenerOp, error) {
	sa := sock.TCPAddrToSockaddr(addr)
	domain := AF_INET6
	if addr.IP.To4() != nil {
		domain = AF_INET
	}
	return m.startSocket(domain, SOCK_STREAM|SOCK_CLOEXEC, IPPROTO_TCP, sa, backlog, handler)
}

// ListenUnix prepares and submits the initial SOCKET for Unix domain.
func (m *ListenerManager) ListenUnix(addr *net.UnixAddr, backlog int, handler ListenerHandler) (*ListenerOp, error) {
	sa := sock.UnixAddrToSockaddr(addr)
	return m.startSocket(AF_LOCAL, SOCK_STREAM|SOCK_CLOEXEC, 0, sa, backlog, handler)
}

func (m *ListenerManager) startSocket(domain, sockType, proto int, sa Sockaddr, backlog int, handler ListenerHandler) (*ListenerOp, error) {
	ext := m.pool.Extended()
	if ext == nil {
		return nil, iox.ErrWouldBlock
	}
	op := &ListenerOp{
		ring: m.ring,
		ext:  ext,
		pool: m.pool,
	}
	op.fd.Store(-1)
	extAnchors(ext).owner = op

	if err := PrepareListenerSocket(ext, domain, sockType, proto, sa, backlog, handler); err != nil {
		m.pool.PutExtended(ext)
		return nil, err
	}

	sqeCtx := PackExtended(ext)
	if err := m.ring.SubmitExtended(sqeCtx); err != nil {
		m.pool.PutExtended(ext)
		return nil, err
	}

	return op, nil
}

// ListenerOp is a thin handle for a listener creation in progress.

// ListenerOp is a handle to a listener creation operation.
// The caller drives the setup state machine; ListenerOp holds the listener FD
// and provides convenience methods around it.
type ListenerOp struct {
	ring    *Uring
	ext     *ExtSQE
	fd      atomic.Int32
	pending atomic.Uint32
	closed  atomic.Bool
	pool    *ContextPools
}

func (op *ListenerOp) closeFD() {
	fd := op.fd.Swap(-1)
	if fd >= 0 {
		zcall.Close(uintptr(fd))
	}
}

// FD returns the listener socket file descriptor.
// Returns -1 if the socket hasn't been created yet.
//
//go:nosplit
func (op *ListenerOp) FD() iofd.FD {
	return iofd.FD(op.fd.Load())
}

// SetFD stores the socket FD obtained from SOCKET completion.
func (op *ListenerOp) SetFD(fd iofd.FD) {
	op.fd.Store(int32(fd))
	if op.closed.Load() {
		op.closeFD()
	}
}

// Ext returns the ExtSQE for use with Prepare helpers and SubmitExtended.
func (op *ListenerOp) Ext() *ExtSQE {
	return op.ext
}

// Close releases resources. If the listener has an open FD, it is closed.
// When a listener setup SQE is still in flight, Close keeps the pooled ExtSQE
// borrowed until the caller drains that CQE and calls Close again.
// Caller must drain all in-flight operations before calling Close.
// Close is not safe for concurrent use.
// Caller must serialize Close and only perform final cleanup after draining
// pending listener setup CQEs.
func (op *ListenerOp) Close() {
	op.closed.Store(true)
	op.closeFD()
	if op.pending.Load() != 0 {
		return
	}
	op.releaseExt()
}

// AcceptMultishot starts a multishot accept subscription on a ready listener.
// Options are forwarded to [Uring.AcceptMultishot].
// Returns ErrNotReady until the listener FD is set and valid.
func (op *ListenerOp) AcceptMultishot(handler MultishotHandler, options ...OpOptionFunc) (*MultishotSubscription, error) {
	fd := op.FD()
	if fd < 0 {
		return nil, ErrNotReady
	}
	sqeCtx := ForFD(int32(fd))
	return op.ring.AcceptMultishot(sqeCtx, handler, options...)
}

// ────────────────────────────────────────────────────────────────────
// Internal helpers
// ────────────────────────────────────────────────────────────────────

func listenerInitCtx(ext *ExtSQE, handler ListenerHandler) *listenerCtx {
	if handler == nil {
		handler = NoopListenerHandler{}
	}
	ctx := (*listenerCtx)(unsafe.Pointer(&ext.UserData[0]))
	anchors := extAnchors(ext)
	anchors.handler = handler
	anchors.sockaddr = nil
	ctx.Data = listenerCtxData{Tag: listenerTag, State: ListenerStateInit}
	return ctx
}

func asListenerCtx(ext *ExtSQE) *listenerCtx {
	return (*listenerCtx)(unsafe.Pointer(&ext.UserData[0]))
}

func listenerGetHandler(ext *ExtSQE) ListenerHandler {
	handler, _ := extAnchors(ext).handler.(ListenerHandler)
	return handler
}

func listenerSetSockaddr(ext *ExtSQE, ctx *listenerCtx, sa Sockaddr) error {
	if sa == nil {
		return nil
	}

	saPtr, saLen := sa.Raw()
	ctx.Data.SockaddrLen = uint16(saLen)
	if int(saLen) <= len(ctx.Data.Sockaddr) {
		copy(ctx.Data.Sockaddr[:], unsafe.Slice((*byte)(saPtr), saLen))
		return nil
	}

	extAnchors(ext).sockaddr = sa
	return nil
}

func listenerSockaddrRaw(ext *ExtSQE, ctx *listenerCtx) (unsafe.Pointer, uint32) {
	if ctx.Data.SockaddrLen == 0 {
		return nil, 0
	}
	if int(ctx.Data.SockaddrLen) <= len(ctx.Data.Sockaddr) {
		return unsafe.Pointer(&ctx.Data.Sockaddr[0]), uint32(ctx.Data.SockaddrLen)
	}

	sa := extAnchors(ext).sockaddr
	if sa == nil {
		panic("uring: missing anchored listener sockaddr")
	}
	saPtr, saLen := sa.Raw()
	if saLen != uint32(ctx.Data.SockaddrLen) {
		panic("uring: listener sockaddr anchor length drift")
	}
	return saPtr, saLen
}

func listenerTrackSubmit(ext *ExtSQE) {
	if !listenerOpcode(ext.SQE.opcode) {
		return
	}
	op, _ := extAnchors(ext).owner.(*ListenerOp)
	if op == nil {
		return
	}
	op.pending.Add(1)
}

func listenerTrackCompletion(ext *ExtSQE, opCode uint8) {
	if !listenerOpcode(opCode) {
		return
	}
	op, _ := extAnchors(ext).owner.(*ListenerOp)
	if op == nil {
		return
	}
	for {
		pending := op.pending.Load()
		if pending == 0 {
			return
		}
		if op.pending.CompareAndSwap(pending, pending-1) {
			return
		}
	}
}

func listenerOpcode(opCode uint8) bool {
	switch opCode {
	case IORING_OP_SOCKET, IORING_OP_BIND, IORING_OP_LISTEN:
		return true
	default:
		return false
	}
}

func (op *ListenerOp) releaseExt() {
	if op.ext == nil || op.pool == nil {
		return
	}
	op.pool.PutExtended(op.ext)
	op.ext = nil
}

// ErrNotReady indicates the listener is not yet ready for accept.
var ErrNotReady = errors.New("uring: listener not ready")

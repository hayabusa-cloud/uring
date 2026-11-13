// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import (
	"errors"
	"math"
	"net"
	"sync/atomic"
	"unsafe"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/sock"
	"code.hybscloud.com/zcall"
)

// Listener state and context for compact SQE/CQE encoding.

// ListenerState identifies the prepared socket→bind→listen stage or final state.
type ListenerState uint8

const (
	ListenerStateInit   ListenerState = iota // Initial state
	ListenerStateSocket                      // Socket creation prepared
	ListenerStateBind                        // Bind prepared
	ListenerStateListen                      // Listen prepared
	ListenerStateReady                       // Listener is ready for accept
	ListenerStateFailed                      // Operation failed
)

// ListenerHandler receives completion events during listener setup.
// [ListenerOp.HandleCQE] invokes callbacks and submits the next stage.
// Alternatively, the caller can use [DecodeListenerCQE] and the Prepare helpers.
// A false callback result stops setup; the caller still owns Close.
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
	FD      iofd.FD       // Successful SOCKET result, or request FD for BIND/LISTEN
	State   ListenerState // Prepared stage, or ListenerStateFailed on kernel error
	Err     error         // Non-nil if the operation failed
	Ext     *ExtSQE       // The ExtSQE carrying this listener context
	Handler ListenerHandler
}

// DecodeListenerCQE decodes a CQE into a ListenerStep.
// Returns (step, true) if the CQE belongs to a listener operation,
// or (zero, false) if it does not.
// Decoding retires that setup request. Consume each CQE exactly once, through
// either DecodeListenerCQE or [ListenerOp.HandleCQE].
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
	if domain < 0 || domain > math.MaxUint16 || sockType < 0 || sockType > math.MaxInt32 || proto < 0 || proto > math.MaxInt32 || backlog < 0 || backlog > math.MaxInt32 {
		return ErrInvalidParam
	}
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

// ListenerManager provides convenience methods for starting async listener
// creation. It prepares the initial SOCKET SQE and submits it.
// Route completions to [ListenerOp.HandleCQE] to advance bind→listen, or use
// [DecodeListenerCQE] and the Prepare helpers for manual advancement.
// The caller owns polling, admission retry, and cleanup.
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
// A nil addr or empty IP binds the IPv4 wildcard; port zero lets the kernel choose.
// The caller's address is not modified.
// Dispatch completions through [ListenerOp.HandleCQE] to finish setup.
func (m *ListenerManager) ListenTCP4(addr *net.TCPAddr, backlog int, handler ListenerHandler) (*ListenerOp, error) {
	var address net.TCPAddr
	if addr != nil {
		address = *addr
	}
	if len(address.IP) == 0 {
		address.IP = net.IPv4zero
	}
	sa := sock.TCPAddrToSockaddr(&address)
	return m.startSocket(AF_INET, SOCK_STREAM|SOCK_CLOEXEC, IPPROTO_TCP, sa, backlog, handler)
}

// ListenTCP6 prepares and submits the initial SOCKET for TCP IPv6.
// A nil addr or empty IP binds the IPv6 wildcard; port zero lets the kernel choose.
// An IPv4 address selects IPv4. The caller's address is not modified.
// Dispatch completions through [ListenerOp.HandleCQE] to finish setup.
func (m *ListenerManager) ListenTCP6(addr *net.TCPAddr, backlog int, handler ListenerHandler) (*ListenerOp, error) {
	var address net.TCPAddr
	if addr != nil {
		address = *addr
	}
	if len(address.IP) == 0 {
		address.IP = net.IPv6zero
	}
	sa := sock.TCPAddrToSockaddr(&address)
	domain := AF_INET6
	if address.IP.To4() != nil {
		domain = AF_INET
	}
	return m.startSocket(domain, SOCK_STREAM|SOCK_CLOEXEC, IPPROTO_TCP, sa, backlog, handler)
}

// ListenUnix prepares and submits the initial SOCKET for Unix domain.
// Dispatch completions through [ListenerOp.HandleCQE] to finish setup.
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

// ListenerOp is a handle to a listener creation operation.
// It holds the listener FD and advances setup through HandleCQE when selected.
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

// FD returns the listener socket FD, or -1 before creation or after Close.
//
//go:nosplit
func (op *ListenerOp) FD() iofd.FD {
	return iofd.FD(op.fd.Load())
}

// SetFD stores a successful SOCKET result, closing it if Close was already called.
func (op *ListenerOp) SetFD(fd iofd.FD) {
	op.fd.Store(int32(fd))
	if op.closed.Load() {
		op.closeFD()
	}
}

// Ext returns the borrowed setup context for Prepare helpers and SubmitExtended.
// It returns nil after final Close.
func (op *ListenerOp) Ext() *ExtSQE {
	return op.ext
}

// HandleCQE consumes this listener's CQE, invokes its callback, and attempts
// the next submission once:
//
//	SOCKET -> OnSocketCreated -> BIND -> OnBound -> LISTEN -> OnListening
//
// It returns false for unrelated CQEs. A true result reports CQE consumption;
// OnListening reports readiness. Kernel failures go to OnError; the returned
// error reports only the next submission's failure. On [iox.ErrWouldBlock],
// the next SQE remains prepared: drive the ring, then retry
// ring.SubmitExtended(PackExtended(op.Ext())). Never replay the CQE or callback.
//
// Caller must serialize dispatch, retry, and Close and consume each CQE exactly
// once through this method or [DecodeListenerCQE]. Callbacks must not reenter
// dispatch or change the prepared SQE. A false callback result or Close stops
// advancement; the caller retains cleanup responsibility.
//
// After an earlier Close, this method drains pending setup CQEs without callbacks
// and closes any late socket result. Call Close again after draining to release
// the context. Drain all operations using the listener FD before final Close.
func (op *ListenerOp) HandleCQE(cqe CQEView) (bool, error) {
	if op.ext == nil || !cqe.Extended() || cqe.ExtSQE() != op.ext {
		return false, nil
	}
	step, ok := DecodeListenerCQE(cqe)
	if !ok {
		return false, nil
	}
	if step.Op == IORING_OP_SOCKET && step.Err == nil {
		op.SetFD(step.FD)
	}
	if op.closed.Load() {
		return true, nil
	}
	if step.Err != nil {
		asListenerCtx(step.Ext).Data.State = ListenerStateFailed
		step.Handler.OnError(step.Op, step.Err)
		return true, nil
	}
	switch step.Op {
	case IORING_OP_SOCKET:
		if !step.Handler.OnSocketCreated(step.FD) || op.closed.Load() {
			return true, nil
		}
		PrepareListenerBind(step.Ext, step.FD)
	case IORING_OP_BIND:
		if !step.Handler.OnBound() || op.closed.Load() {
			return true, nil
		}
		PrepareListenerListen(step.Ext, step.FD)
	case IORING_OP_LISTEN:
		SetListenerReady(step.Ext)
		step.Handler.OnListening()
		return true, nil
	}
	return true, op.ring.SubmitExtended(PackExtended(step.Ext))
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
// Caller must complete bind and listen before calling; FD validity alone does
// not establish that the socket is listening.
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

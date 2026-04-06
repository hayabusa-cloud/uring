// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"code.hybscloud.com/iofd"
)

var zcrxBufPool sync.Pool

// ZCRXConfig is a stub. ZCRX is unsupported on Darwin.
type ZCRXConfig struct {
	IfName       string
	RxQueue      int
	AreaSize     int
	RqEntries    int
	ChunkSize    int
	UseHugePages bool
}

func (c *ZCRXConfig) defaults()       {}
func (c *ZCRXConfig) validate() error { return ErrNotSupported }

// ZCRXHandler handles ZCRX receive events.
type ZCRXHandler interface {
	// OnData handles received data. Return false for best-effort stop.
	OnData(buf *ZCRXBuffer) bool
	// OnError handles a CQE error. Return false for best-effort stop.
	OnError(err error) bool
	// OnStopped runs once on the CQE path during terminal retirement before state becomes Stopped.
	OnStopped()
}

// ZCRXBuffer is a stub preserving the Linux API shape.
type ZCRXBuffer struct {
	data     []byte
	slotLen  int
	offset   uint64
	token    uint64
	rq       *zcrxRefillQ
	released atomic.Bool
}

type zcrxArea struct {
	ptr       unsafe.Pointer
	size      int
	token     uint64
	chunkSize int
	rq        *zcrxRefillQ
	ringPtr   unsafe.Pointer
	ringSize  int
}

func (a *zcrxArea) unmap() {}

func (b *ZCRXBuffer) Bytes() []byte  { return nil }
func (b *ZCRXBuffer) Len() int       { return 0 }
func (b *ZCRXBuffer) Release() error { return nil }

type zcrxRefillQ struct{}

func (q *zcrxRefillQ) push(offset, token uint64, length int) error { return nil }
func (q *zcrxRefillQ) available() uint32                           { return 0 }

// ZCRXReceiver is a stub.
// External Stop and Close must not race CQE dispatch or terminal retirement. Call Close only after Stopped reports true.
type ZCRXReceiver struct {
	ring     *Uring
	pool     *ContextPools
	zcrxID   uint32
	area     zcrxArea
	handler  ZCRXHandler
	fd       iofd.FD
	closed   atomic.Bool
	state    atomic.Uint32
	ext      *ExtSQE
	userData atomic.Uint64
}

// NewZCRXReceiver returns ErrNotSupported on Darwin.
func NewZCRXReceiver(ring *Uring, pool *ContextPools, cfg ZCRXConfig) (*ZCRXReceiver, error) {
	return nil, ErrNotSupported
}

// Start returns ErrNotSupported on Darwin.
func (r *ZCRXReceiver) Start(fd iofd.FD, handler ZCRXHandler) error { return ErrNotSupported }

// Stop is a no-op stub on Darwin.
func (r *ZCRXReceiver) Stop() error { return nil }

// Stopped always returns true on Darwin.
func (r *ZCRXReceiver) Stopped() bool { return true }

// Close is a no-op stub on Darwin.
// Safe to call more than once; subsequent calls return nil.
func (r *ZCRXReceiver) Close() error { return nil }

// Active always returns false on Darwin.
func (r *ZCRXReceiver) Active() bool { return false }

// NoopZCRXHandler provides default ZCRXHandler behavior.
// Embed to override selectively.
type NoopZCRXHandler struct{}

func (NoopZCRXHandler) OnData(buf *ZCRXBuffer) bool {
	return buf.Release() == nil
}
func (NoopZCRXHandler) OnError(error) bool { return false }
func (NoopZCRXHandler) OnStopped()         {}

var _ ZCRXHandler = NoopZCRXHandler{}

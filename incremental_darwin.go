// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import "code.hybscloud.com/iofd"

// IncrementalHandler handles incremental receive completion events.
type IncrementalHandler interface {
	OnData(buf []byte, hasMore bool)
	OnComplete()
	OnError(err error)
}

// IncrementalReceiver manages receive operations with incremental buffer consumption.
type IncrementalReceiver struct{}

// NewIncrementalReceiver creates a receiver (stub on Darwin).
func NewIncrementalReceiver(ring *Uring, pool *ContextPools, groupID uint16, bufSize int, bufBacking []byte, entries int) *IncrementalReceiver {
	return &IncrementalReceiver{}
}

// Recv is a stub on Darwin.
func (r *IncrementalReceiver) Recv(fd iofd.FD, handler IncrementalHandler) error {
	return ErrNotSupported
}

// HandleCQE is a stub on Darwin.
func (r *IncrementalReceiver) HandleCQE(cqe CQEView) bool {
	return false
}

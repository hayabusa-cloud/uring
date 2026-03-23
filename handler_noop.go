// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "code.hybscloud.com/iofd"

// NoopMultishotHandler provides default implementations for MultishotHandler.
// Embed this in custom handlers to only override methods you need.
//
// Default behavior:
//   - successful steps continue
//   - error steps stop
//   - terminal stop is ignored
//
// Example:
//
//	type myObserver struct {
//	   uring.NoopMultishotHandler
//	   connections int
//	}
//
//	func (o *myObserver) OnMultishotStep(step uring.MultishotStep) uring.MultishotAction {
//	   if step.Err == nil && step.CQE.Res >= 0 {
//	       o.connections++
//	       return uring.MultishotContinue
//	   }
//	   return o.NoopMultishotHandler.OnMultishotStep(step)
//	}
type NoopMultishotHandler struct{}

// OnMultishotStep returns the default action for the observed step.
func (NoopMultishotHandler) OnMultishotStep(step MultishotStep) MultishotAction {
	if step.Err == nil {
		return MultishotContinue
	}
	return MultishotStop
}

// OnMultishotStop is a no-op.
func (NoopMultishotHandler) OnMultishotStop(error, bool) {}

// Verify interface compliance.
var _ MultishotHandler = NoopMultishotHandler{}

// NoopListenerHandler provides default implementations for ListenerHandler.
// Embed this in custom handlers to only override methods you need.
//
// Default behavior:
//   - OnSocketCreated: returns true (continue to bind)
//   - OnBound: returns true (continue to listen)
//   - OnListening: no-op
//   - OnError: no-op
//
// Example:
//
//	type myHandler struct {
//	   uring.NoopListenerHandler
//	   fd iofd.FD
//	}
//
//	func (h *myHandler) OnSocketCreated(fd iofd.FD) bool {
//	   h.fd = fd
//	   // Set custom socket options here
//	   return true
//	}
type NoopListenerHandler struct{}

// OnSocketCreated returns true to continue the chain.
func (NoopListenerHandler) OnSocketCreated(iofd.FD) bool { return true }

// OnBound returns true to continue the chain.
func (NoopListenerHandler) OnBound() bool { return true }

// OnListening is a no-op.
func (NoopListenerHandler) OnListening() {}

// OnError is a no-op.
func (NoopListenerHandler) OnError(uint8, error) {}

// Verify interface compliance.
var _ ListenerHandler = NoopListenerHandler{}

// NoopIncrementalHandler provides default implementations for IncrementalHandler.
// Embed this in custom handlers to only override methods you need.
//
// Default behavior:
//   - OnData: no-op
//   - OnComplete: no-op
//   - OnError: no-op
//
// Example:
//
//	type myHandler struct {
//	   uring.NoopIncrementalHandler
//	   totalBytes int
//	}
//
//	func (h *myHandler) OnData(buf []byte, hasMore bool) {
//	   h.totalBytes += len(buf)
//	}
type NoopIncrementalHandler struct{}

// OnData is a no-op.
func (NoopIncrementalHandler) OnData([]byte, bool) {}

// OnComplete is a no-op.
func (NoopIncrementalHandler) OnComplete() {}

// OnError is a no-op.
func (NoopIncrementalHandler) OnError(error) {}

// Verify interface compliance.
var _ IncrementalHandler = NoopIncrementalHandler{}

// NoopZCHandler provides default implementations for ZCHandler.
// Embed this in custom handlers to only override methods you need.
//
// Default behavior:
//   - OnCompleted: no-op
//   - OnNotification: no-op
//
// Example:
//
//	type myHandler struct {
//	   uring.NoopZCHandler
//	   bytesSent int32
//	}
//
//	func (h *myHandler) OnCompleted(result int32) {
//	   if result > 0 {
//	       h.bytesSent += result
//	   }
//	}
type NoopZCHandler struct{}

// OnCompleted is a no-op.
func (NoopZCHandler) OnCompleted(int32) {}

// OnNotification is a no-op.
func (NoopZCHandler) OnNotification(int32) {}

// Verify interface compliance.
var _ ZCHandler = NoopZCHandler{}

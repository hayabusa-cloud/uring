// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"errors"
	"testing"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/uring"
)

// Test MultishotSubscriber builder pattern
func TestMultishotSubscriber(t *testing.T) {
	resultCalled := false
	errorCalled := false
	stopCalled := false

	sub := uring.NewMultishotSubscriber().
		OnStep(func(step uring.MultishotStep) uring.MultishotAction {
			if step.Err == nil {
				resultCalled = true
				if step.CQE.Res >= 0 {
					return uring.MultishotContinue
				}
			}
			errorCalled = true
			return uring.MultishotStop
		}).
		OnStop(func(error, bool) {
			stopCalled = true
		})

	handler := sub.Handler()

	// Test result step.
	if handler.OnMultishotStep(uring.MultishotStep{CQE: uring.CQEView{Res: 42, Flags: uring.IORING_CQE_F_MORE}}) != uring.MultishotContinue {
		t.Error("result step should return MultishotContinue for positive result")
	}
	if !resultCalled {
		t.Error("result callback not called")
	}

	// Test error step.
	if handler.OnMultishotStep(uring.MultishotStep{CQE: uring.CQEView{Res: -1, Flags: uring.IORING_CQE_F_MORE}, Err: errors.New("boom")}) != uring.MultishotStop {
		t.Error("error step should return MultishotStop")
	}
	if !errorCalled {
		t.Error("error callback not called")
	}

	// Test stop callback.
	handler.OnMultishotStop(nil, false)
	if !stopCalled {
		t.Error("stop callback not called")
	}
}

// Test ListenerSubscriber builder pattern
func TestListenerSubscriber(t *testing.T) {
	socketCreatedCalled := false
	boundCalled := false
	listeningCalled := false
	errorCalled := false

	sub := uring.NewListenerSubscriber().
		OnSocketCreated(func(fd iofd.FD) bool {
			socketCreatedCalled = true
			return fd >= 0
		}).
		OnBound(func() bool {
			boundCalled = true
			return true
		}).
		OnListening(func() {
			listeningCalled = true
		}).
		OnError(func(op uint8, err error) {
			errorCalled = true
		})

	handler := sub.Handler()

	// Test OnSocketCreated
	if !handler.OnSocketCreated(5) {
		t.Error("OnSocketCreated should return true for valid fd")
	}
	if !socketCreatedCalled {
		t.Error("OnSocketCreated callback not called")
	}

	// Test OnBound
	if !handler.OnBound() {
		t.Error("OnBound should return true")
	}
	if !boundCalled {
		t.Error("OnBound callback not called")
	}

	// Test OnListening
	handler.OnListening()
	if !listeningCalled {
		t.Error("OnListening callback not called")
	}

	// Test OnError
	handler.OnError(1, nil)
	if !errorCalled {
		t.Error("OnError callback not called")
	}
}

// Test IncrementalSubscriber builder pattern
func TestIncrementalSubscriber(t *testing.T) {
	dataCalled := false
	completeCalled := false
	errorCalled := false

	sub := uring.NewIncrementalSubscriber().
		OnData(func(buf []byte, hasMore bool) {
			dataCalled = true
		}).
		OnComplete(func() {
			completeCalled = true
		}).
		OnError(func(err error) {
			errorCalled = true
		})

	handler := sub.Handler()

	// Test OnData
	handler.OnData([]byte("test"), true)
	if !dataCalled {
		t.Error("OnData callback not called")
	}

	// Test OnComplete
	handler.OnComplete()
	if !completeCalled {
		t.Error("OnComplete callback not called")
	}

	// Test OnError
	handler.OnError(nil)
	if !errorCalled {
		t.Error("OnError callback not called")
	}
}

// Test ZCSubscriber builder pattern
func TestZCSubscriber(t *testing.T) {
	completedCalled := false
	notificationCalled := false

	sub := uring.NewZCSubscriber().
		OnCompleted(func(result int32) {
			completedCalled = true
		}).
		OnNotification(func(result int32) {
			notificationCalled = true
		})

	handler := sub.Handler()

	// Test OnCompleted
	handler.OnCompleted(100)
	if !completedCalled {
		t.Error("OnCompleted callback not called")
	}

	// Test OnNotification
	handler.OnNotification(0)
	if !notificationCalled {
		t.Error("OnNotification callback not called")
	}
}

// Test NoopMultishotHandler default behavior
func TestNoopMultishotHandler(t *testing.T) {
	var handler uring.NoopMultishotHandler

	// Result steps should continue.
	if handler.OnMultishotStep(uring.MultishotStep{CQE: uring.CQEView{Flags: uring.IORING_CQE_F_MORE}}) != uring.MultishotContinue {
		t.Error("NoopMultishotHandler.OnMultishotStep(result) should return MultishotContinue")
	}

	// Error steps should stop.
	if handler.OnMultishotStep(uring.MultishotStep{CQE: uring.CQEView{Res: -1, Flags: uring.IORING_CQE_F_MORE}, Err: errors.New("boom")}) != uring.MultishotStop {
		t.Error("NoopMultishotHandler.OnMultishotStep(error) should return MultishotStop")
	}

	// Stop callbacks should not panic.
	handler.OnMultishotStop(nil, false)
}

// Test NoopListenerHandler default behavior
func TestNoopListenerHandler(t *testing.T) {
	var handler uring.NoopListenerHandler

	// OnSocketCreated should return true
	if !handler.OnSocketCreated(5) {
		t.Error("NoopListenerHandler.OnSocketCreated should return true")
	}

	// OnBound should return true
	if !handler.OnBound() {
		t.Error("NoopListenerHandler.OnBound should return true")
	}

	// OnListening should not panic
	handler.OnListening()

	// OnError should not panic
	handler.OnError(0, nil)
}

// Test NoopIncrementalHandler default behavior
func TestNoopIncrementalHandler(t *testing.T) {
	var handler uring.NoopIncrementalHandler

	// All methods should not panic
	handler.OnData([]byte("test"), true)
	handler.OnComplete()
	handler.OnError(nil)
}

// Test NoopZCHandler default behavior
func TestNoopZCHandler(t *testing.T) {
	var handler uring.NoopZCHandler

	// All methods should not panic
	handler.OnCompleted(100)
	handler.OnNotification(0)
}

// Test embedding NoopMultishotHandler
func TestNoopMultishotHandlerEmbedding(t *testing.T) {
	type customHandler struct {
		uring.NoopMultishotHandler
		count int
	}

	handler := &customHandler{}

	// Override result handling.
	onResult := func(cqe uring.CQEView) uring.MultishotAction {
		handler.count++
		return handler.NoopMultishotHandler.OnMultishotStep(uring.MultishotStep{CQE: cqe})
	}

	// Simulate calling the overridden method
	if onResult(uring.CQEView{Res: 1}) != uring.MultishotContinue {
		t.Error("embedded handler should return MultishotContinue")
	}
	if handler.count != 1 {
		t.Errorf("count = %d, want 1", handler.count)
	}
}

// Test MultishotSubscriber default handlers
func TestMultishotSubscriberDefaults(t *testing.T) {
	// Test with NO overrides - use default handlers
	sub := uring.NewMultishotSubscriber()
	handler := sub.Handler()

	// Default successful step handling returns continue.
	if handler.OnMultishotStep(uring.MultishotStep{CQE: uring.CQEView{Res: 1, Flags: uring.IORING_CQE_F_MORE}}) != uring.MultishotContinue {
		t.Error("default successful step handling should return MultishotContinue")
	}

	// Default error step handling returns stop.
	if handler.OnMultishotStep(uring.MultishotStep{CQE: uring.CQEView{Res: -1, Flags: uring.IORING_CQE_F_MORE}, Err: errors.New("boom")}) != uring.MultishotStop {
		t.Error("default error step handling should return MultishotStop")
	}

	// Default stop handling is no-op (should not panic).
	handler.OnMultishotStop(nil, false)
}

// Test ListenerSubscriber default handlers
func TestListenerSubscriberDefaults(t *testing.T) {
	// Test with NO overrides - use default handlers
	sub := uring.NewListenerSubscriber()
	handler := sub.Handler()

	// Default OnSocketCreated returns true
	if !handler.OnSocketCreated(5) {
		t.Error("default OnSocketCreated should return true")
	}

	// Default OnBound returns true
	if !handler.OnBound() {
		t.Error("default OnBound should return true")
	}

	// Default OnListening is no-op (should not panic)
	handler.OnListening()

	// Default OnError is no-op (should not panic)
	handler.OnError(0, nil)
}

// Test MultishotSubscriber direct callback methods
func TestMultishotSubscriberCallbacks(t *testing.T) {
	resultCalled := false
	errorCalled := false
	stopCalled := false

	sub := uring.NewMultishotSubscriber().
		OnStep(func(step uring.MultishotStep) uring.MultishotAction {
			if step.Err == nil {
				resultCalled = true
				return uring.MultishotContinue
			}
			errorCalled = true
			return uring.MultishotStop
		}).
		OnStop(func(error, bool) {
			stopCalled = true
		})

	// Test OnMultishotStep directly.
	if sub.OnMultishotStep(uring.MultishotStep{CQE: uring.CQEView{Res: -1, Flags: uring.IORING_CQE_F_MORE}, Err: errors.New("boom")}) != uring.MultishotStop {
		t.Error("error step should return MultishotStop")
	}
	if !errorCalled {
		t.Error("error callback not called")
	}

	if sub.OnMultishotStep(uring.MultishotStep{CQE: uring.CQEView{Res: 1, Flags: uring.IORING_CQE_F_MORE}}) != uring.MultishotContinue {
		t.Error("result step should return MultishotContinue")
	}
	if !resultCalled {
		t.Error("result callback not called")
	}

	// Test stop callback directly.
	sub.OnMultishotStop(nil, false)
	if !stopCalled {
		t.Error("stop callback not called")
	}
}

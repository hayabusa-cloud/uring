// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "code.hybscloud.com/iofd"

// MultishotSubscriber adapts functions to `MultishotHandler`.
type MultishotSubscriber struct {
	onStep func(step MultishotStep) MultishotAction
	onStop func(err error, cancelled bool)
}

// NewMultishotSubscriber creates a subscriber with default handlers.
func NewMultishotSubscriber() *MultishotSubscriber {
	return &MultishotSubscriber{
		onStep: func(step MultishotStep) MultishotAction {
			if step.Err == nil {
				return MultishotContinue
			}
			return MultishotStop
		},
		onStop: func(error, bool) {},
	}
}

// OnStep sets the multishot step handler.
func (s *MultishotSubscriber) OnStep(fn func(step MultishotStep) MultishotAction) *MultishotSubscriber {
	s.onStep = fn
	return s
}

// OnStop sets the multishot terminal stop handler.
func (s *MultishotSubscriber) OnStop(fn func(err error, cancelled bool)) *MultishotSubscriber {
	s.onStop = fn
	return s
}

// OnMultishotStep implements MultishotHandler.
func (s *MultishotSubscriber) OnMultishotStep(step MultishotStep) MultishotAction {
	return s.onStep(step)
}

// OnMultishotStop implements MultishotHandler.
func (s *MultishotSubscriber) OnMultishotStop(err error, cancelled bool) {
	s.onStop(err, cancelled)
}

// Handler returns `s` as a `MultishotHandler`.
func (s *MultishotSubscriber) Handler() MultishotHandler {
	return s
}

var _ MultishotHandler = (*MultishotSubscriber)(nil)

// ListenerSubscriber adapts functions to `ListenerHandler`.
type ListenerSubscriber struct {
	onSocketCreated func(fd iofd.FD) bool
	onBound         func() bool
	onListening     func()
	onError         func(op uint8, err error)
}

// NewListenerSubscriber creates a subscriber with default handlers.
func NewListenerSubscriber() *ListenerSubscriber {
	return &ListenerSubscriber{
		onSocketCreated: func(iofd.FD) bool { return true },
		onBound:         func() bool { return true },
		onListening:     func() {},
		onError:         func(uint8, error) {},
	}
}

// OnSocketCreated sets the socket-created handler.
func (s *ListenerSubscriber) OnSocketCreated(fn func(fd iofd.FD) bool) *ListenerSubscriber {
	s.onSocketCreated = fn
	return s
}

// OnBound sets the bound handler.
func (s *ListenerSubscriber) OnBound(fn func() bool) *ListenerSubscriber {
	s.onBound = fn
	return s
}

// OnListening sets the listening handler.
func (s *ListenerSubscriber) OnListening(fn func()) *ListenerSubscriber {
	s.onListening = fn
	return s
}

// OnError sets the error handler.
func (s *ListenerSubscriber) OnError(fn func(op uint8, err error)) *ListenerSubscriber {
	s.onError = fn
	return s
}

// listenerSubscriberAdapter forwards to ListenerSubscriber.
var _ ListenerHandler = (*listenerSubscriberAdapter)(nil)

type listenerSubscriberAdapter struct {
	*ListenerSubscriber
}

func (a *listenerSubscriberAdapter) OnSocketCreated(fd iofd.FD) bool { return a.onSocketCreated(fd) }
func (a *listenerSubscriberAdapter) OnBound() bool                   { return a.onBound() }
func (a *listenerSubscriberAdapter) OnListening()                    { a.onListening() }
func (a *listenerSubscriberAdapter) OnError(op uint8, err error)     { a.onError(op, err) }

// Handler returns `s` as a `ListenerHandler`.
func (s *ListenerSubscriber) Handler() ListenerHandler {
	return &listenerSubscriberAdapter{s}
}

// IncrementalSubscriber adapts functions to `IncrementalHandler`.
type IncrementalSubscriber struct {
	onData     func(buf []byte, hasMore bool)
	onComplete func()
	onError    func(err error)
}

// NewIncrementalSubscriber creates a subscriber with default handlers.
func NewIncrementalSubscriber() *IncrementalSubscriber {
	return &IncrementalSubscriber{
		onData:     func([]byte, bool) {},
		onComplete: func() {},
		onError:    func(error) {},
	}
}

// OnData sets the data handler.
func (s *IncrementalSubscriber) OnData(fn func(buf []byte, hasMore bool)) *IncrementalSubscriber {
	s.onData = fn
	return s
}

// OnComplete sets the completion handler.
func (s *IncrementalSubscriber) OnComplete(fn func()) *IncrementalSubscriber {
	s.onComplete = fn
	return s
}

// OnError sets the error handler.
func (s *IncrementalSubscriber) OnError(fn func(err error)) *IncrementalSubscriber {
	s.onError = fn
	return s
}

// incrementalSubscriberAdapter forwards to IncrementalSubscriber.
var _ IncrementalHandler = (*incrementalSubscriberAdapter)(nil)

type incrementalSubscriberAdapter struct {
	*IncrementalSubscriber
}

func (a *incrementalSubscriberAdapter) OnData(buf []byte, hasMore bool) { a.onData(buf, hasMore) }
func (a *incrementalSubscriberAdapter) OnComplete()                     { a.onComplete() }
func (a *incrementalSubscriberAdapter) OnError(err error)               { a.onError(err) }

// Handler returns `s` as an `IncrementalHandler`.
func (s *IncrementalSubscriber) Handler() IncrementalHandler {
	return &incrementalSubscriberAdapter{s}
}

// ZCSubscriber adapts functions to `ZCHandler`.
type ZCSubscriber struct {
	onCompleted    func(result int32)
	onNotification func(result int32)
}

// NewZCSubscriber creates a subscriber with default handlers.
func NewZCSubscriber() *ZCSubscriber {
	return &ZCSubscriber{
		onCompleted:    func(int32) {},
		onNotification: func(int32) {},
	}
}

// OnCompleted sets the completed handler.
func (s *ZCSubscriber) OnCompleted(fn func(result int32)) *ZCSubscriber {
	s.onCompleted = fn
	return s
}

// OnNotification sets the notification handler.
func (s *ZCSubscriber) OnNotification(fn func(result int32)) *ZCSubscriber {
	s.onNotification = fn
	return s
}

// zcSubscriberAdapter forwards to ZCSubscriber.
var _ ZCHandler = (*zcSubscriberAdapter)(nil)

type zcSubscriberAdapter struct {
	*ZCSubscriber
}

func (a *zcSubscriberAdapter) OnCompleted(result int32)    { a.onCompleted(result) }
func (a *zcSubscriberAdapter) OnNotification(result int32) { a.onNotification(result) }

// Handler returns `s` as a `ZCHandler`.
func (s *ZCSubscriber) Handler() ZCHandler {
	return &zcSubscriberAdapter{s}
}

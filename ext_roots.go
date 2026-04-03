// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package uring

import "unsafe"

// slotOwner is the entity that owns a pooled ExtSQE slot.
// Concrete types: *MultishotSubscription, *IncrementalReceiver, *ZCTracker, *ZCRXReceiver.
type slotOwner interface{}

// completionHandler is the callback target for CQE dispatch.
// Concrete types: IncrementalHandler, ListenerHandler, ZCHandler.
type completionHandler interface{}

// extSQERoots keeps Go-visible roots adjacent to a pooled ExtSQE slot.
// ExtSQE.UserData remains raw scalar storage; live Go refs stay here.
type extSQERoots struct {
	owner   slotOwner
	handler completionHandler
}

func (r *extSQERoots) clear() {
	r.owner = nil
	r.handler = nil
}

// pooledExtSQE keeps the borrowed ExtSQE together with GC-visible roots.
// ExtSQE must stay first so *ExtSQE can recover its slot roots via extRoots.
type pooledExtSQE struct {
	ext   ExtSQE
	roots extSQERoots
}

func extRoots(ext *ExtSQE) *extSQERoots {
	return &(*pooledExtSQE)(unsafe.Pointer(ext)).roots
}

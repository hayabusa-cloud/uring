// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package uring

import "unsafe"

// slotOwner is the entity that owns a pooled ExtSQE slot.
// Concrete types: *ListenerOp, *MultishotSubscription, *IncrementalReceiver, *ZCTracker, *ZCRXReceiver.
type slotOwner interface{}

// completionHandler is the callback target for CQE dispatch.
// Concrete types: IncrementalHandler, ListenerHandler, ZCHandler.
type completionHandler interface{}

// extSQEAnchors keeps Go-visible anchors adjacent to a pooled ExtSQE slot.
// ExtSQE.UserData remains raw scalar storage; live Go refs stay here
// so the GC does not collect objects the kernel still references.
type extSQEAnchors struct {
	owner    slotOwner
	handler  completionHandler
	sockaddr Sockaddr
}

func (a *extSQEAnchors) clear() {
	a.owner = nil
	a.handler = nil
	a.sockaddr = nil
}

// pooledExtSQE keeps the borrowed ExtSQE together with GC-visible anchors.
// ExtSQE must stay first so *ExtSQE can recover its slot anchors via extAnchors.
type pooledExtSQE struct {
	ext     ExtSQE
	anchors extSQEAnchors
}

func extAnchors(ext *ExtSQE) *extSQEAnchors {
	return &(*pooledExtSQE)(unsafe.Pointer(ext)).anchors
}

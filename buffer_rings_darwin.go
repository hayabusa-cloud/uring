// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

// uringBufferRings manages buffer rings for io_uring buffer selection.
// On Darwin, this is a simplified implementation since there are no kernel buffer rings.
type uringBufferRings struct {
	rings []*ioUringBufRing
}

func newUringBufferRings() *uringBufferRings {
	return &uringBufferRings{
		rings: make([]*ioUringBufRing, 0),
	}
}

func (r *uringBufferRings) registerBuffers(ur *ioUring, buffers *uringProvideBuffers) error {
	return buffers.register(ur, 0)
}

func (r *uringBufferRings) registerGroups(ur *ioUring, groups *uringProvideBufferGroups) error {
	return groups.register(ur, 0)
}

func (r *uringBufferRings) advance(ur *ioUring) {
	// No-op on Darwin simulator
}

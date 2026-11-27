// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

import "code.hybscloud.com/iofd"

// waitBatchDirect is the internal Direct mode batch retrieval.
func (ur *ioUring) waitBatchDirect(cqes []DirectCQE) (int, error) {
	if len(cqes) == 0 {
		return 0, nil
	}

	claim, available, err := ur.claimCQ(len(cqes))
	if err != nil {
		return 0, err
	}

	h, mask := uint32(claim), *ur.cq.kRingMask
	n := int(available)
	for i := range n {
		e := ur.cq.cqeAt((h + uint32(i)) & mask)
		ctx := e.Context()
		cqes[i] = DirectCQE{
			Res:      e.res,
			Flags:    e.flags,
			Op:       ctx.Op(),
			SQEFlags: ctx.Flags(),
			BufGroup: ctx.BufGroup(),
			FD:       iofd.FD(ctx.FD()),
		}
	}
	ur.publishCQ(claim, available)
	return n, nil
}

// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring

// waitBatchExtended is the internal Extended mode batch retrieval.
func (ur *ioUring) waitBatchExtended(cqes []ExtCQE) (int, error) {
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
		cqes[i] = e.extendedCQE()
	}
	ur.publishCQ(claim, available)
	return n, nil
}

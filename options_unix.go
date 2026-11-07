// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build unix

package uring

const (
	providedBufferValueMin     = 2
	providedBufferValueMax     = 1 << 28
	providedBufferGroupEntries = 1 << 15
	multiSizeBufferScaleMax    = 1 << 12
	bufferGroupIDCardinality   = 1 << 16
	defaultBufferGroupTiers    = bufferGroupIndexLarge + 1
)

func (opt Options) validate() error {
	maxLockedBufferMem := registerBufferSize * registerBufferNum
	if opt.Entries < 1 || opt.Entries > EntriesHuge ||
		opt.LockedBufferMem < registerBufferSize ||
		opt.LockedBufferMem > maxLockedBufferMem ||
		opt.LockedBufferMem%registerBufferSize != 0 ||
		opt.MultiSizeBuffer < 0 || opt.MultiSizeBuffer > multiSizeBufferScaleMax ||
		opt.ReadBufferGidOffset < 0 {
		return ErrInvalidParam
	}

	groupSpan := 1
	if opt.MultiSizeBuffer == 0 {
		if opt.ReadBufferSize < providedBufferValueMin || opt.ReadBufferSize > providedBufferValueMax ||
			opt.ReadBufferNum < providedBufferValueMin || opt.ReadBufferNum > providedBufferValueMax {
			return ErrInvalidParam
		}
		bufferNum := roundToPowerOf2(opt.ReadBufferNum)
		if bufferNum > providedBufferGroupEntries {
			groupSpan = bufferNum / providedBufferGroupEntries
		}
	} else {
		scale := roundToPowerOf2(opt.MultiSizeBuffer)
		groupSpan = (scale-1)*int(bufferGroupIndexEnd) + defaultBufferGroupTiers
	}
	if opt.ReadBufferGidOffset > bufferGroupIDCardinality-groupSpan {
		return ErrInvalidParam
	}
	return nil
}

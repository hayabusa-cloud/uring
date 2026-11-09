// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.
// Refactored from code.hybscloud.com/sox.

//go:build linux || darwin

package uring

import "code.hybscloud.com/iofd"

// SendTargets represents a caller-owned ordered set of target sockets.
// The count and descriptors must remain stable during a multicast call.
type SendTargets interface {
	Count() int
	FD(i int) iofd.FD
}

// Multicast submits sends to each target in order, automatically selecting
// copy or zero-copy by payload size and target count. bufIndex selects a
// registered buffer; use -1 to send from p. offset and n select the payload.
// Unregistered buffers always use ordinary sends. Registered buffers use
// zero-copy at 8 KiB for fewer than 8 targets, 4 KiB for 8-63, 3 KiB for 64-511,
// 2 KiB for 512-4095, and 1.5 KiB for 4096 or more. These are workload heuristics;
// MulticastZeroCopy uses lower thresholds to favor zero-copy.
//
// sqeCtx must use Direct mode so each target retains its own completion identity.
// For pointer-based contexts, submit each send with a separate context instead.
//
// The return count is the accepted target prefix, not completed sends or bytes.
// Submission stops at the first error, including ErrWouldBlock. Resume with the
// remaining targets; replaying the accepted prefix can duplicate delivery.
// A stream send may complete with fewer than n bytes. When completions are
// enabled, inspect each target's result and resume only its unsent suffix.
// Keep the payload valid and unmodified until all accepted sends release it.
// Ordinary sends follow NotifySucceed. Selected zero-copy sends always request
// completion evidence and reject explicit IOSQE_CQE_SKIP_SUCCESS; their borrows
// end at notification or terminal data without MORE. Inspect each CQE's opcode
// and flags: resuming with fewer targets can change the selected operation.
func (ur *Uring) Multicast(sqeCtx SQEContext, targets SendTargets, bufIndex int, p []byte, offset int64, n int, options ...OpOptionFunc) (int, error) {
	count := targets.Count()
	if count == 0 {
		return 0, nil
	}
	if !sqeCtx.IsDirect() || count < 0 || n < 0 {
		return 0, ErrInvalidParam
	}
	buf := p
	useZeroCopy := false
	if bufIndex >= 0 {
		buf = ur.RegisteredBuffer(bufIndex)
		if buf == nil {
			return 0, ErrInvalidParam
		}
		threshold := 8192
		switch {
		case count >= 4096:
			threshold = 1536
		case count >= 512:
			threshold = 2048
		case count >= 64:
			threshold = 3072
		case count >= 8:
			threshold = 4096
		}
		useZeroCopy = n >= threshold
	}
	if offset < 0 || offset > int64(len(buf)) || n > len(buf)-int(offset) {
		return 0, ErrInvalidParam
	}
	ctx := sqeCtx.OrFlags(ur.operationOptions(options))
	if useZeroCopy {
		if ctx.Flags()&IOSQE_CQE_SKIP_SUCCESS != 0 {
			return 0, ErrInvalidParam
		}
	} else {
		ctx = ctx.OrFlags(ur.writeLikeOpFlags)
	}
	for i := range count {
		targetCtx := ctx.WithFD(targets.FD(i))
		var err error
		if useZeroCopy {
			err = ur.sendZeroCopyFixed(targetCtx, bufIndex, uint64(offset), n, 0)
		} else if bufIndex >= 0 {
			err = ur.sendFixed(targetCtx, bufIndex, uint64(offset), n, 0)
		} else {
			err = ur.send(targetCtx, 0, p, uint64(offset), n)
		}
		if err != nil {
			return i, err
		}
	}
	return count, nil
}

// MulticastZeroCopy submits sends from a registered buffer to each target,
// automatically favoring zero-copy at lower thresholds than Multicast.
// It selects zero-copy at 1.5 KiB for fewer than 4 targets, 1 KiB for 4-15,
// 512 bytes for 16-63, 128 bytes for 64-255, and any size for 256 or more.
// Smaller payloads use ordinary sends. These thresholds are workload heuristics.
// It uses the same Direct-context and accepted-prefix contract as Multicast.
// Each target's data CQE reports byte progress and may describe a short stream
// send. Caller code owns any submission of that target's unsent suffix.
//
// Both selected paths request completion evidence, independent of NotifySucceed.
// Explicit IOSQE_CQE_SKIP_SUCCESS is invalid. Keep the buffer range immutable
// until every accepted target's ordinary-send completion, or zero-copy
// notification or terminal data CQE without MORE. Inspect each CQE's opcode
// and flags; a resumed call can select a different operation. Kernel errors
// remain visible; this helper does not retry a rejected zero-copy operation.
func (ur *Uring) MulticastZeroCopy(sqeCtx SQEContext, targets SendTargets, bufIndex int, offset int64, n int, options ...OpOptionFunc) (int, error) {
	count := targets.Count()
	if count == 0 {
		return 0, nil
	}
	if !sqeCtx.IsDirect() || count < 0 || n < 0 {
		return 0, ErrInvalidParam
	}
	buf := ur.RegisteredBuffer(bufIndex)
	if buf == nil || offset < 0 || offset > int64(len(buf)) || n > len(buf)-int(offset) {
		return 0, ErrInvalidParam
	}
	ctx := sqeCtx.OrFlags(ur.operationOptions(options))
	if ctx.Flags()&IOSQE_CQE_SKIP_SUCCESS != 0 {
		return 0, ErrInvalidParam
	}
	threshold := 1536
	switch {
	case count >= 256:
		threshold = 0
	case count >= 64:
		threshold = 128
	case count >= 16:
		threshold = 512
	case count >= 4:
		threshold = 1024
	}
	useZeroCopy := n >= threshold
	for i := range count {
		targetCtx := ctx.WithFD(targets.FD(i))
		var err error
		if useZeroCopy {
			err = ur.sendZeroCopyFixed(targetCtx, bufIndex, uint64(offset), n, 0)
		} else {
			err = ur.sendFixed(targetCtx, bufIndex, uint64(offset), n, 0)
		}
		if err != nil {
			return i, err
		}
	}
	return count, nil
}

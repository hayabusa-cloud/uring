// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
)

func (ur *ioUring) waitBatchDirect(cqes []DirectCQE) (int, error) {
	if len(cqes) == 0 {
		return 0, nil
	}

	n := 0
	for n < len(cqes) {
		select {
		case cqe := <-ur.cqChan:
			if cqe != nil {
				ctx := cqe.Context()
				cqes[n] = DirectCQE{
					Res:      cqe.res,
					Flags:    cqe.flags,
					Op:       ctx.Op(),
					SQEFlags: ctx.Flags(),
					BufGroup: ctx.BufGroup(),
					FD:       iofd.FD(ctx.FD()),
				}
				n++
			}
		default:
			if n > 0 {
				return n, nil
			}
			return 0, iox.ErrWouldBlock
		}
	}
	return n, nil
}

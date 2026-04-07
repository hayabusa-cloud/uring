// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
)

// DirectCQE is a zero-overhead CQE for Direct mode operations.
type DirectCQE struct {
	Res      int32
	Flags    uint32
	Op       uint8
	SQEFlags uint8
	BufGroup uint16
	FD       iofd.FD
}

func (c *DirectCQE) IsSuccess() bool      { return cqeIsSuccess(c.Res) }
func (c *DirectCQE) HasMore() bool        { return cqeHasMore(c.Flags) }
func (c *DirectCQE) HasBuffer() bool      { return cqeHasBuffer(c.Flags) }
func (c *DirectCQE) BufID() uint16        { return cqeBufID(c.Flags) }
func (c *DirectCQE) IsNotification() bool { return cqeIsNotification(c.Flags) }

// WaitDirect retrieves completion events using Direct mode fast-path (darwin stub).
func (ur *Uring) WaitDirect(cqes []DirectCQE) (int, error) {
	if err := ur.ioUring.enter(); err != nil {
		return 0, err
	}
	return ur.ioUring.waitBatchDirect(cqes)
}

func (ur *ioUring) waitBatchDirect(cqes []DirectCQE) (int, error) {
	if len(cqes) == 0 {
		return 0, nil
	}

	n := 0
	for n < len(cqes) {
		select {
		case cqe := <-ur.cqChan:
			if cqe != nil {
				ctx := SQEContextFromRaw(cqe.userData)
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

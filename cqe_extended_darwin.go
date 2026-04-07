// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import (
	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
)

// ExtCQE is a zero-overhead CQE for Extended mode operations.
type ExtCQE struct {
	Res   int32
	Flags uint32
	Ext   *ExtSQE
}

func (c *ExtCQE) IsSuccess() bool      { return cqeIsSuccess(c.Res) }
func (c *ExtCQE) HasMore() bool        { return cqeHasMore(c.Flags) }
func (c *ExtCQE) HasBuffer() bool      { return cqeHasBuffer(c.Flags) }
func (c *ExtCQE) BufID() uint16        { return cqeBufID(c.Flags) }
func (c *ExtCQE) IsNotification() bool { return cqeIsNotification(c.Flags) }
func (c *ExtCQE) HasBufferMore() bool  { return cqeHasBufferMore(c.Flags) }
func (c *ExtCQE) Op() uint8            { return c.Ext.SQE.opcode }
func (c *ExtCQE) FD() iofd.FD          { return iofd.FD(c.Ext.SQE.fd) }

// WaitExtended retrieves completion events using Extended mode fast-path (darwin stub).
func (ur *Uring) WaitExtended(cqes []ExtCQE) (int, error) {
	if err := ur.ioUring.enter(); err != nil {
		return 0, err
	}
	return ur.ioUring.waitBatchExtended(cqes)
}

func (ur *ioUring) waitBatchExtended(cqes []ExtCQE) (int, error) {
	if len(cqes) == 0 {
		return 0, nil
	}

	n := 0
	for n < len(cqes) {
		select {
		case cqe := <-ur.cqChan:
			if cqe != nil {
				ctx := SQEContextFromRaw(cqe.userData)
				var ext *ExtSQE
				if ctx.Mode() == CtxModeExtended {
					ext = ctx.ExtSQE()
				}
				cqes[n] = ExtCQE{
					Res:   cqe.res,
					Flags: cqe.flags,
					Ext:   ext,
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

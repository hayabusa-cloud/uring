// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

import "code.hybscloud.com/iox"

// ExtendedCQE is a zero-overhead CQE for Extended mode operations.
type ExtendedCQE struct {
	Res   int32
	Flags uint32
	Ext   *ExtSQE
}

func (c *ExtendedCQE) IsSuccess() bool      { return c.Res >= 0 }
func (c *ExtendedCQE) HasMore() bool        { return c.Flags&IORING_CQE_F_MORE != 0 }
func (c *ExtendedCQE) HasBuffer() bool      { return c.Flags&IORING_CQE_F_BUFFER != 0 }
func (c *ExtendedCQE) BufID() uint16        { return uint16(c.Flags >> IORING_CQE_BUFFER_SHIFT) }
func (c *ExtendedCQE) IsNotification() bool { return c.Flags&IORING_CQE_F_NOTIF != 0 }
func (c *ExtendedCQE) HasBufferMore() bool  { return c.Flags&IORING_CQE_F_BUF_MORE != 0 }
func (c *ExtendedCQE) Op() uint8            { return c.Ext.SQE.opcode }
func (c *ExtendedCQE) FD() int32            { return c.Ext.SQE.fd }

// WaitExtended retrieves completion events using Extended mode fast-path (darwin stub).
func (ur *Uring) WaitExtended(cqes []ExtendedCQE) (int, error) {
	if err := ur.ioUring.enter(); err != nil {
		return 0, err
	}
	return ur.ioUring.waitBatchExtended(cqes)
}

func (ur *ioUring) waitBatchExtended(cqes []ExtendedCQE) (int, error) {
	if len(cqes) == 0 {
		return 0, nil
	}

	n := 0
	for n < len(cqes) {
		select {
		case cqe := <-ur.cqChan:
			if cqe != nil {
				ctx := SQEContext(cqe.userData)
				var ext *ExtSQE
				if ctx.Mode() == CtxModeExtended {
					ext = ctx.ExtSQE()
				}
				cqes[n] = ExtendedCQE{
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

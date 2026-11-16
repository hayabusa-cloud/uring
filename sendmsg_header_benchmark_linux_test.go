// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"fmt"
	"runtime"
	"testing"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
	"code.hybscloud.com/zcall"
)

// BenchmarkMessageHeaderErrorCompletion measures submission and reap costs with
// preprovisioned storage. EBADF avoids socket work; this is not a throughput test.
func BenchmarkMessageHeaderErrorCompletion(b *testing.B) {
	for _, receive := range []bool{false, true} {
		for _, withOption := range []bool{false, true} {
			b.Run(fmt.Sprintf("receive%t/option%t", receive, withOption), func(b *testing.B) {
				ring := newPublicPerformanceSingleIssuerIORing(b)
				fd := iofd.FD(-1)
				msg := new(uring.Msghdr)
				ctx := uring.PackDirect(0, 0, 1, 0)
				var options []uring.OpOptionFunc
				if withOption {
					options = []uring.OpOptionFunc{uring.WithFlags(0)}
				}
				var cqes [1]uring.CQEView
				var backoff iox.Backoff
				b.ReportAllocs()
				for b.Loop() {
					var err error
					if receive {
						err = ring.RecvMsg(ctx, &fd, msg, options...)
					} else {
						err = ring.SendMsgHeader(ctx, fd, msg, options...)
					}
					if err != nil {
						b.Fatal(err)
					}
					for {
						n, err := ring.Wait(cqes[:])
						if err != nil && !iox.IsWouldBlock(err) {
							b.Fatal(err)
						}
						if n == 0 {
							backoff.Wait()
							continue
						}
						backoff.Reset()
						if cqes[0].Res != -int32(zcall.EBADF) {
							b.Fatalf("invalid descriptor completion: %+v", cqes[0])
						}
						break
					}
				}
				runtime.KeepAlive(msg)
			})
		}
	}
}

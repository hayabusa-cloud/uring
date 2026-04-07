// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"testing"

	"code.hybscloud.com/iofd"
	"code.hybscloud.com/uring"
)

func TestExtCQEFields(t *testing.T) {
	pool := uring.NewContextPools(16)

	ext := pool.Extended()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer pool.PutExtended(ext)

	if err := uring.PrepareListenerSocket(ext, uring.AF_INET, uring.SOCK_STREAM|uring.SOCK_CLOEXEC, uring.IPPROTO_TCP, nil, 128, nil); err != nil {
		t.Fatalf("PrepareListenerSocket: %v", err)
	}
	uring.PrepareListenerBind(ext, iofd.FD(42))

	cqe := uring.ExtCQE{
		Res:   7,
		Flags: uring.IORING_CQE_F_MORE | uring.IORING_CQE_F_BUFFER | (19 << uring.IORING_CQE_BUFFER_SHIFT),
		Ext:   ext,
	}

	if !cqe.IsSuccess() {
		t.Fatal("IsSuccess: got false, want true")
	}
	if !cqe.HasMore() {
		t.Fatal("HasMore: got false, want true")
	}
	if !cqe.HasBuffer() {
		t.Fatal("HasBuffer: got false, want true")
	}
	if got := cqe.BufID(); got != 19 {
		t.Fatalf("BufID: got %d, want 19", got)
	}
	if got := cqe.Op(); got != uring.IORING_OP_BIND {
		t.Fatalf("Op: got %d, want %d", got, uring.IORING_OP_BIND)
	}
	if got := cqe.FD(); got != 42 {
		t.Fatalf("FD: got %d, want 42", got)
	}
}

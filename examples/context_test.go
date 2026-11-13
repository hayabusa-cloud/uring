// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package examples_test

import (
	"testing"
	"time"

	"code.hybscloud.com/iox"
	"code.hybscloud.com/uring"
)

// A submission queue entry (SQE) describes work for the kernel. A completion
// queue entry (CQE) reports an outcome. SQEContext travels in user_data so the
// caller can associate each completion with its request.
//
//	Mode      Stored in user_data    Context storage
//	Direct    Inline scalar fields   No separate target
//	Indirect  Tagged address         64-byte base SQE
//	Extended  Tagged address         Base SQE + 64-byte UserData
//
// Choose Direct when the scalar fields suffice, Indirect to retain the submitted
// SQE, and Extended to attach caller data as well. Direct-mode user_data layout,
// from high to low bits:
//
//	Bits   63..62 | 61..32 | 31..16   | 15..8 | 7..0
//	Field  Mode=0 | FD     | BufGroup | Flags | Op
func TestSQEContextDirect(t *testing.T) {
	// Direct mode stores scalar fields in user_data; it has no borrowed target.
	// PackDirect arguments: opcode, SQE flags, buffer group, file descriptor.
	ctx := uring.PackDirect(uring.IORING_OP_READ, uring.IOSQE_ASYNC, 5, 42)
	if !ctx.IsDirect() || ctx.Op() != uring.IORING_OP_READ ||
		ctx.Flags() != uring.IOSQE_ASYNC || ctx.BufGroup() != 5 || ctx.FD() != 42 {
		t.Fatalf("incorrect Direct context: %#x", ctx.Raw())
	}
}

// SQE flags control submission; CQE flags describe a completion. Common SQE flags:
//
//	Flag                    Meaning
//	IOSQE_FIXED_FILE        FD names a registered-file slot
//	IOSQE_IO_DRAIN          Issue after earlier in-flight work completes
//	IOSQE_IO_LINK           Link this request to the next SQE
//	IOSQE_ASYNC             Offload execution without an initial inline attempt
//	IOSQE_BUFFER_SELECT     Select from a provided-buffer group
//	IOSQE_CQE_SKIP_SUCCESS  Suppress the successful operation CQE
//
// This test checks flag encoding. The I/O examples enable NotifySucceed so
// their completion loops can account for successful operations too.
func TestSQEContextWithFlags(t *testing.T) {
	flags := uint8(uring.IOSQE_ASYNC | uring.IOSQE_IO_LINK)
	ctx := uring.PackDirect(uring.IORING_OP_WRITE, flags, 0, 10)
	// WithFlags replaces the flag set; OrFlags adds bits. Direct mode returns a
	// new value; Indirect and Extended setters modify their shared target.
	replaced := ctx.WithFlags(uring.IOSQE_IO_DRAIN)
	combined := ctx.OrFlags(uring.IOSQE_IO_DRAIN)
	if replaced.Flags() != uring.IOSQE_IO_DRAIN ||
		combined.Flags() != flags|uring.IOSQE_IO_DRAIN || ctx.Flags() != flags {
		t.Fatal("incorrect Direct flag transformation")
	}
}

// Indirect mode retains the submitted SQE in a separate context target:
//
//	SQE.user_data -> IndirectSQE <- CQE.Context()
//
// Submission fills the target's SQE fields. The completion recovers that same
// target; return it to the pool only after completion processing finishes.
func TestSQEContextIndirect(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Drain submitted requests before this deferred teardown.
	defer func() {
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	indirect := ring.IndirectSQE()
	if indirect == nil {
		t.Fatal("context pool exhausted")
	}
	ctx := uring.PackIndirect(indirect)
	if err := ring.Nop(ctx); err != nil {
		ring.PutIndirectSQE(indirect)
		t.Fatalf("Nop: %v", err)
	}
	var cqes [1]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := ring.Wait(cqes[:])
		if err != nil && !iox.IsWouldBlock(err) {
			t.Fatalf("Wait: %v", err)
		}
		if n > 0 {
			backoff.Reset()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for completion")
		}
		backoff.Wait()
	}
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	if !cqe.Context().IsIndirect() || cqe.Context().IndirectSQE() != indirect {
		t.Fatal("completion did not retain the Indirect context")
	}
	// SQEView borrows the context target. Copy required facts before release.
	op := cqe.SQE().Opcode()
	ring.PutIndirectSQE(indirect)
	if op != uring.IORING_OP_NOP {
		t.Fatalf("submitted opcode = %d", op)
	}
}

// Extended mode adds 64 bytes of caller data, useful for request IDs or tags:
//
//	ExtSQE = [retained base SQE: 64 bytes][UserData: 64 bytes]
//
// This example stores a connection tag and retrieves it on completion.
func TestSQEContextExtended(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Drain submitted requests before this deferred teardown.
	defer func() {
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("context pool exhausted")
	}
	// ExtSQE adds caller storage to a retained base SQE. Its UserData is separate
	// from the kernel's optional SQE128 extension.
	copy(ext.UserData[:], "CONN_001")
	ctx := uring.PackExtended(ext)
	if err := ring.Nop(ctx); err != nil {
		ring.PutExtSQE(ext)
		t.Fatalf("Nop: %v", err)
	}
	var cqes [1]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := ring.Wait(cqes[:])
		if err != nil && !iox.IsWouldBlock(err) {
			t.Fatalf("Wait: %v", err)
		}
		if n > 0 {
			backoff.Reset()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for completion")
		}
		backoff.Wait()
	}
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	if !cqe.Extended() || cqe.ExtSQE() != ext {
		t.Fatal("completion did not retain the Extended context")
	}
	// UserData is raw storage. Use pointer-free data or keep Go roots elsewhere.
	// The copy survives pool reuse; cqe.ExtSQE() remains a borrowed pointer.
	tag := cqe.ExtSQE().UserData
	ring.PutExtSQE(ext)
	if string(tag[:8]) != "CONN_001" {
		t.Fatalf("tag = %q", tag[:8])
	}
}

// CQEView.Res contains the operation result or negative errno; Flags contains
// IORING_CQE_F_* completion flags. Context recovers the submission context;
// Indirect and Extended views borrow their retained targets.
//
//	Completion flag       Meaning (IORING_CQE_F_ prefix omitted)
//	BUFFER                Upper flag bits identify a selected buffer
//	MORE                  The request has further completion events
//	NOTIF                 Buffer-release notification for a zero-copy send
//	BUF_MORE              The kernel retains an incremental buffer
//
// MORE concerns the request; BUF_MORE concerns the buffer. Neither is a byte
// count. Interpret Res by opcode: for example, NOP returns 0 and READ returns
// bytes read, while a negative result encodes an errno.
func TestCQEViewAccess(t *testing.T) {
	ring, err := uring.New(func(opt *uring.Options) {
		opt.Entries = uring.EntriesNano
		opt.LockedBufferMem = uring.BufferSizeLarge
		opt.ReadBufferNum = 64
		opt.NotifySucceed = true
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ring.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Drain submitted requests before this deferred teardown.
	defer func() {
		if err := ring.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	ctx := uring.PackDirect(uring.IORING_OP_NOP, 0, 99, 77)
	if err := ring.Nop(ctx); err != nil {
		t.Fatalf("Nop: %v", err)
	}
	var cqes [1]uring.CQEView
	var backoff iox.Backoff
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := ring.Wait(cqes[:])
		if err != nil && !iox.IsWouldBlock(err) {
			t.Fatalf("Wait: %v", err)
		}
		if n > 0 {
			backoff.Reset()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for completion")
		}
		backoff.Wait()
	}
	cqe := cqes[0]
	if cqe.Op() != uring.IORING_OP_NOP || cqe.Res != 0 {
		t.Fatalf("unexpected completion: opcode=%d result=%d error=%v", cqe.Op(), cqe.Res, cqe.Err())
	}
	if cqe.BufGroup() != 99 || cqe.FD() != 77 {
		t.Fatal("completion lost caller correlation fields")
	}
}

// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build linux

package uring_test

import (
	"testing"

	"code.hybscloud.com/uring"
)

func TestViewCtxBasic(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer ring.PutExtSQE(ext)

	// Test ViewCtx (0 refs)
	ctx0 := uring.ViewCtx(ext)
	c := ctx0.Vals0()
	if c == nil {
		t.Error("ViewCtx().Vals0() returned nil")
	}

	// Test Vals1-Vals7 methods
	v1 := ctx0.Vals1()
	if v1 == nil {
		t.Error("ViewCtx().Vals1() returned nil")
	}

	v2 := ctx0.Vals2()
	if v2 == nil {
		t.Error("ViewCtx().Vals2() returned nil")
	}

	v3 := ctx0.Vals3()
	if v3 == nil {
		t.Error("ViewCtx().Vals3() returned nil")
	}
}

type testRef struct {
	value int
}

func TestViewCtx1(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer ring.PutExtSQE(ext)

	// Test ViewCtx1[T] (1 ref) - Ref1 is *T, so use testRef directly
	ctx1 := uring.ViewCtx1[testRef](ext)
	c := ctx1.Vals0()
	if c == nil {
		t.Error("ViewCtx1().Vals0() returned nil")
	}

	// Set ref to point to our test value
	ref := &testRef{value: 42}
	c.Ref1 = ref

	// Retrieve and verify
	c2 := ctx1.Vals0()
	if c2.Ref1 != ref {
		t.Error("Ref1 was not preserved")
	}
	if c2.Ref1.value != 42 {
		t.Errorf("Ref1.value: got %d, want 42", c2.Ref1.value)
	}
}

func TestViewCtx2(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer ring.PutExtSQE(ext)

	// Test ViewCtx2[T1, T2] (2 refs) - Ref1/Ref2 are *T1/*T2
	ctx2 := uring.ViewCtx2[testRef, testRef](ext)
	c := ctx2.Vals0()
	if c == nil {
		t.Error("ViewCtx2().Vals0() returned nil")
	}

	// Set refs
	ref1 := &testRef{value: 100}
	ref2 := &testRef{value: 200}
	c.Ref1 = ref1
	c.Ref2 = ref2

	// Verify
	c2 := ctx2.Vals0()
	if c2.Ref1.value != 100 || c2.Ref2.value != 200 {
		t.Errorf("Refs not preserved: got %d, %d", c2.Ref1.value, c2.Ref2.value)
	}
}

func TestViewCtx3Through7(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	// Test ViewCtx3-7 creation (just verify they don't panic)
	t.Run("ViewCtx3", func(t *testing.T) {
		ext := ring.ExtSQE()
		if ext == nil {
			t.Skip("pool exhausted")
		}
		defer ring.PutExtSQE(ext)

		ctx := uring.ViewCtx3[int, int, int](ext)
		if ctx.Vals0() == nil {
			t.Error("ViewCtx3().Vals0() returned nil")
		}
	})

	t.Run("ViewCtx4", func(t *testing.T) {
		ext := ring.ExtSQE()
		if ext == nil {
			t.Skip("pool exhausted")
		}
		defer ring.PutExtSQE(ext)

		ctx := uring.ViewCtx4[int, int, int, int](ext)
		if ctx.Vals0() == nil {
			t.Error("ViewCtx4().Vals0() returned nil")
		}
	})

	t.Run("ViewCtx5", func(t *testing.T) {
		ext := ring.ExtSQE()
		if ext == nil {
			t.Skip("pool exhausted")
		}
		defer ring.PutExtSQE(ext)

		ctx := uring.ViewCtx5[int, int, int, int, int](ext)
		if ctx.Vals0() == nil {
			t.Error("ViewCtx5().Vals0() returned nil")
		}
	})

	t.Run("ViewCtx6", func(t *testing.T) {
		ext := ring.ExtSQE()
		if ext == nil {
			t.Skip("pool exhausted")
		}
		defer ring.PutExtSQE(ext)

		ctx := uring.ViewCtx6[int, int, int, int, int, int](ext)
		if ctx.Vals0() == nil {
			t.Error("ViewCtx6().Vals0() returned nil")
		}
	})

	t.Run("ViewCtx7", func(t *testing.T) {
		ext := ring.ExtSQE()
		if ext == nil {
			t.Skip("pool exhausted")
		}
		defer ring.PutExtSQE(ext)

		ctx := uring.ViewCtx7[int, int, int, int, int, int, int](ext)
		if ctx.Vals0() == nil {
			t.Error("ViewCtx7().Vals0() returned nil")
		}
	})
}

func TestCtxRefs0ValMethods(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer ring.PutExtSQE(ext)

	ctx0 := uring.ViewCtx(ext)

	// Test all V methods
	if ctx0.Vals1() == nil {
		t.Error("V1() returned nil")
	}
	if ctx0.Vals2() == nil {
		t.Error("V2() returned nil")
	}
	if ctx0.Vals3() == nil {
		t.Error("V3() returned nil")
	}
	if ctx0.Vals4() == nil {
		t.Error("V4() returned nil")
	}
	if ctx0.Vals5() == nil {
		t.Error("V5() returned nil")
	}
	if ctx0.Vals6() == nil {
		t.Error("V6() returned nil")
	}
	if ctx0.Vals7() == nil {
		t.Error("V7() returned nil")
	}
}

func TestCtxRefs1ValMethods(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer ring.PutExtSQE(ext)

	ctx1 := uring.ViewCtx1[int](ext)

	// Test all V methods for CtxRefs1
	if ctx1.Vals1() == nil {
		t.Error("V1() returned nil")
	}
	if ctx1.Vals2() == nil {
		t.Error("V2() returned nil")
	}
	if ctx1.Vals3() == nil {
		t.Error("V3() returned nil")
	}
	if ctx1.Vals4() == nil {
		t.Error("V4() returned nil")
	}
	if ctx1.Vals5() == nil {
		t.Error("V5() returned nil")
	}
	if ctx1.Vals6() == nil {
		t.Error("V6() returned nil")
	}
}

func TestCtxRefs2ValMethods(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer ring.PutExtSQE(ext)

	ctx2 := uring.ViewCtx2[int, int](ext)

	// Test all V methods for CtxRefs2
	if ctx2.Vals1() == nil {
		t.Error("V1() returned nil")
	}
	if ctx2.Vals2() == nil {
		t.Error("V2() returned nil")
	}
	if ctx2.Vals3() == nil {
		t.Error("V3() returned nil")
	}
	if ctx2.Vals4() == nil {
		t.Error("V4() returned nil")
	}
	if ctx2.Vals5() == nil {
		t.Error("V5() returned nil")
	}
}

func TestCtxRefs3ValMethods(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer ring.PutExtSQE(ext)

	ctx3 := uring.ViewCtx3[int, int, int](ext)

	// Test all V methods for CtxRefs3
	if ctx3.Vals1() == nil {
		t.Error("V1() returned nil")
	}
	if ctx3.Vals2() == nil {
		t.Error("V2() returned nil")
	}
	if ctx3.Vals3() == nil {
		t.Error("V3() returned nil")
	}
	if ctx3.Vals4() == nil {
		t.Error("V4() returned nil")
	}
}

func TestCtxRefs4ValMethods(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer ring.PutExtSQE(ext)

	ctx4 := uring.ViewCtx4[int, int, int, int](ext)

	// Test all V methods for CtxRefs4
	if ctx4.Vals1() == nil {
		t.Error("V1() returned nil")
	}
	if ctx4.Vals2() == nil {
		t.Error("V2() returned nil")
	}
	if ctx4.Vals3() == nil {
		t.Error("V3() returned nil")
	}
}

func TestCtxRefs5ValMethods(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer ring.PutExtSQE(ext)

	ctx5 := uring.ViewCtx5[int, int, int, int, int](ext)

	// Test all V methods for CtxRefs5
	if ctx5.Vals1() == nil {
		t.Error("V1() returned nil")
	}
	if ctx5.Vals2() == nil {
		t.Error("V2() returned nil")
	}
}

func TestCtxRefs6ValMethods(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer ring.PutExtSQE(ext)

	ctx6 := uring.ViewCtx6[int, int, int, int, int, int](ext)

	// Test V1 method for CtxRefs6
	if ctx6.Vals1() == nil {
		t.Error("V1() returned nil")
	}
}

func TestCtxShorthandHelpers(t *testing.T) {
	ring, err := uring.New(testMinimalBufferOptions, func(opt *uring.Options) {
		opt.Entries = uring.EntriesSmall
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustStartRing(t, ring)

	ext := ring.ExtSQE()
	if ext == nil {
		t.Fatal("pool exhausted")
	}
	defer ring.PutExtSQE(ext)

	if uring.CtxOf(ext) == nil {
		t.Error("CtxOf() returned nil")
	}
	if uring.CtxV1Of(ext) == nil {
		t.Error("CtxV1Of() returned nil")
	}
	if uring.CtxV2Of(ext) == nil {
		t.Error("CtxV2Of() returned nil")
	}
	if uring.CtxV3Of(ext) == nil {
		t.Error("CtxV3Of() returned nil")
	}
	if uring.CtxV4Of(ext) == nil {
		t.Error("CtxV4Of() returned nil")
	}
	if uring.CtxV5Of(ext) == nil {
		t.Error("CtxV5Of() returned nil")
	}
	if uring.CtxV6Of(ext) == nil {
		t.Error("CtxV6Of() returned nil")
	}
	if uring.CtxV7Of(ext) == nil {
		t.Error("CtxV7Of() returned nil")
	}

	if uring.Ctx0Of(ext) == nil {
		t.Error("Ctx0Of() returned nil")
	}
	if uring.Ctx0V1Of(ext) == nil {
		t.Error("Ctx0V1Of() returned nil")
	}
	if uring.Ctx0V2Of(ext) == nil {
		t.Error("Ctx0V2Of() returned nil")
	}
	if uring.Ctx0V3Of(ext) == nil {
		t.Error("Ctx0V3Of() returned nil")
	}
	if uring.Ctx0V4Of(ext) == nil {
		t.Error("Ctx0V4Of() returned nil")
	}
	if uring.Ctx0V5Of(ext) == nil {
		t.Error("Ctx0V5Of() returned nil")
	}
	if uring.Ctx0V6Of(ext) == nil {
		t.Error("Ctx0V6Of() returned nil")
	}
	if uring.Ctx0V7Of(ext) == nil {
		t.Error("Ctx0V7Of() returned nil")
	}

	if uring.Ctx1Of[int](ext) == nil {
		t.Error("Ctx1Of() returned nil")
	}
	if uring.Ctx1V1Of[int](ext) == nil {
		t.Error("Ctx1V1Of() returned nil")
	}
	if uring.Ctx1V2Of[int](ext) == nil {
		t.Error("Ctx1V2Of() returned nil")
	}
	if uring.Ctx1V3Of[int](ext) == nil {
		t.Error("Ctx1V3Of() returned nil")
	}
	if uring.Ctx1V4Of[int](ext) == nil {
		t.Error("Ctx1V4Of() returned nil")
	}
	if uring.Ctx1V5Of[int](ext) == nil {
		t.Error("Ctx1V5Of() returned nil")
	}
	if uring.Ctx1V6Of[int](ext) == nil {
		t.Error("Ctx1V6Of() returned nil")
	}

	if uring.Ctx2Of[int, int](ext) == nil {
		t.Error("Ctx2Of() returned nil")
	}
	if uring.Ctx2V1Of[int, int](ext) == nil {
		t.Error("Ctx2V1Of() returned nil")
	}
	if uring.Ctx2V2Of[int, int](ext) == nil {
		t.Error("Ctx2V2Of() returned nil")
	}
	if uring.Ctx2V3Of[int, int](ext) == nil {
		t.Error("Ctx2V3Of() returned nil")
	}
	if uring.Ctx2V4Of[int, int](ext) == nil {
		t.Error("Ctx2V4Of() returned nil")
	}
	if uring.Ctx2V5Of[int, int](ext) == nil {
		t.Error("Ctx2V5Of() returned nil")
	}

	if uring.Ctx3Of[int, int, int](ext) == nil {
		t.Error("Ctx3Of() returned nil")
	}
	if uring.Ctx3V1Of[int, int, int](ext) == nil {
		t.Error("Ctx3V1Of() returned nil")
	}
	if uring.Ctx3V2Of[int, int, int](ext) == nil {
		t.Error("Ctx3V2Of() returned nil")
	}
	if uring.Ctx3V3Of[int, int, int](ext) == nil {
		t.Error("Ctx3V3Of() returned nil")
	}
	if uring.Ctx3V4Of[int, int, int](ext) == nil {
		t.Error("Ctx3V4Of() returned nil")
	}

	if uring.Ctx4Of[int, int, int, int](ext) == nil {
		t.Error("Ctx4Of() returned nil")
	}
	if uring.Ctx4V1Of[int, int, int, int](ext) == nil {
		t.Error("Ctx4V1Of() returned nil")
	}
	if uring.Ctx4V2Of[int, int, int, int](ext) == nil {
		t.Error("Ctx4V2Of() returned nil")
	}
	if uring.Ctx4V3Of[int, int, int, int](ext) == nil {
		t.Error("Ctx4V3Of() returned nil")
	}

	if uring.Ctx5Of[int, int, int, int, int](ext) == nil {
		t.Error("Ctx5Of() returned nil")
	}
	if uring.Ctx5V1Of[int, int, int, int, int](ext) == nil {
		t.Error("Ctx5V1Of() returned nil")
	}
	if uring.Ctx5V2Of[int, int, int, int, int](ext) == nil {
		t.Error("Ctx5V2Of() returned nil")
	}

	if uring.Ctx6Of[int, int, int, int, int, int](ext) == nil {
		t.Error("Ctx6Of() returned nil")
	}
	if uring.Ctx6V1Of[int, int, int, int, int, int](ext) == nil {
		t.Error("Ctx6V1Of() returned nil")
	}

	if uring.Ctx7Of[int, int, int, int, int, int, int](ext) == nil {
		t.Error("Ctx7Of() returned nil")
	}
}

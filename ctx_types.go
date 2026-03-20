// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Refactored from code.hybscloud.com/sox.

package uring

import "unsafe"

// Handler is the callback function signature for completion handling.
// It receives the Uring instance, the original SQE, and the completion result.
//
// The handler pointer is stored in the first 8 bytes of the UserData area
// within ExtSQE.
//
// Example:
//
//	func handleRecv(ring *Uring, sqe *ioUringSqe, cqe *ioUringCqe) {
//	    if cqe.res < 0 {
//	        // Handle error
//	        return
//	    }
//	    // Process received data
//	}
type Handler = func(ring *Uring, sqe *ioUringSqe, cqe *ioUringCqe)

// Compile-time size assertion for Handler pointer.
var _ [8 - unsafe.Sizeof(Handler(nil))]struct{}

// Naming rule:
//   - CtxN means N typed refs and 0 vals.
//   - CtxNVm means N typed refs and m int64 vals.
//
// ========================================
// 0 Refs: Ctx0, Ctx0V1-Ctx0V7
// ========================================

// Ctx0 has 0 refs, 0 vals, and 56 bytes of data.
type Ctx0 struct {
	Fn   Handler  // 8 bytes
	Data [56]byte // 56 bytes
}

var _ [64 - unsafe.Sizeof(Ctx0{})]struct{}

// Ctx0V1 has 0 refs, 1 val, and 48 bytes of data.
type Ctx0V1 struct {
	Fn   Handler  // 8 bytes
	Val1 int64    // 8 bytes
	Data [48]byte // 48 bytes
}

var _ [64 - unsafe.Sizeof(Ctx0V1{})]struct{}

// Ctx0V2 has 0 refs, 2 vals, and 40 bytes of data.
type Ctx0V2 struct {
	Fn   Handler  // 8 bytes
	Val1 int64    // 8 bytes
	Val2 int64    // 8 bytes
	Data [40]byte // 40 bytes
}

var _ [64 - unsafe.Sizeof(Ctx0V2{})]struct{}

// Ctx0V3 has 0 refs, 3 vals, and 32 bytes of data.
type Ctx0V3 struct {
	Fn   Handler  // 8 bytes
	Val1 int64    // 8 bytes
	Val2 int64    // 8 bytes
	Val3 int64    // 8 bytes
	Data [32]byte // 32 bytes
}

var _ [64 - unsafe.Sizeof(Ctx0V3{})]struct{}

// Ctx0V4 has 0 refs, 4 vals, and 24 bytes of data.
type Ctx0V4 struct {
	Fn   Handler  // 8 bytes
	Val1 int64    // 8 bytes
	Val2 int64    // 8 bytes
	Val3 int64    // 8 bytes
	Val4 int64    // 8 bytes
	Data [24]byte // 24 bytes
}

var _ [64 - unsafe.Sizeof(Ctx0V4{})]struct{}

// Ctx0V5 has 0 refs, 5 vals, and 16 bytes of data.
type Ctx0V5 struct {
	Fn   Handler  // 8 bytes
	Val1 int64    // 8 bytes
	Val2 int64    // 8 bytes
	Val3 int64    // 8 bytes
	Val4 int64    // 8 bytes
	Val5 int64    // 8 bytes
	Data [16]byte // 16 bytes
}

var _ [64 - unsafe.Sizeof(Ctx0V5{})]struct{}

// Ctx0V6 has 0 refs, 6 vals, and 8 bytes of data.
type Ctx0V6 struct {
	Fn   Handler // 8 bytes
	Val1 int64   // 8 bytes
	Val2 int64   // 8 bytes
	Val3 int64   // 8 bytes
	Val4 int64   // 8 bytes
	Val5 int64   // 8 bytes
	Val6 int64   // 8 bytes
	Data [8]byte // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx0V6{})]struct{}

// Ctx0V7 has 0 refs, 7 vals, and 0 bytes of data.
type Ctx0V7 struct {
	Fn   Handler // 8 bytes
	Val1 int64   // 8 bytes
	Val2 int64   // 8 bytes
	Val3 int64   // 8 bytes
	Val4 int64   // 8 bytes
	Val5 int64   // 8 bytes
	Val6 int64   // 8 bytes
	Val7 int64   // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx0V7{})]struct{}

// ========================================
// 1 Ref: Ctx1[T1], Ctx1V1-V6[T1]
// ========================================

// Ctx1 has 1 ref, 0 vals, 48 bytes data.
type Ctx1[T1 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Data [48]byte // 48 bytes
}

var _ [64 - unsafe.Sizeof(Ctx1[int]{})]struct{}

// Ctx1V1 has 1 ref, 1 val, 40 bytes data.
type Ctx1V1[T1 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Val1 int64    // 8 bytes
	Data [40]byte // 40 bytes
}

var _ [64 - unsafe.Sizeof(Ctx1V1[int]{})]struct{}

// Ctx1V2 has 1 ref, 2 vals, 32 bytes data.
type Ctx1V2[T1 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Val1 int64    // 8 bytes
	Val2 int64    // 8 bytes
	Data [32]byte // 32 bytes
}

var _ [64 - unsafe.Sizeof(Ctx1V2[int]{})]struct{}

// Ctx1V3 has 1 ref, 3 vals, 24 bytes data.
type Ctx1V3[T1 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Val1 int64    // 8 bytes
	Val2 int64    // 8 bytes
	Val3 int64    // 8 bytes
	Data [24]byte // 24 bytes
}

var _ [64 - unsafe.Sizeof(Ctx1V3[int]{})]struct{}

// Ctx1V4 has 1 ref, 4 vals, 16 bytes data.
type Ctx1V4[T1 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Val1 int64    // 8 bytes
	Val2 int64    // 8 bytes
	Val3 int64    // 8 bytes
	Val4 int64    // 8 bytes
	Data [16]byte // 16 bytes
}

var _ [64 - unsafe.Sizeof(Ctx1V4[int]{})]struct{}

// Ctx1V5 has 1 ref, 5 vals, 8 bytes data.
type Ctx1V5[T1 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Val1 int64   // 8 bytes
	Val2 int64   // 8 bytes
	Val3 int64   // 8 bytes
	Val4 int64   // 8 bytes
	Val5 int64   // 8 bytes
	Data [8]byte // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx1V5[int]{})]struct{}

// Ctx1V6 has 1 ref, 6 vals, 0 bytes data.
type Ctx1V6[T1 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Val1 int64   // 8 bytes
	Val2 int64   // 8 bytes
	Val3 int64   // 8 bytes
	Val4 int64   // 8 bytes
	Val5 int64   // 8 bytes
	Val6 int64   // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx1V6[int]{})]struct{}

// ========================================
// 2 Refs: Ctx2[T1,T2], Ctx2V1-V5[T1,T2]
// ========================================

// Ctx2 has 2 refs, 0 vals, 40 bytes data.
type Ctx2[T1, T2 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Ref2 *T2      // 8 bytes
	Data [40]byte // 40 bytes
}

var _ [64 - unsafe.Sizeof(Ctx2[int, int]{})]struct{}

// Ctx2V1 has 2 refs, 1 val, 32 bytes data.
type Ctx2V1[T1, T2 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Ref2 *T2      // 8 bytes
	Val1 int64    // 8 bytes
	Data [32]byte // 32 bytes
}

var _ [64 - unsafe.Sizeof(Ctx2V1[int, int]{})]struct{}

// Ctx2V2 has 2 refs, 2 vals, 24 bytes data.
type Ctx2V2[T1, T2 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Ref2 *T2      // 8 bytes
	Val1 int64    // 8 bytes
	Val2 int64    // 8 bytes
	Data [24]byte // 24 bytes
}

var _ [64 - unsafe.Sizeof(Ctx2V2[int, int]{})]struct{}

// Ctx2V3 has 2 refs, 3 vals, 16 bytes data.
type Ctx2V3[T1, T2 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Ref2 *T2      // 8 bytes
	Val1 int64    // 8 bytes
	Val2 int64    // 8 bytes
	Val3 int64    // 8 bytes
	Data [16]byte // 16 bytes
}

var _ [64 - unsafe.Sizeof(Ctx2V3[int, int]{})]struct{}

// Ctx2V4 has 2 refs, 4 vals, 8 bytes data.
type Ctx2V4[T1, T2 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Ref2 *T2     // 8 bytes
	Val1 int64   // 8 bytes
	Val2 int64   // 8 bytes
	Val3 int64   // 8 bytes
	Val4 int64   // 8 bytes
	Data [8]byte // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx2V4[int, int]{})]struct{}

// Ctx2V5 has 2 refs, 5 vals, 0 bytes data.
type Ctx2V5[T1, T2 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Ref2 *T2     // 8 bytes
	Val1 int64   // 8 bytes
	Val2 int64   // 8 bytes
	Val3 int64   // 8 bytes
	Val4 int64   // 8 bytes
	Val5 int64   // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx2V5[int, int]{})]struct{}

// ========================================
// 3 Refs: Ctx3[T1,T2,T3], Ctx3V1-V4[T1,T2,T3]
// ========================================

// Ctx3 has 3 refs, 0 vals, 32 bytes data.
type Ctx3[T1, T2, T3 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Ref2 *T2      // 8 bytes
	Ref3 *T3      // 8 bytes
	Data [32]byte // 32 bytes
}

var _ [64 - unsafe.Sizeof(Ctx3[int, int, int]{})]struct{}

// Ctx3V1 has 3 refs, 1 val, 24 bytes data.
type Ctx3V1[T1, T2, T3 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Ref2 *T2      // 8 bytes
	Ref3 *T3      // 8 bytes
	Val1 int64    // 8 bytes
	Data [24]byte // 24 bytes
}

var _ [64 - unsafe.Sizeof(Ctx3V1[int, int, int]{})]struct{}

// Ctx3V2 has 3 refs, 2 vals, 16 bytes data.
type Ctx3V2[T1, T2, T3 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Ref2 *T2      // 8 bytes
	Ref3 *T3      // 8 bytes
	Val1 int64    // 8 bytes
	Val2 int64    // 8 bytes
	Data [16]byte // 16 bytes
}

var _ [64 - unsafe.Sizeof(Ctx3V2[int, int, int]{})]struct{}

// Ctx3V3 has 3 refs, 3 vals, 8 bytes data.
type Ctx3V3[T1, T2, T3 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Ref2 *T2     // 8 bytes
	Ref3 *T3     // 8 bytes
	Val1 int64   // 8 bytes
	Val2 int64   // 8 bytes
	Val3 int64   // 8 bytes
	Data [8]byte // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx3V3[int, int, int]{})]struct{}

// Ctx3V4 has 3 refs, 4 vals, 0 bytes data.
type Ctx3V4[T1, T2, T3 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Ref2 *T2     // 8 bytes
	Ref3 *T3     // 8 bytes
	Val1 int64   // 8 bytes
	Val2 int64   // 8 bytes
	Val3 int64   // 8 bytes
	Val4 int64   // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx3V4[int, int, int]{})]struct{}

// ========================================
// 4 Refs: Ctx4[T1,T2,T3,T4], Ctx4V1-V3[T1,T2,T3,T4]
// ========================================

// Ctx4 has 4 refs, 0 vals, 24 bytes data.
type Ctx4[T1, T2, T3, T4 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Ref2 *T2      // 8 bytes
	Ref3 *T3      // 8 bytes
	Ref4 *T4      // 8 bytes
	Data [24]byte // 24 bytes
}

var _ [64 - unsafe.Sizeof(Ctx4[int, int, int, int]{})]struct{}

// Ctx4V1 has 4 refs, 1 val, 16 bytes data.
type Ctx4V1[T1, T2, T3, T4 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Ref2 *T2      // 8 bytes
	Ref3 *T3      // 8 bytes
	Ref4 *T4      // 8 bytes
	Val1 int64    // 8 bytes
	Data [16]byte // 16 bytes
}

var _ [64 - unsafe.Sizeof(Ctx4V1[int, int, int, int]{})]struct{}

// Ctx4V2 has 4 refs, 2 vals, 8 bytes data.
type Ctx4V2[T1, T2, T3, T4 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Ref2 *T2     // 8 bytes
	Ref3 *T3     // 8 bytes
	Ref4 *T4     // 8 bytes
	Val1 int64   // 8 bytes
	Val2 int64   // 8 bytes
	Data [8]byte // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx4V2[int, int, int, int]{})]struct{}

// Ctx4V3 has 4 refs, 3 vals, 0 bytes data.
type Ctx4V3[T1, T2, T3, T4 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Ref2 *T2     // 8 bytes
	Ref3 *T3     // 8 bytes
	Ref4 *T4     // 8 bytes
	Val1 int64   // 8 bytes
	Val2 int64   // 8 bytes
	Val3 int64   // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx4V3[int, int, int, int]{})]struct{}

// ========================================
// 5 Refs: Ctx5[T1,T2,T3,T4,T5], Ctx5V1-V2[T1,T2,T3,T4,T5]
// ========================================

// Ctx5 has 5 refs, 0 vals, 16 bytes data.
type Ctx5[T1, T2, T3, T4, T5 any] struct {
	Fn   Handler  // 8 bytes
	Ref1 *T1      // 8 bytes
	Ref2 *T2      // 8 bytes
	Ref3 *T3      // 8 bytes
	Ref4 *T4      // 8 bytes
	Ref5 *T5      // 8 bytes
	Data [16]byte // 16 bytes
}

var _ [64 - unsafe.Sizeof(Ctx5[int, int, int, int, int]{})]struct{}

// Ctx5V1 has 5 refs, 1 val, 8 bytes data.
type Ctx5V1[T1, T2, T3, T4, T5 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Ref2 *T2     // 8 bytes
	Ref3 *T3     // 8 bytes
	Ref4 *T4     // 8 bytes
	Ref5 *T5     // 8 bytes
	Val1 int64   // 8 bytes
	Data [8]byte // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx5V1[int, int, int, int, int]{})]struct{}

// Ctx5V2 has 5 refs, 2 vals, 0 bytes data.
type Ctx5V2[T1, T2, T3, T4, T5 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Ref2 *T2     // 8 bytes
	Ref3 *T3     // 8 bytes
	Ref4 *T4     // 8 bytes
	Ref5 *T5     // 8 bytes
	Val1 int64   // 8 bytes
	Val2 int64   // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx5V2[int, int, int, int, int]{})]struct{}

// ========================================
// 6 Refs: Ctx6[T1,T2,T3,T4,T5,T6], Ctx6V1[T1,T2,T3,T4,T5,T6]
// ========================================

// Ctx6 has 6 refs, 0 vals, 8 bytes data.
type Ctx6[T1, T2, T3, T4, T5, T6 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Ref2 *T2     // 8 bytes
	Ref3 *T3     // 8 bytes
	Ref4 *T4     // 8 bytes
	Ref5 *T5     // 8 bytes
	Ref6 *T6     // 8 bytes
	Data [8]byte // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx6[int, int, int, int, int, int]{})]struct{}

// Ctx6V1 has 6 refs, 1 val, 0 bytes data.
type Ctx6V1[T1, T2, T3, T4, T5, T6 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Ref2 *T2     // 8 bytes
	Ref3 *T3     // 8 bytes
	Ref4 *T4     // 8 bytes
	Ref5 *T5     // 8 bytes
	Ref6 *T6     // 8 bytes
	Val1 int64   // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx6V1[int, int, int, int, int, int]{})]struct{}

// ========================================
// 7 Refs: Ctx7[T1,T2,T3,T4,T5,T6,T7]
// ========================================

// Ctx7 has 7 refs, 0 vals, 0 bytes data.
type Ctx7[T1, T2, T3, T4, T5, T6, T7 any] struct {
	Fn   Handler // 8 bytes
	Ref1 *T1     // 8 bytes
	Ref2 *T2     // 8 bytes
	Ref3 *T3     // 8 bytes
	Ref4 *T4     // 8 bytes
	Ref5 *T5     // 8 bytes
	Ref6 *T6     // 8 bytes
	Ref7 *T7     // 8 bytes
}

var _ [64 - unsafe.Sizeof(Ctx7[int, int, int, int, int, int, int]{})]struct{}

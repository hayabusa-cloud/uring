// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Refactored from code.hybscloud.com/sox.

package uring

import "unsafe"

// ========================================
// Phase 1: Select Ref Count (standalone functions)
// ========================================

// Note: Go does not support type parameters on methods, so we use
// standalone functions for Phase 1 (ref count selection).

// Each returned CtxRefs* view aliases sqe.UserData directly. The typed layout
// pointers returned by Vals* remain valid only while the owning ExtSQE remains
// borrowed and must not outlive PutExtended/PutExtSQE. Raw `UserData` bytes do
// not root overlaid Go references for the GC, so caller code must keep them
// live elsewhere when using pointer-bearing views.

// ViewCtx creates a CtxRefs0 for accessing the UserData with 0 refs.
//
// Example:
//
//	c := uring.ViewCtx(sqe).Vals3()  // 0 refs, 3 vals
//	c.Val1 = timestamp
//	c.Val2 = flags
//	c.Val3 = seqNum
func ViewCtx(sqe *ExtSQE) CtxRefs0 {
	return CtxRefs0{sqe: sqe}
}

// ViewCtx1 creates a CtxRefs1 for accessing the UserData with 1 typed ref.
//
// Example:
//
//	c := uring.ViewCtx1[Connection](sqe).Vals1()  // 1 ref, 1 val
//	c.Val1 = time.Now().UnixNano()
func ViewCtx1[T1 any](sqe *ExtSQE) CtxRefs1[T1] {
	return CtxRefs1[T1]{sqe: sqe}
}

// ViewCtx2 creates a CtxRefs2 for accessing the UserData with 2 typed refs.
//
// Example:
//
//	c := uring.ViewCtx2[Connection, Buffer](sqe).Vals2()  // 2 refs, 2 vals
//	c.Val1 = offset
//	c.Val2 = length
func ViewCtx2[T1, T2 any](sqe *ExtSQE) CtxRefs2[T1, T2] {
	return CtxRefs2[T1, T2]{sqe: sqe}
}

// ViewCtx3 creates a CtxRefs3 for accessing the UserData with 3 typed refs.
func ViewCtx3[T1, T2, T3 any](sqe *ExtSQE) CtxRefs3[T1, T2, T3] {
	return CtxRefs3[T1, T2, T3]{sqe: sqe}
}

// ViewCtx4 creates a CtxRefs4 for accessing the UserData with 4 typed refs.
func ViewCtx4[T1, T2, T3, T4 any](sqe *ExtSQE) CtxRefs4[T1, T2, T3, T4] {
	return CtxRefs4[T1, T2, T3, T4]{sqe: sqe}
}

// ViewCtx5 creates a CtxRefs5 for accessing the UserData with 5 typed refs.
func ViewCtx5[T1, T2, T3, T4, T5 any](sqe *ExtSQE) CtxRefs5[T1, T2, T3, T4, T5] {
	return CtxRefs5[T1, T2, T3, T4, T5]{sqe: sqe}
}

// ViewCtx6 creates a CtxRefs6 for accessing the UserData with 6 typed refs.
func ViewCtx6[T1, T2, T3, T4, T5, T6 any](sqe *ExtSQE) CtxRefs6[T1, T2, T3, T4, T5, T6] {
	return CtxRefs6[T1, T2, T3, T4, T5, T6]{sqe: sqe}
}

// ViewCtx7 creates a CtxRefs7 for accessing the UserData with 7 typed refs.
func ViewCtx7[T1, T2, T3, T4, T5, T6, T7 any](sqe *ExtSQE) CtxRefs7[T1, T2, T3, T4, T5, T6, T7] {
	return CtxRefs7[T1, T2, T3, T4, T5, T6, T7]{sqe: sqe}
}

// ========================================
// Phase 2 Views: Select Val Count
// ========================================

// CtxRefs0 is a view into ExtSQE with 0 refs.
// Use its methods to select the number of vals (0-7).
type CtxRefs0 struct {
	sqe *ExtSQE
}

// Vals0 returns a Ctx0 pointer (0 refs, 0 vals, 56B data).
//
//go:nosplit
func (v CtxRefs0) Vals0() *Ctx0 {
	return (*Ctx0)(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals1 returns a Ctx0V1 pointer (0 refs, 1 val, 48B data).
//
//go:nosplit
func (v CtxRefs0) Vals1() *Ctx0V1 {
	return (*Ctx0V1)(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals2 returns a Ctx0V2 pointer (0 refs, 2 vals, 40B data).
//
//go:nosplit
func (v CtxRefs0) Vals2() *Ctx0V2 {
	return (*Ctx0V2)(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals3 returns a Ctx0V3 pointer (0 refs, 3 vals, 32B data).
//
//go:nosplit
func (v CtxRefs0) Vals3() *Ctx0V3 {
	return (*Ctx0V3)(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals4 returns a Ctx0V4 pointer (0 refs, 4 vals, 24B data).
//
//go:nosplit
func (v CtxRefs0) Vals4() *Ctx0V4 {
	return (*Ctx0V4)(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals5 returns a Ctx0V5 pointer (0 refs, 5 vals, 16B data).
//
//go:nosplit
func (v CtxRefs0) Vals5() *Ctx0V5 {
	return (*Ctx0V5)(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals6 returns a Ctx0V6 pointer (0 refs, 6 vals, 8B data).
//
//go:nosplit
func (v CtxRefs0) Vals6() *Ctx0V6 {
	return (*Ctx0V6)(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals7 returns a Ctx0V7 pointer (0 refs, 7 vals, 0B data).
//
//go:nosplit
func (v CtxRefs0) Vals7() *Ctx0V7 {
	return (*Ctx0V7)(unsafe.Pointer(&v.sqe.UserData[0]))
}

// CtxRefs1 is a view into ExtSQE with 1 ref.
// Use its methods to select the number of vals (0-6).
type CtxRefs1[T1 any] struct {
	sqe *ExtSQE
}

// Vals0 returns a Ctx1 pointer (1 ref, 0 vals, 48B data).
//
//go:nosplit
func (v CtxRefs1[T1]) Vals0() *Ctx1[T1] {
	return (*Ctx1[T1])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals1 returns a Ctx1V1 pointer (1 ref, 1 val, 40B data).
//
//go:nosplit
func (v CtxRefs1[T1]) Vals1() *Ctx1V1[T1] {
	return (*Ctx1V1[T1])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals2 returns a Ctx1V2 pointer (1 ref, 2 vals, 32B data).
//
//go:nosplit
func (v CtxRefs1[T1]) Vals2() *Ctx1V2[T1] {
	return (*Ctx1V2[T1])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals3 returns a Ctx1V3 pointer (1 ref, 3 vals, 24B data).
//
//go:nosplit
func (v CtxRefs1[T1]) Vals3() *Ctx1V3[T1] {
	return (*Ctx1V3[T1])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals4 returns a Ctx1V4 pointer (1 ref, 4 vals, 16B data).
//
//go:nosplit
func (v CtxRefs1[T1]) Vals4() *Ctx1V4[T1] {
	return (*Ctx1V4[T1])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals5 returns a Ctx1V5 pointer (1 ref, 5 vals, 8B data).
//
//go:nosplit
func (v CtxRefs1[T1]) Vals5() *Ctx1V5[T1] {
	return (*Ctx1V5[T1])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals6 returns a Ctx1V6 pointer (1 ref, 6 vals, 0B data).
//
//go:nosplit
func (v CtxRefs1[T1]) Vals6() *Ctx1V6[T1] {
	return (*Ctx1V6[T1])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// CtxRefs2 is a view into ExtSQE with 2 refs.
// Use its methods to select the number of vals (0-5).
type CtxRefs2[T1, T2 any] struct {
	sqe *ExtSQE
}

// Vals0 returns a Ctx2 pointer (2 refs, 0 vals, 40B data).
//
//go:nosplit
func (v CtxRefs2[T1, T2]) Vals0() *Ctx2[T1, T2] {
	return (*Ctx2[T1, T2])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals1 returns a Ctx2V1 pointer (2 refs, 1 val, 32B data).
//
//go:nosplit
func (v CtxRefs2[T1, T2]) Vals1() *Ctx2V1[T1, T2] {
	return (*Ctx2V1[T1, T2])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals2 returns a Ctx2V2 pointer (2 refs, 2 vals, 24B data).
//
//go:nosplit
func (v CtxRefs2[T1, T2]) Vals2() *Ctx2V2[T1, T2] {
	return (*Ctx2V2[T1, T2])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals3 returns a Ctx2V3 pointer (2 refs, 3 vals, 16B data).
//
//go:nosplit
func (v CtxRefs2[T1, T2]) Vals3() *Ctx2V3[T1, T2] {
	return (*Ctx2V3[T1, T2])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals4 returns a Ctx2V4 pointer (2 refs, 4 vals, 8B data).
//
//go:nosplit
func (v CtxRefs2[T1, T2]) Vals4() *Ctx2V4[T1, T2] {
	return (*Ctx2V4[T1, T2])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals5 returns a Ctx2V5 pointer (2 refs, 5 vals, 0B data).
//
//go:nosplit
func (v CtxRefs2[T1, T2]) Vals5() *Ctx2V5[T1, T2] {
	return (*Ctx2V5[T1, T2])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// CtxRefs3 is a view into ExtSQE with 3 refs.
// Use its methods to select the number of vals (0-4).
type CtxRefs3[T1, T2, T3 any] struct {
	sqe *ExtSQE
}

// Vals0 returns a Ctx3 pointer (3 refs, 0 vals, 32B data).
//
//go:nosplit
func (v CtxRefs3[T1, T2, T3]) Vals0() *Ctx3[T1, T2, T3] {
	return (*Ctx3[T1, T2, T3])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals1 returns a Ctx3V1 pointer (3 refs, 1 val, 24B data).
//
//go:nosplit
func (v CtxRefs3[T1, T2, T3]) Vals1() *Ctx3V1[T1, T2, T3] {
	return (*Ctx3V1[T1, T2, T3])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals2 returns a Ctx3V2 pointer (3 refs, 2 vals, 16B data).
//
//go:nosplit
func (v CtxRefs3[T1, T2, T3]) Vals2() *Ctx3V2[T1, T2, T3] {
	return (*Ctx3V2[T1, T2, T3])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals3 returns a Ctx3V3 pointer (3 refs, 3 vals, 8B data).
//
//go:nosplit
func (v CtxRefs3[T1, T2, T3]) Vals3() *Ctx3V3[T1, T2, T3] {
	return (*Ctx3V3[T1, T2, T3])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals4 returns a Ctx3V4 pointer (3 refs, 4 vals, 0B data).
//
//go:nosplit
func (v CtxRefs3[T1, T2, T3]) Vals4() *Ctx3V4[T1, T2, T3] {
	return (*Ctx3V4[T1, T2, T3])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// CtxRefs4 is a view into ExtSQE with 4 refs.
// Use its methods to select the number of vals (0-3).
type CtxRefs4[T1, T2, T3, T4 any] struct {
	sqe *ExtSQE
}

// Vals0 returns a Ctx4 pointer (4 refs, 0 vals, 24B data).
//
//go:nosplit
func (v CtxRefs4[T1, T2, T3, T4]) Vals0() *Ctx4[T1, T2, T3, T4] {
	return (*Ctx4[T1, T2, T3, T4])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals1 returns a Ctx4V1 pointer (4 refs, 1 val, 16B data).
//
//go:nosplit
func (v CtxRefs4[T1, T2, T3, T4]) Vals1() *Ctx4V1[T1, T2, T3, T4] {
	return (*Ctx4V1[T1, T2, T3, T4])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals2 returns a Ctx4V2 pointer (4 refs, 2 vals, 8B data).
//
//go:nosplit
func (v CtxRefs4[T1, T2, T3, T4]) Vals2() *Ctx4V2[T1, T2, T3, T4] {
	return (*Ctx4V2[T1, T2, T3, T4])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals3 returns a Ctx4V3 pointer (4 refs, 3 vals, 0B data).
//
//go:nosplit
func (v CtxRefs4[T1, T2, T3, T4]) Vals3() *Ctx4V3[T1, T2, T3, T4] {
	return (*Ctx4V3[T1, T2, T3, T4])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// CtxRefs5 is a view into ExtSQE with 5 refs.
// Use its methods to select the number of vals (0-2).
type CtxRefs5[T1, T2, T3, T4, T5 any] struct {
	sqe *ExtSQE
}

// Vals0 returns a Ctx5 pointer (5 refs, 0 vals, 16B data).
//
//go:nosplit
func (v CtxRefs5[T1, T2, T3, T4, T5]) Vals0() *Ctx5[T1, T2, T3, T4, T5] {
	return (*Ctx5[T1, T2, T3, T4, T5])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals1 returns a Ctx5V1 pointer (5 refs, 1 val, 8B data).
//
//go:nosplit
func (v CtxRefs5[T1, T2, T3, T4, T5]) Vals1() *Ctx5V1[T1, T2, T3, T4, T5] {
	return (*Ctx5V1[T1, T2, T3, T4, T5])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals2 returns a Ctx5V2 pointer (5 refs, 2 vals, 0B data).
//
//go:nosplit
func (v CtxRefs5[T1, T2, T3, T4, T5]) Vals2() *Ctx5V2[T1, T2, T3, T4, T5] {
	return (*Ctx5V2[T1, T2, T3, T4, T5])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// CtxRefs6 is a view into ExtSQE with 6 refs.
// Use its methods to select the number of vals (0-1).
type CtxRefs6[T1, T2, T3, T4, T5, T6 any] struct {
	sqe *ExtSQE
}

// Vals0 returns a Ctx6 pointer (6 refs, 0 vals, 8B data).
//
//go:nosplit
func (v CtxRefs6[T1, T2, T3, T4, T5, T6]) Vals0() *Ctx6[T1, T2, T3, T4, T5, T6] {
	return (*Ctx6[T1, T2, T3, T4, T5, T6])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// Vals1 returns a Ctx6V1 pointer (6 refs, 1 val, 0B data).
//
//go:nosplit
func (v CtxRefs6[T1, T2, T3, T4, T5, T6]) Vals1() *Ctx6V1[T1, T2, T3, T4, T5, T6] {
	return (*Ctx6V1[T1, T2, T3, T4, T5, T6])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// CtxRefs7 is a view into ExtSQE with 7 refs.
// The only option is Vals0 (no vals available).
type CtxRefs7[T1, T2, T3, T4, T5, T6, T7 any] struct {
	sqe *ExtSQE
}

// Vals0 returns a Ctx7 pointer (7 refs, 0 vals, 0B data).
//
//go:nosplit
func (v CtxRefs7[T1, T2, T3, T4, T5, T6, T7]) Vals0() *Ctx7[T1, T2, T3, T4, T5, T6, T7] {
	return (*Ctx7[T1, T2, T3, T4, T5, T6, T7])(unsafe.Pointer(&v.sqe.UserData[0]))
}

// ========================================
// Common Shorthand Functions
// ========================================

// --- 0 refs ---

// Ctx0Of is a shorthand for ViewCtx(sqe).Vals0().
// Use when you need just a handler with max data space (56B).
func Ctx0Of(sqe *ExtSQE) *Ctx0 {
	return ViewCtx(sqe).Vals0()
}

// Ctx0V1Of is a shorthand for ViewCtx(sqe).Vals1().
// Use when you need 0 refs and 1 val (e.g., handler + timestamp).
func Ctx0V1Of(sqe *ExtSQE) *Ctx0V1 {
	return ViewCtx(sqe).Vals1()
}

// Ctx0V2Of is a shorthand for ViewCtx(sqe).Vals2().
// Use when you need 0 refs and 2 vals (e.g., handler + offset + length).
func Ctx0V2Of(sqe *ExtSQE) *Ctx0V2 {
	return ViewCtx(sqe).Vals2()
}

// CtxOf is a shorthand for ViewCtx(sqe).Vals0().
func CtxOf(sqe *ExtSQE) *Ctx0 {
	return Ctx0Of(sqe)
}

// CtxV1Of is a shorthand for ViewCtx(sqe).Vals1().
func CtxV1Of(sqe *ExtSQE) *Ctx0V1 {
	return Ctx0V1Of(sqe)
}

// CtxV2Of is a shorthand for ViewCtx(sqe).Vals2().
func CtxV2Of(sqe *ExtSQE) *Ctx0V2 {
	return Ctx0V2Of(sqe)
}

// --- 1 ref ---

// Ctx1Of is a shorthand for ViewCtx1[T](sqe).Vals0().
// Use when you need 1 ref and 0 vals (e.g., handler + connection ref).
func Ctx1Of[T any](sqe *ExtSQE) *Ctx1[T] {
	return ViewCtx1[T](sqe).Vals0()
}

// Ctx1V1Of is a shorthand for ViewCtx1[T](sqe).Vals1() - the most common case.
// Use when you need 1 ref and 1 val (e.g., connection + timestamp).
func Ctx1V1Of[T any](sqe *ExtSQE) *Ctx1V1[T] {
	return ViewCtx1[T](sqe).Vals1()
}

// Ctx1V2Of is a shorthand for ViewCtx1[T](sqe).Vals2().
// Use when you need 1 ref and 2 vals (e.g., connection + offset + length).
func Ctx1V2Of[T any](sqe *ExtSQE) *Ctx1V2[T] {
	return ViewCtx1[T](sqe).Vals2()
}

// --- 2 refs ---

// Ctx2Of is a shorthand for ViewCtx2[T1,T2](sqe).Vals0().
// Use when you need 2 refs and 0 vals (e.g., conn + buffer).
func Ctx2Of[T1, T2 any](sqe *ExtSQE) *Ctx2[T1, T2] {
	return ViewCtx2[T1, T2](sqe).Vals0()
}

// Ctx2V1Of is a shorthand for ViewCtx2[T1,T2](sqe).Vals1().
// Use when you need 2 refs and 1 val (e.g., conn + buf + offset).
func Ctx2V1Of[T1, T2 any](sqe *ExtSQE) *Ctx2V1[T1, T2] {
	return ViewCtx2[T1, T2](sqe).Vals1()
}

// Ctx2V2Of is a shorthand for ViewCtx2[T1,T2](sqe).Vals2().
// Use when you need 2 refs and 2 vals (e.g., conn + buf + offset + length).
func Ctx2V2Of[T1, T2 any](sqe *ExtSQE) *Ctx2V2[T1, T2] {
	return ViewCtx2[T1, T2](sqe).Vals2()
}

// --- 3 refs ---

// Ctx3Of is a shorthand for ViewCtx3[T1,T2,T3](sqe).Vals0().
// Use when you need 3 refs and 0 vals.
func Ctx3Of[T1, T2, T3 any](sqe *ExtSQE) *Ctx3[T1, T2, T3] {
	return ViewCtx3[T1, T2, T3](sqe).Vals0()
}

// Ctx3V1Of is a shorthand for ViewCtx3[T1,T2,T3](sqe).Vals1().
// Use when you need 3 refs and 1 val.
func Ctx3V1Of[T1, T2, T3 any](sqe *ExtSQE) *Ctx3V1[T1, T2, T3] {
	return ViewCtx3[T1, T2, T3](sqe).Vals1()
}

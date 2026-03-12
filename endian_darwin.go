// ©Hayabusa Cloud Co., Ltd. 2026. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build darwin

package uring

// Darwin is always little-endian (both Intel and ARM Macs).
const isBigEndian = false

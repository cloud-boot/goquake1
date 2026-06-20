// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package mathlib is the Go port of tyrquake's common/mathlib.c +
// include/mathlib.h, providing the Vec3 / Plane primitives and the
// rotation, normalisation, side-of-plane, and fixed-point math used
// throughout the rest of the engine.
//
// Upstream pin: github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
// Hand-port date: 2026-06-14 (Q-1a kickoff)
// Port conventions: see ../../CONVENTIONS.md
//
// Mapping notes:
//   - C vec3_t -> Vec3 ([3]float32 with PITCH/YAW/ROLL indices)
//   - C vec_t -> float32 (the engine's float-wide native unit)
//   - C mplane_t -> Plane
//   - C in-place void f(a, b, OUT) -> Go pure func(a, b) OUT;
//     callers do `c := Add(a, b)` instead of `Add(a, b, &c)`. This
//     respects Go value semantics and lets the inliner elide the
//     [3]float32 copy under -O when the result is immediately stored.
//   - C macro DotProduct(a, b) -> method a.Dot(b)
//   - C macro VectorCopy(a, b) -> idiomatic Go assignment b = a (Vec3 is
//     a value type so this is a true copy).
package mathlib

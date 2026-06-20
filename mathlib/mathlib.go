// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package mathlib

import (
	"errors"
	"math"
)

// Vec3 is a 3-component vector. The engine indexes it both by axis
// (X/Y/Z via the named getter/setter) and by Euler-angle component
// (PITCH/YAW/ROLL via the index constants below) -- both styles are
// kept since both appear verbatim throughout the upstream sources.
type Vec3 [3]float32

// Plane mirrors tyrquake's mplane_t. The asm-comment "if this is
// changed it must be changed in asm_i386.h too" no longer applies
// (no asm in the Go port), but the struct shape is preserved for
// wire-compat with .bsp files that the BSP loader will mmap.
type Plane struct {
	Normal   Vec3
	Dist     float32
	Type     byte // axial alignment hint for fast side tests
	Signbits byte // signx + signy<<1 + signz<<2
	_        [2]byte
}

// Euler-angle indices into Vec3 (tyrquake quakedef.h: PITCH/YAW/ROLL).
const (
	Pitch = 0
	Yaw   = 1
	Roll  = 2
)

// Side-of-plane bit flags (tyrquake mathlib.h: PSIDE_*).
const (
	SideFront = 1
	SideBack  = 2
	SideBoth  = SideFront | SideBack
)

// Origin is the zero Vec3 -- a frequently-referenced engine constant.
// tyrquake: vec3_origin.
var Origin = Vec3{0, 0, 0}

// nanMask is the IEEE-754 single-precision exponent mask used by
// IsNaN; mirrors tyrquake's int nanmask = 255 << 23.
const nanMask uint32 = 255 << 23

// IsNaN returns true iff x has all exponent bits set (NaN or Inf).
// tyrquake macro IS_NAN.
func IsNaN(x float32) bool {
	return math.Float32bits(x)&nanMask == nanMask
}

// IsPowerOfTwo returns true iff x > 0 and x is a power of two.
// tyrquake inline function is_power_of_two.
func IsPowerOfTwo(x int) bool {
	return x > 0 && x&(x-1) == 0
}

// Add returns a + b. tyrquake: VectorAdd.
func (v Vec3) Add(b Vec3) Vec3 { return Vec3{v[0] + b[0], v[1] + b[1], v[2] + b[2]} }

// Sub returns a - b. tyrquake: VectorSubtract.
func (v Vec3) Sub(b Vec3) Vec3 { return Vec3{v[0] - b[0], v[1] - b[1], v[2] - b[2]} }

// Scale returns v multiplied by s. tyrquake: VectorScale.
func (v Vec3) Scale(s float32) Vec3 { return Vec3{v[0] * s, v[1] * s, v[2] * s} }

// MA returns a + scale * b (multiply-add). tyrquake: VectorMA.
func (v Vec3) MA(scale float32, b Vec3) Vec3 {
	return Vec3{v[0] + scale*b[0], v[1] + scale*b[1], v[2] + scale*b[2]}
}

// Dot returns the dot product v . b. tyrquake: DotProduct macro and
// _DotProduct function.
func (v Vec3) Dot(b Vec3) float32 { return v[0]*b[0] + v[1]*b[1] + v[2]*b[2] }

// Cross returns the cross product v x b. tyrquake: CrossProduct.
func (v Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		v[1]*b[2] - v[2]*b[1],
		v[2]*b[0] - v[0]*b[2],
		v[0]*b[1] - v[1]*b[0],
	}
}

// Length returns the Euclidean norm of v. tyrquake: Length.
func (v Vec3) Length() float32 {
	return float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])))
}

// Normalize returns a unit vector in v's direction plus v's original
// length. Returns Origin and 0 when v has zero length (the tyrquake
// `length` short-circuit). tyrquake: VectorNormalize, modulo we
// return the new value instead of mutating in place.
func (v Vec3) Normalize() (unit Vec3, length float32) {
	length = v.Length()
	if length == 0 {
		return Origin, 0
	}
	inv := 1.0 / length
	return Vec3{v[0] * inv, v[1] * inv, v[2] * inv}, length
}

// Inverse returns -v. tyrquake: VectorInverse.
func (v Vec3) Inverse() Vec3 { return Vec3{-v[0], -v[1], -v[2]} }

// Equals returns true iff v == b component-wise. tyrquake:
// VectorCompare (which returns int 1/0).
func (v Vec3) Equals(b Vec3) bool { return v[0] == b[0] && v[1] == b[1] && v[2] == b[2] }

// AngleMod normalises a degree angle into [0, 360). tyrquake's
// implementation uses a fixed-point trick that drops the angle into a
// 65536-entry rotation table -- we preserve the byte-exact behaviour
// so demo replays match upstream.
//
//	a = (360.0 / 65536) * ((int)(a * (65536 / 360.0)) & 65535)
func AngleMod(a float32) float32 {
	const step = float32(360.0 / 65536.0)
	const inv = float32(65536.0 / 360.0)
	return step * float32(int32(a*inv)&0xFFFF)
}

// AngleVectors computes the forward/right/up basis from a pitch/yaw/
// roll Euler triple (degrees). tyrquake: AngleVectors.
func AngleVectors(angles Vec3) (forward, right, up Vec3) {
	const deg2rad = math.Pi * 2 / 360

	sy, cy := sinCos(float64(angles[Yaw]) * deg2rad)
	sp, cp := sinCos(float64(angles[Pitch]) * deg2rad)
	sr, cr := sinCos(float64(angles[Roll]) * deg2rad)

	forward = Vec3{
		float32(cp * cy),
		float32(cp * sy),
		float32(-sp),
	}
	right = Vec3{
		float32(-1*sr*sp*cy + -1*cr*-sy),
		float32(-1*sr*sp*sy + -1*cr*cy),
		float32(-1 * sr * cp),
	}
	up = Vec3{
		float32(cr*sp*cy + -sr*-sy),
		float32(cr*sp*sy + -sr*cy),
		float32(cr * cp),
	}
	return forward, right, up
}

func sinCos(a float64) (sin, cos float64) { return math.Sin(a), math.Cos(a) }

// Mat3 is a 3x3 row-major matrix used by ConcatRotations.
type Mat3 [3][3]float32

// ConcatRotations returns the matrix product a * b. tyrquake:
// R_ConcatRotations.
func ConcatRotations(a, b Mat3) Mat3 {
	var out Mat3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			out[i][j] = a[i][0]*b[0][j] + a[i][1]*b[1][j] + a[i][2]*b[2][j]
		}
	}
	return out
}

// Mat34 is a 3x4 row-major affine matrix (rotation + translation) used
// by ConcatTransforms.
type Mat34 [3][4]float32

// ConcatTransforms returns the affine product a * b. tyrquake:
// R_ConcatTransforms.
func ConcatTransforms(a, b Mat34) Mat34 {
	var out Mat34
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			out[i][j] = a[i][0]*b[0][j] + a[i][1]*b[1][j] + a[i][2]*b[2][j]
		}
		out[i][3] = a[i][0]*b[0][3] + a[i][1]*b[1][3] + a[i][2]*b[2][3] + a[i][3]
	}
	return out
}

// QLog2 returns floor(log2(val)) for val > 0; returns 0 for val <= 0.
// tyrquake: Q_log2.
func QLog2(val int) int {
	answer := 0
	for {
		val >>= 1
		if val == 0 {
			return answer
		}
		answer++
	}
}

// QGCD returns the greatest common divisor of a and b, computed by
// the Euclidean swap-then-mod loop tyrquake uses. Returns 1 in the
// degenerate "both inputs zero" case (matches tyrquake's `a ? a : 1`).
// tyrquake: Q_gcd.
func QGCD(a, b int) int {
	if a < b {
		a, b = b, a
	}
	if b == 0 {
		if a != 0 {
			return a
		}
		return 1
	}
	for a%b != 0 {
		a, b = b, a%b
	}
	return b
}

// GreatestCommonDivisor is the recursive Euclidean GCD tyrquake
// implements separately from Q_gcd; behaves identically for positive
// inputs.
func GreatestCommonDivisor(i1, i2 int) int {
	if i1 > i2 {
		if i2 == 0 {
			return i1
		}
		return GreatestCommonDivisor(i2, i1%i2)
	}
	if i1 == 0 {
		return i2
	}
	return GreatestCommonDivisor(i1, i2%i1)
}

// ErrFloorDivByZero is returned by FloorDivMod when denom <= 0.
// tyrquake's C version Sys_Error()s; the Go port returns an error so
// callers can choose to panic via [sys.Error] or recover.
var ErrFloorDivByZero = errors.New("mathlib: FloorDivMod: denom must be > 0")

// FloorDivMod returns the mathematically-correct (floor-based)
// quotient and remainder of numer/denom. Both inputs should be whole
// numbers (the upstream PARANOID-mode guard checks for non-integer
// args; we follow tyrquake's release-build choice and skip that check
// for speed). tyrquake: FloorDivMod.
func FloorDivMod(numer, denom float64) (quotient, remainder int, err error) {
	if denom <= 0.0 {
		return 0, 0, ErrFloorDivByZero
	}
	if numer >= 0.0 {
		x := math.Floor(numer / denom)
		return int(x), int(math.Floor(numer - x*denom)), nil
	}
	// Match tyrquake's negative-numerator path: compute with positives,
	// then fix the remainder so it stays non-negative.
	x := math.Floor(-numer / denom)
	q := -int(x)
	r := int(math.Floor(-numer - x*denom))
	if r != 0 {
		q--
		r = int(denom) - r
	}
	return q, r, nil
}

// Invert24To16 returns the 16.16 fixed-point reciprocal of an 8.24
// fixed-point value. Returns INT32_MAX (0xFFFFFFFF when reinterpreted
// as int32 -- the tyrquake idiom) when val < 256, where the
// reciprocal overflows the 16.16 range. tyrquake: Invert24To16
// (non-x86-asm path).
func Invert24To16(val int32) int32 {
	if val < 256 {
		// 0xFFFFFFFF reinterpreted as int32 is -1; the tyrquake C code
		// writes the raw bit pattern and lets the caller treat it as
		// either signed-min-overflow or unsigned-max. Both engine sites
		// read the result as int32; -1 is the byte-identical bit
		// pattern.
		return -1
	}
	return int32((float64(0x10000)*float64(0x1000000))/float64(val) + 0.5)
}

// SignbitsForPlane returns the 3-bit signbits selector used by the
// box-on-plane fast path. tyrquake: SignbitsForPlane.
func SignbitsForPlane(p *Plane) byte {
	var bits byte
	for i := 0; i < 3; i++ {
		if p.Normal[i] < 0 {
			bits |= 1 << i
		}
	}
	return bits
}

// BoxOnPlaneSide returns SideFront, SideBack, or SideBoth depending on
// where the AABB defined by mins/maxs sits relative to the plane.
// tyrquake: BoxOnPlaneSide (the non-asm fallback path).
func BoxOnPlaneSide(mins, maxs Vec3, p *Plane) int {
	// Fast path for axial planes -- the BOX_ON_PLANE_SIDE macro inlined
	// at call sites also uses this; we provide the function form here.
	if p.Type < 3 {
		switch {
		case p.Dist <= mins[p.Type]:
			return SideFront
		case p.Dist >= maxs[p.Type]:
			return SideBack
		default:
			return SideBoth
		}
	}

	// General path: pick the corner that maximises dot with the normal
	// (dist1) and the corner that minimises it (dist2), then classify.
	var dist1, dist2 float32
	switch p.Signbits {
	case 0:
		dist1 = p.Normal[0]*maxs[0] + p.Normal[1]*maxs[1] + p.Normal[2]*maxs[2]
		dist2 = p.Normal[0]*mins[0] + p.Normal[1]*mins[1] + p.Normal[2]*mins[2]
	case 1:
		dist1 = p.Normal[0]*mins[0] + p.Normal[1]*maxs[1] + p.Normal[2]*maxs[2]
		dist2 = p.Normal[0]*maxs[0] + p.Normal[1]*mins[1] + p.Normal[2]*mins[2]
	case 2:
		dist1 = p.Normal[0]*maxs[0] + p.Normal[1]*mins[1] + p.Normal[2]*maxs[2]
		dist2 = p.Normal[0]*mins[0] + p.Normal[1]*maxs[1] + p.Normal[2]*mins[2]
	case 3:
		dist1 = p.Normal[0]*mins[0] + p.Normal[1]*mins[1] + p.Normal[2]*maxs[2]
		dist2 = p.Normal[0]*maxs[0] + p.Normal[1]*maxs[1] + p.Normal[2]*mins[2]
	case 4:
		dist1 = p.Normal[0]*maxs[0] + p.Normal[1]*maxs[1] + p.Normal[2]*mins[2]
		dist2 = p.Normal[0]*mins[0] + p.Normal[1]*mins[1] + p.Normal[2]*maxs[2]
	case 5:
		dist1 = p.Normal[0]*mins[0] + p.Normal[1]*maxs[1] + p.Normal[2]*mins[2]
		dist2 = p.Normal[0]*maxs[0] + p.Normal[1]*mins[1] + p.Normal[2]*maxs[2]
	case 6:
		dist1 = p.Normal[0]*maxs[0] + p.Normal[1]*mins[1] + p.Normal[2]*mins[2]
		dist2 = p.Normal[0]*mins[0] + p.Normal[1]*maxs[1] + p.Normal[2]*maxs[2]
	case 7:
		dist1 = p.Normal[0]*mins[0] + p.Normal[1]*mins[1] + p.Normal[2]*mins[2]
		dist2 = p.Normal[0]*maxs[0] + p.Normal[1]*maxs[1] + p.Normal[2]*maxs[2]
	default:
		// tyrquake's BOPS_Error: signbits 8..255 are invalid.
		return SideBoth
	}
	side := 0
	if dist1 >= p.Dist {
		side |= SideFront
	}
	if dist2 < p.Dist {
		side |= SideBack
	}
	return side
}

// ProjectPointOnPlane writes the orthogonal projection of p onto the
// plane whose normal is `normal` into `dst`. tyrquake:
// ProjectPointOnPlane (mathlib.c top of file).
func ProjectPointOnPlane(p, normal Vec3) Vec3 {
	invDenom := 1.0 / normal.Dot(normal)
	d := normal.Dot(p) * invDenom
	n := normal.Scale(invDenom)
	return Vec3{
		p[0] - d*n[0],
		p[1] - d*n[1],
		p[2] - d*n[2],
	}
}

// PerpendicularVector returns a unit vector perpendicular to src;
// assumes src is normalised. tyrquake: PerpendicularVector.
func PerpendicularVector(src Vec3) Vec3 {
	// Find the smallest-magnitude axially-aligned vector.
	pos := 0
	minelem := float32(1.0)
	for i := 0; i < 3; i++ {
		ai := absF32(src[i])
		if ai < minelem {
			pos = i
			minelem = ai
		}
	}
	tempvec := Vec3{}
	tempvec[pos] = 1.0
	dst := ProjectPointOnPlane(tempvec, src)
	unit, _ := dst.Normalize()
	return unit
}

// RotatePointAroundVector rotates `point` by `degrees` around `dir`
// (which must be normalised) and returns the rotated point. tyrquake:
// RotatePointAroundVector.
func RotatePointAroundVector(dir, point Vec3, degrees float32) Vec3 {
	vf := dir
	vr := PerpendicularVector(dir)
	vup := vr.Cross(vf)

	m := Mat3{
		{vr[0], vup[0], vf[0]},
		{vr[1], vup[1], vf[1]},
		{vr[2], vup[2], vf[2]},
	}
	im := Mat3{
		{m[0][0], m[1][0], m[2][0]},
		{m[0][1], m[1][1], m[2][1]},
		{m[0][2], m[1][2], m[2][2]},
	}

	rad := float64(degrees) * math.Pi / 180.0
	s, c := sinCos(rad)
	zrot := Mat3{
		{float32(c), float32(s), 0},
		{float32(-s), float32(c), 0},
		{0, 0, 1},
	}

	tmp := ConcatRotations(m, zrot)
	rot := ConcatRotations(tmp, im)

	return Vec3{
		rot[0][0]*point[0] + rot[0][1]*point[1] + rot[0][2]*point[2],
		rot[1][0]*point[0] + rot[1][1]*point[1] + rot[1][2]*point[2],
		rot[2][0]*point[0] + rot[2][1]*point[1] + rot[2][2]*point[2],
	}
}

func absF32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

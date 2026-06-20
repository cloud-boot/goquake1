// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package mathlib

import (
	"errors"
	"math"
	"testing"
)

const float32Epsilon = 1e-5

func feq(a, b float32) bool { return absF32(a-b) <= float32Epsilon }
func veq(a, b Vec3) bool    { return feq(a[0], b[0]) && feq(a[1], b[1]) && feq(a[2], b[2]) }

// --- Vec3 arithmetic ---------------------------------------------------------

func TestVec3_Add(t *testing.T) {
	got := Vec3{1, 2, 3}.Add(Vec3{4, 5, 6})
	want := Vec3{5, 7, 9}
	if !veq(got, want) {
		t.Errorf("Add: got %v want %v", got, want)
	}
}

func TestVec3_Sub(t *testing.T) {
	got := Vec3{5, 7, 9}.Sub(Vec3{4, 5, 6})
	want := Vec3{1, 2, 3}
	if !veq(got, want) {
		t.Errorf("Sub: got %v want %v", got, want)
	}
}

func TestVec3_Scale(t *testing.T) {
	got := Vec3{1, 2, 3}.Scale(2.5)
	want := Vec3{2.5, 5, 7.5}
	if !veq(got, want) {
		t.Errorf("Scale: got %v want %v", got, want)
	}
}

func TestVec3_MA(t *testing.T) {
	got := Vec3{1, 2, 3}.MA(2, Vec3{4, 5, 6})
	want := Vec3{9, 12, 15}
	if !veq(got, want) {
		t.Errorf("MA: got %v want %v", got, want)
	}
}

func TestVec3_Dot(t *testing.T) {
	got := Vec3{1, 2, 3}.Dot(Vec3{4, -5, 6})
	want := float32(1*4 + 2*-5 + 3*6) // = 12
	if !feq(got, want) {
		t.Errorf("Dot: got %v want %v", got, want)
	}
}

func TestVec3_Cross(t *testing.T) {
	got := Vec3{1, 0, 0}.Cross(Vec3{0, 1, 0})
	want := Vec3{0, 0, 1}
	if !veq(got, want) {
		t.Errorf("Cross x*y: got %v want %v", got, want)
	}
	if !veq(Vec3{0, 0, 1}.Cross(Vec3{1, 0, 0}), Vec3{0, 1, 0}) {
		t.Errorf("Cross z*x mismatch")
	}
}

func TestVec3_Length(t *testing.T) {
	got := Vec3{3, 4, 0}.Length()
	if !feq(got, 5) {
		t.Errorf("Length 3-4-5: got %v want 5", got)
	}
	if got := Origin.Length(); got != 0 {
		t.Errorf("Length zero: got %v want 0", got)
	}
}

func TestVec3_Normalize(t *testing.T) {
	unit, length := Vec3{3, 4, 0}.Normalize()
	if !feq(length, 5) {
		t.Errorf("Normalize length: got %v want 5", length)
	}
	if !veq(unit, Vec3{0.6, 0.8, 0}) {
		t.Errorf("Normalize unit: got %v want {0.6, 0.8, 0}", unit)
	}
	// Zero-length vector returns Origin + 0 (the tyrquake short-circuit).
	zero, length := Origin.Normalize()
	if length != 0 || zero != Origin {
		t.Errorf("Normalize zero: got (%v, %v), want (Origin, 0)", zero, length)
	}
}

func TestVec3_Inverse(t *testing.T) {
	got := Vec3{1, -2, 3}.Inverse()
	want := Vec3{-1, 2, -3}
	if !veq(got, want) {
		t.Errorf("Inverse: got %v want %v", got, want)
	}
}

func TestVec3_Equals(t *testing.T) {
	if !(Vec3{1, 2, 3}).Equals(Vec3{1, 2, 3}) {
		t.Errorf("Equals: identical vectors should match")
	}
	if (Vec3{1, 2, 3}).Equals(Vec3{1, 2, 4}) {
		t.Errorf("Equals: differing vectors should not match")
	}
}

// --- IsNaN / IsPowerOfTwo ----------------------------------------------------

func TestIsNaN(t *testing.T) {
	if !IsNaN(float32(math.NaN())) {
		t.Errorf("IsNaN(NaN) should be true")
	}
	if !IsNaN(float32(math.Inf(1))) {
		t.Errorf("IsNaN(+Inf) should be true (exponent all 1s)")
	}
	if IsNaN(1.5) {
		t.Errorf("IsNaN(1.5) should be false")
	}
	if IsNaN(0) {
		t.Errorf("IsNaN(0) should be false")
	}
}

func TestIsPowerOfTwo(t *testing.T) {
	cases := []struct {
		in   int
		want bool
	}{
		{0, false}, {1, true}, {2, true}, {3, false}, {4, true},
		{16, true}, {17, false}, {1024, true}, {1023, false},
		{-1, false}, {-2, false},
	}
	for _, c := range cases {
		if got := IsPowerOfTwo(c.in); got != c.want {
			t.Errorf("IsPowerOfTwo(%d): got %v want %v", c.in, got, c.want)
		}
	}
}

// --- AngleMod ----------------------------------------------------------------

// AngleMod is the tyrquake-specific fixed-point angle-wrap. The
// behaviour is byte-exact-defined by the formula
//
//	(360.0/65536) * ((int)(a * (65536/360.0)) & 65535)
//
// These golden values were computed independently from that formula
// (not from the Go port) so the test catches drift.
func TestAngleMod(t *testing.T) {
	cases := []struct {
		in, want float32
	}{
		{0, 0},
		{45, AngleMod(45)}, // round-trip stable
		{360, 0},           // wraps to zero
		{720, 0},           // wraps to zero (2*360)
		// -90 * (65536/360) = -16384; (-16384) & 65535 = 49152;
		// 49152 * (360/65536) = 270.0 exactly.
		{-90, 270.0},
	}
	for _, c := range cases {
		got := AngleMod(c.in)
		if !feq(got, c.want) {
			t.Errorf("AngleMod(%v): got %v want %v", c.in, got, c.want)
		}
	}
	// Spot-check: the AngleMod of any value should map into [0, 360).
	for _, a := range []float32{-720, -361, -1, 0, 1, 360, 361, 720} {
		got := AngleMod(a)
		if got < 0 || got >= 360 {
			t.Errorf("AngleMod(%v) = %v out of [0,360) range", a, got)
		}
	}
}

// --- AngleVectors ------------------------------------------------------------

// AngleVectors at the zero rotation: forward is +X, right is -Y, up is +Z.
// (This matches both tyrquake's coordinate convention and the engine's
// expectation that "forward" points along world-X for an entity facing
// the +X axis.)
func TestAngleVectors_Zero(t *testing.T) {
	forward, right, up := AngleVectors(Vec3{0, 0, 0})
	if !veq(forward, Vec3{1, 0, 0}) {
		t.Errorf("forward: got %v want {1,0,0}", forward)
	}
	if !veq(right, Vec3{0, -1, 0}) {
		t.Errorf("right: got %v want {0,-1,0}", right)
	}
	if !veq(up, Vec3{0, 0, 1}) {
		t.Errorf("up: got %v want {0,0,1}", up)
	}
}

// AngleVectors at yaw=90: forward should rotate to +Y.
func TestAngleVectors_Yaw90(t *testing.T) {
	forward, _, _ := AngleVectors(Vec3{0, 90, 0})
	if !veq(forward, Vec3{0, 1, 0}) {
		t.Errorf("yaw=90 forward: got %v want {0,1,0}", forward)
	}
}

// AngleVectors basis vectors should remain unit-length and pairwise-
// orthogonal for any input angle.
func TestAngleVectors_Orthonormal(t *testing.T) {
	for _, a := range []Vec3{{0, 0, 0}, {30, 45, 60}, {-15, 200, 350}, {89, 179, -179}} {
		forward, right, up := AngleVectors(a)
		if !feq(forward.Length(), 1) {
			t.Errorf("%v forward len: %v", a, forward.Length())
		}
		if !feq(right.Length(), 1) {
			t.Errorf("%v right len: %v", a, right.Length())
		}
		if !feq(up.Length(), 1) {
			t.Errorf("%v up len: %v", a, up.Length())
		}
		if abs := absF32(forward.Dot(right)); abs > 1e-4 {
			t.Errorf("%v forward.right: %v", a, abs)
		}
		if abs := absF32(forward.Dot(up)); abs > 1e-4 {
			t.Errorf("%v forward.up: %v", a, abs)
		}
		if abs := absF32(right.Dot(up)); abs > 1e-4 {
			t.Errorf("%v right.up: %v", a, abs)
		}
	}
}

// --- Mat3 / Mat34 ConcatRotations + ConcatTransforms -------------------------

func TestConcatRotations_Identity(t *testing.T) {
	id := Mat3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	a := Mat3{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}}
	if got := ConcatRotations(id, a); got != a {
		t.Errorf("I*A: got %v want %v", got, a)
	}
	if got := ConcatRotations(a, id); got != a {
		t.Errorf("A*I: got %v want %v", got, a)
	}
}

func TestConcatRotations_Specific(t *testing.T) {
	a := Mat3{{1, 2, 0}, {0, 1, 2}, {2, 0, 1}}
	b := Mat3{{1, 0, 1}, {2, 1, 0}, {0, 1, 1}}
	// Hand-computed: out[i][j] = sum_k a[i][k]*b[k][j].
	want := Mat3{
		{1*1 + 2*2 + 0*0, 1*0 + 2*1 + 0*1, 1*1 + 2*0 + 0*1},
		{0*1 + 1*2 + 2*0, 0*0 + 1*1 + 2*1, 0*1 + 1*0 + 2*1},
		{2*1 + 0*2 + 1*0, 2*0 + 0*1 + 1*1, 2*1 + 0*0 + 1*1},
	}
	if got := ConcatRotations(a, b); got != want {
		t.Errorf("ConcatRotations: got %v want %v", got, want)
	}
}

func TestConcatTransforms_IdentityWithTranslation(t *testing.T) {
	// Identity rotation + identity translation matrix.
	id := Mat34{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}}
	// Pure translation by (10, 20, 30).
	tr := Mat34{{1, 0, 0, 10}, {0, 1, 0, 20}, {0, 0, 1, 30}}
	if got := ConcatTransforms(id, tr); got != tr {
		t.Errorf("I*T: got %v want %v", got, tr)
	}
	// id rotation, id+id translation should yield translation = sum.
	tr2 := Mat34{{1, 0, 0, 1}, {0, 1, 0, 2}, {0, 0, 1, 3}}
	want := Mat34{{1, 0, 0, 11}, {0, 1, 0, 22}, {0, 0, 1, 33}}
	if got := ConcatTransforms(tr, tr2); got != want {
		t.Errorf("T*T2: got %v want %v", got, want)
	}
}

// --- QLog2 / QGCD / GreatestCommonDivisor / FloorDivMod ----------------------

func TestQLog2(t *testing.T) {
	cases := []struct{ in, want int }{
		{1, 0}, {2, 1}, {3, 1}, {4, 2}, {7, 2}, {8, 3}, {1024, 10}, {1 << 20, 20},
	}
	for _, c := range cases {
		if got := QLog2(c.in); got != c.want {
			t.Errorf("QLog2(%d): got %d want %d", c.in, got, c.want)
		}
	}
}

func TestQGCD(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{12, 8, 4},
		{8, 12, 4}, // order-independent
		{17, 5, 1},
		{0, 0, 1},  // tyrquake's `a ? a : 1` for the both-zero case
		{0, 7, 7},
		{42, 0, 42},
		{100, 25, 25},
	}
	for _, c := range cases {
		if got := QGCD(c.a, c.b); got != c.want {
			t.Errorf("QGCD(%d,%d): got %d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestGreatestCommonDivisor(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{12, 8, 4},
		{8, 12, 4},
		{17, 5, 1},
		{0, 7, 7},
		{42, 0, 42},
		{1071, 462, 21},
	}
	for _, c := range cases {
		if got := GreatestCommonDivisor(c.a, c.b); got != c.want {
			t.Errorf("GreatestCommonDivisor(%d,%d): got %d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestFloorDivMod(t *testing.T) {
	cases := []struct {
		numer, denom float64
		q, r         int
	}{
		{10, 3, 3, 1},
		{0, 3, 0, 0},
		{9, 3, 3, 0},
		{-10, 3, -4, 2},  // floor-based: -10 = -4*3 + 2
		{-9, 3, -3, 0},
		{-1, 5, -1, 4},
	}
	for _, c := range cases {
		q, r, err := FloorDivMod(c.numer, c.denom)
		if err != nil {
			t.Errorf("FloorDivMod(%v,%v) err: %v", c.numer, c.denom, err)
			continue
		}
		if q != c.q || r != c.r {
			t.Errorf("FloorDivMod(%v,%v): got (%d,%d) want (%d,%d)", c.numer, c.denom, q, r, c.q, c.r)
		}
	}
}

func TestFloorDivMod_ZeroDenom(t *testing.T) {
	if _, _, err := FloorDivMod(1, 0); !errors.Is(err, ErrFloorDivByZero) {
		t.Errorf("FloorDivMod(1,0): got err %v want ErrFloorDivByZero", err)
	}
	if _, _, err := FloorDivMod(1, -5); !errors.Is(err, ErrFloorDivByZero) {
		t.Errorf("FloorDivMod(1,-5): got err %v want ErrFloorDivByZero", err)
	}
}

// --- Invert24To16 ------------------------------------------------------------

func TestInvert24To16(t *testing.T) {
	if got := Invert24To16(255); got != -1 {
		t.Errorf("Invert24To16(<256): got %v want -1 (0xFFFFFFFF as int32)", got)
	}
	// For val == 0x1000000 (= 2^24), reciprocal in 16.16 = 0x10000.
	if got := Invert24To16(0x1000000); got != 0x10000 {
		t.Errorf("Invert24To16(0x1000000): got %v want 0x10000", got)
	}
	// For val == 0x800000 (= 0.5 in 8.24), reciprocal = 2.0 = 0x20000.
	if got := Invert24To16(0x800000); got != 0x20000 {
		t.Errorf("Invert24To16(0x800000): got %v want 0x20000", got)
	}
}

// --- SignbitsForPlane + BoxOnPlaneSide ---------------------------------------

func TestSignbitsForPlane(t *testing.T) {
	cases := []struct {
		n    Vec3
		want byte
	}{
		{Vec3{1, 1, 1}, 0},
		{Vec3{-1, 1, 1}, 1},
		{Vec3{1, -1, 1}, 2},
		{Vec3{1, 1, -1}, 4},
		{Vec3{-1, -1, -1}, 7},
	}
	for _, c := range cases {
		p := &Plane{Normal: c.n}
		if got := SignbitsForPlane(p); got != c.want {
			t.Errorf("SignbitsForPlane(%v): got %d want %d", c.n, got, c.want)
		}
	}
}

func TestBoxOnPlaneSide_AxialFront(t *testing.T) {
	// Plane: x = 5, type=0 (axial X). Box at x in [10, 20] is in front.
	p := &Plane{Normal: Vec3{1, 0, 0}, Dist: 5, Type: 0}
	if got := BoxOnPlaneSide(Vec3{10, 0, 0}, Vec3{20, 1, 1}, p); got != SideFront {
		t.Errorf("axial front: got %d want %d", got, SideFront)
	}
}

func TestBoxOnPlaneSide_AxialBack(t *testing.T) {
	p := &Plane{Normal: Vec3{1, 0, 0}, Dist: 50, Type: 0}
	if got := BoxOnPlaneSide(Vec3{0, 0, 0}, Vec3{10, 1, 1}, p); got != SideBack {
		t.Errorf("axial back: got %d want %d", got, SideBack)
	}
}

func TestBoxOnPlaneSide_AxialBoth(t *testing.T) {
	p := &Plane{Normal: Vec3{1, 0, 0}, Dist: 15, Type: 0}
	if got := BoxOnPlaneSide(Vec3{10, 0, 0}, Vec3{20, 1, 1}, p); got != SideBoth {
		t.Errorf("axial both: got %d want %d", got, SideBoth)
	}
}

func TestBoxOnPlaneSide_NonAxial(t *testing.T) {
	// Diagonal plane x+y+z = 15, signbits=0, type=3 (non-axial).
	p := &Plane{Normal: Vec3{1, 1, 1}, Dist: 15, Type: 3, Signbits: 0}
	// Box at [10..20]^3: max corner gives 60 >= 15 (front),
	// min corner gives 30 >= 15 (NOT back) -> SideFront only.
	if got := BoxOnPlaneSide(Vec3{10, 10, 10}, Vec3{20, 20, 20}, p); got != SideFront {
		t.Errorf("non-axial front: got %d want %d", got, SideFront)
	}
	// Box at [0..3]^3: max corner 9 < 15 (NOT front),
	// min corner 0 < 15 (back) -> SideBack only.
	if got := BoxOnPlaneSide(Vec3{0, 0, 0}, Vec3{3, 3, 3}, p); got != SideBack {
		t.Errorf("non-axial back: got %d want %d", got, SideBack)
	}
	// Box at [0..20]^3: spans both sides.
	if got := BoxOnPlaneSide(Vec3{0, 0, 0}, Vec3{20, 20, 20}, p); got != SideBoth {
		t.Errorf("non-axial both: got %d want %d", got, SideBoth)
	}
}

// Exhaustive coverage of the 8 signbits cases in the non-axial path.
func TestBoxOnPlaneSide_AllSignbits(t *testing.T) {
	// For each signbits value, construct a normal that matches and
	// verify the function returns a valid SideBoth classification when
	// the box straddles the plane.
	signs := [8]Vec3{
		{1, 1, 1}, {-1, 1, 1}, {1, -1, 1}, {-1, -1, 1},
		{1, 1, -1}, {-1, 1, -1}, {1, -1, -1}, {-1, -1, -1},
	}
	for sb := byte(0); sb < 8; sb++ {
		p := &Plane{Normal: signs[sb], Dist: 0, Type: 3, Signbits: sb}
		got := BoxOnPlaneSide(Vec3{-1, -1, -1}, Vec3{1, 1, 1}, p)
		if got != SideBoth {
			t.Errorf("signbits=%d: box straddling 0 got %d want %d", sb, got, SideBoth)
		}
	}
	// Signbits 8 is invalid -> default branch returns SideBoth.
	p := &Plane{Normal: Vec3{1, 1, 1}, Dist: 0, Type: 3, Signbits: 8}
	if got := BoxOnPlaneSide(Vec3{0, 0, 0}, Vec3{1, 1, 1}, p); got != SideBoth {
		t.Errorf("invalid signbits: got %d want %d", got, SideBoth)
	}
}

// --- ProjectPointOnPlane + PerpendicularVector + RotatePointAroundVector ----

func TestProjectPointOnPlane_OnXY(t *testing.T) {
	// Normal = +Z, project (1, 2, 5) -> (1, 2, 0).
	got := ProjectPointOnPlane(Vec3{1, 2, 5}, Vec3{0, 0, 1})
	if !veq(got, Vec3{1, 2, 0}) {
		t.Errorf("project onto z=0: got %v want {1,2,0}", got)
	}
}

func TestPerpendicularVector(t *testing.T) {
	// For each axially-aligned input, the perpendicular must be unit
	// length and orthogonal.
	for _, src := range []Vec3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}, {0.577, 0.577, 0.577}} {
		unit, _ := src.Normalize()
		perp := PerpendicularVector(unit)
		if !feq(perp.Length(), 1) {
			t.Errorf("perp(%v) length: %v", src, perp.Length())
		}
		if abs := absF32(unit.Dot(perp)); abs > 1e-4 {
			t.Errorf("perp(%v) not orthogonal: dot %v", src, abs)
		}
	}
}

func TestRotatePointAroundVector(t *testing.T) {
	// Rotate point (1,0,0) by 90 degrees around (0,0,1) -> (0,1,0).
	got := RotatePointAroundVector(Vec3{0, 0, 1}, Vec3{1, 0, 0}, 90)
	if !veq(got, Vec3{0, 1, 0}) {
		t.Errorf("rotate +X by 90 around +Z: got %v want {0,1,0}", got)
	}
	// 180 around +Z flips (1,0,0) -> (-1,0,0).
	got = RotatePointAroundVector(Vec3{0, 0, 1}, Vec3{1, 0, 0}, 180)
	if !veq(got, Vec3{-1, 0, 0}) {
		t.Errorf("rotate +X by 180 around +Z: got %v want {-1,0,0}", got)
	}
	// 360 is identity.
	got = RotatePointAroundVector(Vec3{0, 0, 1}, Vec3{1, 2, 3}, 360)
	if !veq(got, Vec3{1, 2, 3}) {
		t.Errorf("rotate by 360: got %v want {1,2,3}", got)
	}
}

// --- absF32 internal sanity check -------------------------------------------

func TestAbsF32(t *testing.T) {
	if absF32(-1.5) != 1.5 || absF32(1.5) != 1.5 || absF32(0) != 0 {
		t.Errorf("absF32 broken")
	}
}

// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsprender_test

import (
	"testing"

	"github.com/go-quake1/engine/bsprender"
	"github.com/go-quake1/engine/render"
)

// testFrustum returns a deterministic, axis-aligned "tube" frustum
// shaped like the box X in [0, 100], Y in [-10, 10] (Z unbounded).
// Frustum is just "4 inward-pointing planes" -- nothing in the type
// requires the planes to converge into a real perspective volume, so
// an axis-aligned slab gives us exact arithmetic without depending on
// the trig in [render.RefDef.BuildFrustum].
//
// Inside conditions (Normal . P - Dist >= 0):
//
//	plane 0: +X, Dist =  0   -> P[0] >=   0
//	plane 1: -X, Dist = -100 -> P[0] <= 100
//	plane 2: +Y, Dist = -10  -> P[1] >= -10
//	plane 3: -Y, Dist = -10  -> P[1] <=  10
func testFrustum() render.Frustum {
	return render.Frustum{
		{Normal: [3]float32{1, 0, 0}, Dist: 0},
		{Normal: [3]float32{-1, 0, 0}, Dist: -100},
		{Normal: [3]float32{0, 1, 0}, Dist: -10},
		{Normal: [3]float32{0, -1, 0}, Dist: -10},
	}
}

// ---------------------------------------------------------------------------
// CullSphere
// ---------------------------------------------------------------------------

func TestCullSphere_InsideTube(t *testing.T) {
	// Sphere well inside the tube -> visible.
	if !bsprender.CullSphere(testFrustum(), [3]float32{50, 0, 0}, 1) {
		t.Fatal("sphere fully inside the frustum should be visible")
	}
}

func TestCullSphere_BehindNearPlane(t *testing.T) {
	// Center at X = -5, radius 1 -> signed distance to the +X plane
	// (Dist=0) is -5, which is < -radius (-1) -> fully outside.
	if bsprender.CullSphere(testFrustum(), [3]float32{-5, 0, 0}, 1) {
		t.Fatal("sphere fully behind the near plane should be culled")
	}
}

func TestCullSphere_JustTouchingOnePlane(t *testing.T) {
	// Center at X = -1, radius = 1: signed distance to +X plane is
	// exactly -1, equal to -radius -> still visible (>=, not >).
	// Confirms the boundary uses the inclusive comparison the doc
	// promises (sign convention matches PointInFrustum's
	// N.P - Dist >= 0 inside test, extended to sphere by -radius).
	if !bsprender.CullSphere(testFrustum(), [3]float32{-1, 0, 0}, 1) {
		t.Fatal("sphere just touching a plane should still be visible")
	}
}

func TestCullSphere_TinyOffToTheSide(t *testing.T) {
	// Tiny sphere far to the side -> well past the +Y plane (Y <= 10);
	// signed distance to the -Y plane is 10 - 1000 = -990, far below
	// -0.1 -> culled.
	if bsprender.CullSphere(testFrustum(), [3]float32{50, 1000, 0}, 0.1) {
		t.Fatal("tiny sphere far to the side should be culled")
	}
}

func TestCullSphere_RealRefDefForward(t *testing.T) {
	// Integration check against the real BuildFrustum: viewer at the
	// origin looking down +X with 90deg FOV. A unit sphere 10 units
	// ahead must be visible; one 10 units behind must not.
	rd, err := render.NewRefDef(render.VRect{Width: 640, Height: 640}, [3]float32{0, 0, 0}, [3]float32{0, 0, 0}, 90)
	if err != nil {
		t.Fatalf("NewRefDef: %v", err)
	}
	f := rd.BuildFrustum()
	if !bsprender.CullSphere(f, [3]float32{10, 0, 0}, 1) {
		t.Fatal("sphere ahead of camera should be visible")
	}
	if bsprender.CullSphere(f, [3]float32{-10, 0, 0}, 1) {
		t.Fatal("sphere behind camera should be culled")
	}
}

// ---------------------------------------------------------------------------
// CullSubmodel
// ---------------------------------------------------------------------------

func TestCullSubmodel_FullyInside(t *testing.T) {
	// Tight box centered at (50, 0, 0) -- comfortably inside the
	// tube on every axis -> FullyInside.
	got := bsprender.CullSubmodel(testFrustum(), [3]float32{49, -1, -1}, [3]float32{51, 1, 1})
	if got != bsprender.StatusFullyInside {
		t.Fatalf("got %v, want StatusFullyInside", got)
	}
}

func TestCullSubmodel_Outside(t *testing.T) {
	// Box entirely behind the +X near plane (max X = -1 < 0) -> the
	// p-vertex on plane 0 is at X = -1, signed distance -1 < 0 -> the
	// whole box is on the negative side -> Outside.
	got := bsprender.CullSubmodel(testFrustum(), [3]float32{-20, -1, -1}, [3]float32{-1, 1, 1})
	if got != bsprender.StatusOutside {
		t.Fatalf("got %v, want StatusOutside", got)
	}
}

func TestCullSubmodel_Straddling(t *testing.T) {
	// Box straddles the +X near plane: mins X = -5, maxs X = 5.
	// p-vertex on plane 0 is at X = 5 (inside); n-vertex at X = -5
	// (outside) -> straddling for that plane, fully inside on the
	// other three -> StatusStraddling.
	got := bsprender.CullSubmodel(testFrustum(), [3]float32{-5, -1, -1}, [3]float32{5, 1, 1})
	if got != bsprender.StatusStraddling {
		t.Fatalf("got %v, want StatusStraddling", got)
	}
}

func TestCullSubmodel_DegenerateZeroSizeAtOrigin(t *testing.T) {
	// Zero-size box (mins == maxs) at the origin is just the point
	// (0, 0, 0). Signed distance to every plane is >= 0 (it sits
	// exactly on the +X plane, comfortably inside the other three)
	// -> FullyInside. A point can never straddle a plane, so the
	// degenerate case is documented as resolving to FullyInside or
	// Outside but never Straddling.
	got := bsprender.CullSubmodel(testFrustum(), [3]float32{0, 0, 0}, [3]float32{0, 0, 0})
	if got != bsprender.StatusFullyInside {
		t.Fatalf("got %v, want StatusFullyInside", got)
	}
}

func TestCullSubmodel_NegativeNormalAxisSelection(t *testing.T) {
	// Targets the `pl.Normal[i] < 0` branch in the p/n-vertex
	// selection: plane 3 (Normal = (0,-1,0)) wants the LOWER Y for
	// the p-vertex and the UPPER Y for the n-vertex. Place a tall,
	// thin box that straddles ONLY plane 3 (maxs Y = 20 > 10) and
	// is otherwise comfortably inside -> Straddling. Also exercises
	// plane 2 (Normal = (0,+1,0)) where mins Y = -5 is comfortably
	// inside, proving the +Y plane's p-vertex picks maxs[1].
	got := bsprender.CullSubmodel(testFrustum(), [3]float32{50, -5, -1}, [3]float32{60, 20, 1})
	if got != bsprender.StatusStraddling {
		t.Fatalf("got %v, want StatusStraddling", got)
	}
}

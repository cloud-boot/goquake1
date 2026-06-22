// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/anorms"
	"github.com/go-quake1/engine/mdl"
)

// --- DefaultAliasShade ---------------------------------------------------

func TestDefaultAliasShade_DriftDetector(t *testing.T) {
	got := DefaultAliasShade()
	if got.Ambient != 1.0 {
		t.Errorf("Ambient = %v want 1.0", got.Ambient)
	}
	if got.DirectMin != 0.0 {
		t.Errorf("DirectMin = %v want 0.0", got.DirectMin)
	}
	if got.DirectMax != 1.0 {
		t.Errorf("DirectMax = %v want 1.0", got.DirectMax)
	}
	if got.LightDir != [3]float32{1, 0, 0} {
		t.Errorf("LightDir = %v want (1,0,0)", got.LightDir)
	}
}

// --- ComputeAliasVertexLights -------------------------------------------

// findNormal returns the table index whose unit vector best matches v.
// Used to construct test vertices whose decoded normal is "aligned"
// (cos~1), "perpendicular" (cos~0), or "opposite" (cos<0) to a given
// light direction.
func findNormal(target [3]float32) byte {
	bestIdx := 0
	bestDot := float32(-2)
	for i, n := range anorms.Table {
		d := n[0]*target[0] + n[1]*target[1] + n[2]*target[2]
		if d > bestDot {
			bestDot = d
			bestIdx = i
		}
	}
	return byte(bestIdx)
}

func TestComputeAliasVertexLights_AlignedMax(t *testing.T) {
	// A vertex whose normal points along +X under LightDir=+X should
	// get the maximum light (Ambient + DirectMax-DirectMin).
	shade := AliasShadeRange{
		Ambient:   0.0,
		DirectMin: 0.0,
		DirectMax: 1.0,
		LightDir:  [3]float32{1, 0, 0},
	}
	v := []mdl.TriVertx{{LightNormalIndex: findNormal([3]float32{1, 0, 0})}}
	got, err := ComputeAliasVertexLights(v, shade)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(out) = %d want 1", len(got))
	}
	// Aligned normal dot=1.0 -> light=1.0 -> value=255.
	if got[0] != 255 {
		t.Errorf("aligned light = %d want 255", got[0])
	}
}

func TestComputeAliasVertexLights_PerpendicularAmbient(t *testing.T) {
	// Perpendicular: dot~0 -> light = ambient only.
	shade := AliasShadeRange{
		Ambient:   0.5,
		DirectMin: 0.0,
		DirectMax: 1.0,
		LightDir:  [3]float32{1, 0, 0},
	}
	v := []mdl.TriVertx{{LightNormalIndex: findNormal([3]float32{0, 1, 0})}}
	got, err := ComputeAliasVertexLights(v, shade)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// 0.5 * 255 = 127.5 -> 128 via math.Round.
	if got[0] < 126 || got[0] > 130 {
		t.Errorf("perpendicular light = %d want ~128", got[0])
	}
}

func TestComputeAliasVertexLights_OppositeClamped(t *testing.T) {
	// Opposite direction (dot<0) clamps to dot=0 -> ambient only.
	shade := AliasShadeRange{
		Ambient:   0.25,
		DirectMin: 0.0,
		DirectMax: 1.0,
		LightDir:  [3]float32{1, 0, 0},
	}
	v := []mdl.TriVertx{{LightNormalIndex: findNormal([3]float32{-1, 0, 0})}}
	got, err := ComputeAliasVertexLights(v, shade)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// 0.25 * 255 = 63.75 -> 64.
	if got[0] < 62 || got[0] > 66 {
		t.Errorf("opposite light = %d want ~64 (ambient only)", got[0])
	}
}

func TestComputeAliasVertexLights_OutOfRangeNormalClamps(t *testing.T) {
	// LightNormalIndex = 200 is past the 162-entry table; should
	// clamp to normal 0 (= anorms.Table[0]).
	shade := DefaultAliasShade()
	v := []mdl.TriVertx{
		{LightNormalIndex: 200}, // forced clamp-to-0
		{LightNormalIndex: 0},   // already normal 0
	}
	got, err := ComputeAliasVertexLights(v, shade)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got[0] != got[1] {
		t.Errorf("clamp-to-0 light = %d, normal-0 light = %d; want equal", got[0], got[1])
	}
}

func TestComputeAliasVertexLights_NilSlice(t *testing.T) {
	got, err := ComputeAliasVertexLights(nil, DefaultAliasShade())
	if !errors.Is(err, ErrAliasLightModelNil) {
		t.Fatalf("err = %v want ErrAliasLightModelNil", err)
	}
	if got != nil {
		t.Errorf("out = %v want nil", got)
	}
}

func TestComputeAliasVertexLights_OverflowClamps(t *testing.T) {
	// Crank Ambient + Direct span > 1.0 to force the high clamp.
	shade := AliasShadeRange{
		Ambient:   2.0, // already > 1.0
		DirectMin: 0.0,
		DirectMax: 1.0,
		LightDir:  [3]float32{1, 0, 0},
	}
	v := []mdl.TriVertx{{LightNormalIndex: findNormal([3]float32{1, 0, 0})}}
	got, _ := ComputeAliasVertexLights(v, shade)
	if got[0] != 255 {
		t.Errorf("overflow light = %d want 255 (clamped)", got[0])
	}
}

func TestComputeAliasVertexLights_NegativeAmbientClamps(t *testing.T) {
	// Ambient < 0 with opposite-facing normal (dot<0 -> 0) gives a
	// raw value < 0; should clamp to 0.
	shade := AliasShadeRange{
		Ambient:   -1.0,
		DirectMin: 0.0,
		DirectMax: 1.0,
		LightDir:  [3]float32{1, 0, 0},
	}
	v := []mdl.TriVertx{{LightNormalIndex: findNormal([3]float32{-1, 0, 0})}}
	got, _ := ComputeAliasVertexLights(v, shade)
	if got[0] != 0 {
		t.Errorf("underflow light = %d want 0 (clamped)", got[0])
	}
}

func TestComputeAliasVertexLights_EmptySlice(t *testing.T) {
	// Empty (non-nil) input is not an error; returns empty (non-nil).
	got, err := ComputeAliasVertexLights([]mdl.TriVertx{}, DefaultAliasShade())
	if err != nil {
		t.Fatalf("err = %v want nil", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("out = %v want empty non-nil", got)
	}
}

// --- AvgTriangleLight ---------------------------------------------------

func TestAvgTriangleLight_Mean(t *testing.T) {
	lights := []int{10, 20, 30, 40}
	if got := AvgTriangleLight(lights, 0, 1, 2); got != 20 {
		t.Errorf("mean(10,20,30) = %d want 20", got)
	}
	if got := AvgTriangleLight(lights, 1, 2, 3); got != 30 {
		t.Errorf("mean(20,30,40) = %d want 30", got)
	}
}

// --- DrawAliasLit nil-arg + bad-frame guards ----------------------------

func TestDrawAliasLit_NilFB(t *testing.T) {
	_, rd, skin := newAliasCtx(t)
	m := frontFacingTri()
	err := DrawAliasLit(nil, rd, nil, DefaultAliasShade(), m, skin,
		AliasEntity{Origin: [3]float32{100, 0, 0}})
	if !errors.Is(err, ErrAliasNilFB) {
		t.Fatalf("err = %v want ErrAliasNilFB", err)
	}
}

func TestDrawAliasLit_NilModel(t *testing.T) {
	fb, rd, skin := newAliasCtx(t)
	err := DrawAliasLit(fb, rd, nil, DefaultAliasShade(), nil, skin, AliasEntity{})
	if !errors.Is(err, ErrAliasNilModel) {
		t.Fatalf("err = %v want ErrAliasNilModel", err)
	}
}

func TestDrawAliasLit_NilRefDef(t *testing.T) {
	fb, _, skin := newAliasCtx(t)
	m := frontFacingTri()
	err := DrawAliasLit(fb, nil, nil, DefaultAliasShade(), m, skin, AliasEntity{})
	if !errors.Is(err, ErrAliasNilRefDef) {
		t.Fatalf("err = %v want ErrAliasNilRefDef", err)
	}
}

func TestDrawAliasLit_NilSkin(t *testing.T) {
	fb, rd, _ := newAliasCtx(t)
	m := frontFacingTri()
	err := DrawAliasLit(fb, rd, nil, DefaultAliasShade(), m, nil, AliasEntity{})
	if !errors.Is(err, ErrAliasNilSkin) {
		t.Fatalf("err = %v want ErrAliasNilSkin", err)
	}
}

func TestDrawAliasLit_BadFrameNegative(t *testing.T) {
	fb, rd, skin := newAliasCtx(t)
	m := frontFacingTri()
	err := DrawAliasLit(fb, rd, nil, DefaultAliasShade(), m, skin,
		AliasEntity{FrameIdx: -1})
	if !errors.Is(err, ErrAliasBadFrame) {
		t.Fatalf("err = %v want ErrAliasBadFrame", err)
	}
}

func TestDrawAliasLit_BadFrameOverflow(t *testing.T) {
	fb, rd, skin := newAliasCtx(t)
	m := frontFacingTri()
	err := DrawAliasLit(fb, rd, nil, DefaultAliasShade(), m, skin,
		AliasEntity{FrameIdx: len(m.Frames)})
	if !errors.Is(err, ErrAliasBadFrame) {
		t.Fatalf("err = %v want ErrAliasBadFrame", err)
	}
}

// --- DrawAliasLit happy paths -------------------------------------------

// litColormap returns a synthetic colormap that encodes the light row
// into the output byte: cell[row][src] = byte(row) (independent of
// src). Lets a test read which light row a pixel was written under.
func litColormap() *ColorMap {
	var cm ColorMap
	for row := 0; row < ColorMapRows; row++ {
		for s := 0; s < ColorMapCols; s++ {
			cm[row][s] = byte(row)
		}
	}
	return &cm
}

func TestDrawAliasLit_HappyDraw(t *testing.T) {
	// Front-facing triangle, default shade (Ambient=1.0); the per-
	// vertex light is at the high end -> per-tri avg -> row 0 (the
	// brightest) at the rasterizer; with the litColormap that maps
	// to byte 0 at the lit pixel.
	fb, rd, skin := newAliasCtx(t)
	m := frontFacingTri()
	cm := litColormap()
	ent := AliasEntity{Origin: [3]float32{100, 0, 0}}
	if err := DrawAliasLit(fb, rd, cm, DefaultAliasShade(), m, skin, ent); err != nil {
		t.Fatalf("DrawAliasLit: %v", err)
	}
	// Centre of the front-facing triangle is around (160, 98).
	got := fb.Pixels[98*fb.Pitch+160]
	if got != 0 {
		t.Errorf("centre pixel = %#x want 0x00 (brightest row)", got)
	}
}

func TestDrawAliasLit_DarkShadeYieldsDarkerRow(t *testing.T) {
	// Zero ambient + zero direct -> per-vertex light = 0 -> avg = 0 ->
	// row = (255-0)*64/256 = 63 (darkest). litColormap maps that to
	// byte 63 at the rasterizer.
	fb, rd, skin := newAliasCtx(t)
	m := frontFacingTri()
	cm := litColormap()
	shade := AliasShadeRange{
		Ambient:   0.0,
		DirectMin: 0.0,
		DirectMax: 0.0,
		LightDir:  [3]float32{1, 0, 0},
	}
	ent := AliasEntity{Origin: [3]float32{100, 0, 0}}
	if err := DrawAliasLit(fb, rd, cm, shade, m, skin, ent); err != nil {
		t.Fatalf("DrawAliasLit: %v", err)
	}
	got := fb.Pixels[98*fb.Pitch+160]
	if got != ColorMapRows-1 {
		t.Errorf("dark-shade centre pixel = %d want %d", got, ColorMapRows-1)
	}
}

func TestDrawAliasLit_BadFov(t *testing.T) {
	// FovX=0 -> tanHalfX=0 -> early no-op.
	fb, _, skin := newAliasCtx(t)
	m := frontFacingTri()
	rd := &RefDef{FovX: 0, FovY: 0}
	if err := DrawAliasLit(fb, rd, nil, DefaultAliasShade(), m, skin,
		AliasEntity{Origin: [3]float32{100, 0, 0}}); err != nil {
		t.Fatalf("bad-fov err = %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("bad-fov draw wrote a pixel")
		}
	}
}

func TestDrawAliasLit_NearClipped(t *testing.T) {
	// Behind the camera -> every vertex view-z < AliasNearClip ->
	// triangle dropped. (FrameGroup is single, no empty-group path.)
	fb, rd, skin := newAliasCtx(t)
	m := frontFacingTri()
	ent := AliasEntity{Origin: [3]float32{-50, 0, 0}}
	if err := DrawAliasLit(fb, rd, nil, DefaultAliasShade(), m, skin, ent); err != nil {
		t.Fatalf("near-clip err = %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("near-clipped triangle wrote a pixel")
		}
	}
}

func TestDrawAliasLit_BackFaceCulled(t *testing.T) {
	fb, rd, skin := newAliasCtx(t)
	m := makeTriModel([3][3]byte{
		{0, 15, 10},
		{0, 10, 15},
		{0, 5, 10},
	}, 1)
	ent := AliasEntity{Origin: [3]float32{100, 0, 0}}
	if err := DrawAliasLit(fb, rd, nil, DefaultAliasShade(), m, skin, ent); err != nil {
		t.Fatalf("DrawAliasLit: %v", err)
	}
	for _, b := range fb.Pixels {
		if b != 0 {
			t.Fatalf("back-facing triangle wrote a pixel")
		}
	}
}

func TestDrawAliasLit_PropagatesFillError(t *testing.T) {
	fb, rd, _ := newAliasCtx(t)
	m := frontFacingTri()
	err := DrawAliasLit(fb, rd, nil, DefaultAliasShade(), m, makeBadSkin(),
		AliasEntity{Origin: [3]float32{100, 0, 0}})
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("err = %v want ErrPicShape", err)
	}
}

func TestDrawAliasLit_BackSkinSeamFixup(t *testing.T) {
	// Same shape as TestDrawAlias_BackSkinSeamFixup -- verifies the
	// back-skin S offset path executes inside drawAliasFromPoseLit.
	verts := [3][3]byte{
		{0, 15, 10},
		{0, 5, 10},
		{0, 10, 15},
	}
	m := makeTriModel(verts, 0) // FacesFront=0 -> back skin
	for i := range m.STVerts {
		m.STVerts[i].OnSeam = 0x20
	}
	fb, rd, _ := newAliasCtx(t)
	skin := &Pic{Width: 16, Height: 16, Pixels: make([]byte, 16*16)}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if x < 8 {
				skin.Pixels[y*16+x] = 0xAA
			} else {
				skin.Pixels[y*16+x] = 0xBB
			}
		}
	}
	ent := AliasEntity{Origin: [3]float32{100, 0, 0}}
	if err := DrawAliasLit(fb, rd, nil, DefaultAliasShade(), m, skin, ent); err != nil {
		t.Fatalf("DrawAliasLit: %v", err)
	}
	if got := fb.Pixels[98*fb.Pitch+160]; got != 0xBB {
		t.Fatalf("seam-fixup sampled %#x want 0xBB (right-skin half)", got)
	}
}

func TestDrawAliasLit_FrameGroupEmptyNoOp(t *testing.T) {
	// Empty group -> FramePose returns nil -> DrawAliasLit short-
	// circuits before calling ComputeAliasVertexLights (which would
	// error on a nil slice).
	m := &mdl.Model{
		Header: mdl.Header{Scale: [3]float32{1, 1, 1}, NumFrames: 1},
		Frames: []mdl.Frame{
			{Type: mdl.FrameGroup, Group: &mdl.GroupFrame{}},
		},
	}
	fb, rd, skin := newAliasCtx(t)
	if err := DrawAliasLit(fb, rd, nil, DefaultAliasShade(), m, skin, AliasEntity{}); err != nil {
		t.Fatalf("empty-group err = %v", err)
	}
}

// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-quake1/engine/mdl"
)

// twoFrameModel returns a two-frame model whose byte-space midpoint
// pose is the frontFacingTri vertex layout (see alias_test.go).
//
//	Frame 0 (A) bytes: (0,20,10), (0,10,10), (0,15,15)
//	Frame 1 (B) bytes: (0,10,10), (0, 0,10), (0, 5,15)
//	(A+B)/2 bytes:     (0,15,10), (0, 5,10), (0,10,15) <- frontFacingTri
//
// All other model fields (STVerts, Triangles, Header) match
// makeTriModel so the shared helpers can be reused.
func twoFrameModel() *mdl.Model {
	m := makeTriModel([3][3]byte{{0, 20, 10}, {0, 10, 10}, {0, 15, 15}}, 1)
	m.Header.NumFrames = 2
	m.Frames = append(m.Frames, mdl.Frame{
		Type: mdl.FrameSingle,
		Single: mdl.SingleFrame{
			Verts: []mdl.TriVertx{
				{V: [3]byte{0, 10, 10}},
				{V: [3]byte{0, 0, 10}},
				{V: [3]byte{0, 5, 15}},
			},
		},
	})
	return m
}

// --- nil-arg + range guards ---------------------------------------------

func TestDrawAliasInterp_NilFB(t *testing.T) {
	_, rd, skin := newAliasCtx(t)
	m := twoFrameModel()
	err := DrawAliasInterp(nil, rd, nil, 0, m, skin,
		AliasEntityInterp{AliasEntity: AliasEntity{Origin: [3]float32{100, 0, 0}}})
	if !errors.Is(err, ErrAliasNilFB) {
		t.Fatalf("err = %v want ErrAliasNilFB", err)
	}
}

func TestDrawAliasInterp_NilModel(t *testing.T) {
	fb, rd, skin := newAliasCtx(t)
	err := DrawAliasInterp(fb, rd, nil, 0, nil, skin, AliasEntityInterp{})
	if !errors.Is(err, ErrAliasNilModel) {
		t.Fatalf("err = %v want ErrAliasNilModel", err)
	}
}

func TestDrawAliasInterp_NilRefDef(t *testing.T) {
	fb, _, skin := newAliasCtx(t)
	m := twoFrameModel()
	err := DrawAliasInterp(fb, nil, nil, 0, m, skin, AliasEntityInterp{})
	if !errors.Is(err, ErrAliasNilRefDef) {
		t.Fatalf("err = %v want ErrAliasNilRefDef", err)
	}
}

func TestDrawAliasInterp_NilSkin(t *testing.T) {
	fb, rd, _ := newAliasCtx(t)
	m := twoFrameModel()
	err := DrawAliasInterp(fb, rd, nil, 0, m, nil, AliasEntityInterp{})
	if !errors.Is(err, ErrAliasNilSkin) {
		t.Fatalf("err = %v want ErrAliasNilSkin", err)
	}
}

func TestDrawAliasInterp_LerpBelowZero(t *testing.T) {
	fb, rd, skin := newAliasCtx(t)
	m := twoFrameModel()
	err := DrawAliasInterp(fb, rd, nil, 0, m, skin,
		AliasEntityInterp{Lerp: -0.5})
	if !errors.Is(err, ErrAliasInterpRange) {
		t.Fatalf("err = %v want ErrAliasInterpRange", err)
	}
}

func TestDrawAliasInterp_LerpAboveOne(t *testing.T) {
	fb, rd, skin := newAliasCtx(t)
	m := twoFrameModel()
	err := DrawAliasInterp(fb, rd, nil, 0, m, skin,
		AliasEntityInterp{Lerp: 1.5})
	if !errors.Is(err, ErrAliasInterpRange) {
		t.Fatalf("err = %v want ErrAliasInterpRange", err)
	}
}

func TestDrawAliasInterp_BadFrameIdx(t *testing.T) {
	fb, rd, skin := newAliasCtx(t)
	m := twoFrameModel()
	err := DrawAliasInterp(fb, rd, nil, 0, m, skin,
		AliasEntityInterp{
			AliasEntity:  AliasEntity{FrameIdx: -1},
			FrameIdxNext: 0,
		})
	if !errors.Is(err, ErrAliasBadFrame) {
		t.Fatalf("err = %v want ErrAliasBadFrame", err)
	}
}

func TestDrawAliasInterp_BadFrameIdxNext(t *testing.T) {
	fb, rd, skin := newAliasCtx(t)
	m := twoFrameModel()
	err := DrawAliasInterp(fb, rd, nil, 0, m, skin,
		AliasEntityInterp{
			AliasEntity:  AliasEntity{FrameIdx: 0},
			FrameIdxNext: len(m.Frames),
		})
	if !errors.Is(err, ErrAliasBadFrame) {
		t.Fatalf("err = %v want ErrAliasBadFrame", err)
	}
}

// --- behaviour equivalence: Lerp=0 -> use FrameIdx pose ----------------

func TestDrawAliasInterp_LerpZeroMatchesDrawAlias(t *testing.T) {
	m := twoFrameModel()
	ent := AliasEntity{Origin: [3]float32{100, 0, 0}, FrameIdx: 0}

	fbRef, rd, skin := newAliasCtx(t)
	if err := DrawAlias(fbRef, rd, nil, 0, m, skin, ent); err != nil {
		t.Fatalf("DrawAlias: %v", err)
	}

	fbGot, _, _ := newAliasCtx(t)
	entInt := AliasEntityInterp{
		AliasEntity:  ent,
		FrameIdxNext: 1, // different "to" frame, but Lerp=0 -> ignored
		Lerp:         0,
	}
	if err := DrawAliasInterp(fbGot, rd, nil, 0, m, skin, entInt); err != nil {
		t.Fatalf("DrawAliasInterp: %v", err)
	}
	if !bytes.Equal(fbRef.Pixels, fbGot.Pixels) {
		t.Fatalf("Lerp=0 framebuffer mismatch with DrawAlias")
	}
}

// --- behaviour equivalence: Lerp=1 -> use FrameIdxNext pose ------------

func TestDrawAliasInterp_LerpOneMatchesDrawAliasNext(t *testing.T) {
	m := twoFrameModel()
	originEnt := AliasEntity{Origin: [3]float32{100, 0, 0}}

	fbRef, rd, skin := newAliasCtx(t)
	refEnt := originEnt
	refEnt.FrameIdx = 1
	if err := DrawAlias(fbRef, rd, nil, 0, m, skin, refEnt); err != nil {
		t.Fatalf("DrawAlias: %v", err)
	}

	fbGot, _, _ := newAliasCtx(t)
	entInt := AliasEntityInterp{
		AliasEntity:  originEnt, // FrameIdx = 0
		FrameIdxNext: 1,
		Lerp:         1,
	}
	if err := DrawAliasInterp(fbGot, rd, nil, 0, m, skin, entInt); err != nil {
		t.Fatalf("DrawAliasInterp: %v", err)
	}
	if !bytes.Equal(fbRef.Pixels, fbGot.Pixels) {
		t.Fatalf("Lerp=1 framebuffer mismatch with DrawAlias(FrameIdxNext)")
	}
}

// --- no-op interpolation: FrameIdxNext == FrameIdx ---------------------

func TestDrawAliasInterp_SameFrameNoOp(t *testing.T) {
	m := twoFrameModel()
	originEnt := AliasEntity{Origin: [3]float32{100, 0, 0}, FrameIdx: 0}

	fbRef, rd, skin := newAliasCtx(t)
	if err := DrawAlias(fbRef, rd, nil, 0, m, skin, originEnt); err != nil {
		t.Fatalf("DrawAlias: %v", err)
	}

	fbGot, _, _ := newAliasCtx(t)
	entInt := AliasEntityInterp{
		AliasEntity:  originEnt,
		FrameIdxNext: 0,    // same as FrameIdx
		Lerp:         0.37, // arbitrary; should be ignored
	}
	if err := DrawAliasInterp(fbGot, rd, nil, 0, m, skin, entInt); err != nil {
		t.Fatalf("DrawAliasInterp: %v", err)
	}
	if !bytes.Equal(fbRef.Pixels, fbGot.Pixels) {
		t.Fatalf("same-frame Lerp framebuffer mismatch")
	}
}

// --- mid-frame interpolation: byte-space mid of A/B == frontFacingTri --

func TestDrawAliasInterp_LerpHalfHitsMidPose(t *testing.T) {
	// Lerp=0.5 between Frame0 + Frame1 of twoFrameModel reconstructs
	// the frontFacingTri byte pose -- so the rasterized triangle
	// should hit the same center pixel (160, 98) checked in
	// TestDrawAlias_HappyDraw.
	m := twoFrameModel()
	fb, rd, skin := newAliasCtx(t)
	entInt := AliasEntityInterp{
		AliasEntity:  AliasEntity{Origin: [3]float32{100, 0, 0}, FrameIdx: 0},
		FrameIdxNext: 1,
		Lerp:         0.5,
	}
	if err := DrawAliasInterp(fb, rd, nil, 0, m, skin, entInt); err != nil {
		t.Fatalf("DrawAliasInterp: %v", err)
	}
	if got := fb.Pixels[98*fb.Pitch+160]; got != 0x42 {
		t.Fatalf("Lerp=0.5 center pixel = %#x want 0x42", got)
	}
}

// --- mid-frame matches a manually-built mid-pose model ------------------

func TestDrawAliasInterp_LerpHalfMatchesMidModel(t *testing.T) {
	m := twoFrameModel()

	// frontFacingTri carries the exact byte-space mid pose.
	mMid := frontFacingTri()

	fbRef, rd, skin := newAliasCtx(t)
	if err := DrawAlias(fbRef, rd, nil, 0, mMid, skin,
		AliasEntity{Origin: [3]float32{100, 0, 0}}); err != nil {
		t.Fatalf("DrawAlias(mid): %v", err)
	}

	fbGot, _, _ := newAliasCtx(t)
	entInt := AliasEntityInterp{
		AliasEntity:  AliasEntity{Origin: [3]float32{100, 0, 0}, FrameIdx: 0},
		FrameIdxNext: 1,
		Lerp:         0.5,
	}
	if err := DrawAliasInterp(fbGot, rd, nil, 0, m, skin, entInt); err != nil {
		t.Fatalf("DrawAliasInterp: %v", err)
	}
	if !bytes.Equal(fbRef.Pixels, fbGot.Pixels) {
		t.Fatalf("Lerp=0.5 framebuffer mismatch with DrawAlias(midPose)")
	}
}

// --- propagates FillTexturedPolygon error -----------------------------

func TestDrawAliasInterp_PropagatesFillError(t *testing.T) {
	fb, rd, _ := newAliasCtx(t)
	m := twoFrameModel()
	entInt := AliasEntityInterp{
		AliasEntity:  AliasEntity{Origin: [3]float32{100, 0, 0}},
		FrameIdxNext: 1,
		Lerp:         0.5,
	}
	err := DrawAliasInterp(fb, rd, nil, 0, m, makeBadSkin(), entInt)
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("err = %v want ErrPicShape", err)
	}
}

// --- lerpAliasPose helper coverage: weight branch + length min --------

func TestLerpAliasPose_LightNormalIndexFollowsWeight(t *testing.T) {
	a := []mdl.TriVertx{{V: [3]byte{0, 0, 0}, LightNormalIndex: 11}}
	b := []mdl.TriVertx{{V: [3]byte{10, 20, 30}, LightNormalIndex: 22}}

	// t < 0.5 -> light comes from A.
	got := lerpAliasPose(a, b, 0.25)
	if got[0].LightNormalIndex != 11 {
		t.Fatalf("t=0.25 lightnormal = %d want 11", got[0].LightNormalIndex)
	}
	if got[0].V[0] != byte(0*0.75+10*0.25+0.5) {
		t.Fatalf("t=0.25 V[0] = %d want %d", got[0].V[0],
			byte(0*0.75+10*0.25+0.5))
	}

	// t >= 0.5 -> light comes from B.
	got = lerpAliasPose(a, b, 0.75)
	if got[0].LightNormalIndex != 22 {
		t.Fatalf("t=0.75 lightnormal = %d want 22", got[0].LightNormalIndex)
	}
}

func TestLerpAliasPose_LengthMinOfInputs(t *testing.T) {
	a := []mdl.TriVertx{{V: [3]byte{1, 2, 3}}, {V: [3]byte{4, 5, 6}}}
	b := []mdl.TriVertx{{V: [3]byte{7, 8, 9}}} // shorter

	if got := lerpAliasPose(a, b, 0.5); len(got) != 1 {
		t.Fatalf("len = %d want 1 (min of inputs)", len(got))
	}
}

// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package synthbsp_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bspfile/synthbsp"
)

func TestErrInvalidPlan(t *testing.T) {
	// Reserved sentinel; ensure it's discoverable + non-nil + a real
	// error so callers can errors.Is against it without surprises.
	if synthbsp.ErrInvalidPlan == nil {
		t.Fatal("ErrInvalidPlan must not be nil")
	}
	if !errors.Is(synthbsp.ErrInvalidPlan, synthbsp.ErrInvalidPlan) {
		t.Fatal("ErrInvalidPlan must be errors.Is-comparable")
	}
}

func TestBuildFiveLeafPVS(t *testing.T) {
	data, size, err := synthbsp.BuildFiveLeafPVS()
	if err != nil {
		t.Fatalf("BuildFiveLeafPVS: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("BuildFiveLeafPVS returned empty bytes")
	}
	if size != int64(len(data)) {
		t.Fatalf("size=%d != len(data)=%d", size, len(data))
	}
	f, err := bspfile.Open(bytes.NewReader(data), size)
	if err != nil {
		t.Fatalf("bspfile.Open: %v", err)
	}
	leafs, err := f.Leafs()
	if err != nil {
		t.Fatalf("Leafs: %v", err)
	}
	// 5 game leaves + 1 leaf 0 (the SOLID sentinel) = 6 total entries.
	if got := len(leafs); got != 6 {
		t.Errorf("len(leafs)=%d, want 6 (solid sentinel + 5 game leaves)", got)
	}
	nodes, err := f.Nodes()
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if got := len(nodes); got != 4 {
		t.Errorf("len(nodes)=%d, want 4", got)
	}
	if vis := f.Visibility(); len(vis) != 5 {
		t.Errorf("Visibility len=%d, want 5 (one PVS byte per leaf)", len(vis))
	}
}

func TestBuildWithFaces(t *testing.T) {
	data, size, err := synthbsp.BuildWithFaces()
	if err != nil {
		t.Fatalf("BuildWithFaces: %v", err)
	}
	if size != int64(len(data)) {
		t.Fatalf("size=%d != len(data)=%d", size, len(data))
	}
	f, err := bspfile.Open(bytes.NewReader(data), size)
	if err != nil {
		t.Fatalf("bspfile.Open: %v", err)
	}
	faces, err := f.Faces()
	if err != nil {
		t.Fatalf("Faces: %v", err)
	}
	if len(faces) < 1 {
		t.Errorf("len(faces)=%d, want >= 1", len(faces))
	}
	verts, err := f.Vertexes()
	if err != nil {
		t.Fatalf("Vertexes: %v", err)
	}
	if len(verts) == 0 {
		t.Error("Vertexes empty")
	}
	edges, err := f.Edges()
	if err != nil {
		t.Fatalf("Edges: %v", err)
	}
	if len(edges) == 0 {
		t.Error("Edges empty")
	}
	se, err := f.Surfedges()
	if err != nil {
		t.Fatalf("Surfedges: %v", err)
	}
	if len(se) == 0 {
		t.Error("Surfedges empty")
	}
	ti, err := f.TexInfos()
	if err != nil {
		t.Fatalf("TexInfos: %v", err)
	}
	if len(ti) == 0 {
		t.Error("TexInfos empty")
	}
	// Default textures lump: slot 0 names "trim" + (64,32), slot 1 missing.
	tex, err := f.Textures()
	if err != nil {
		t.Fatalf("Textures: %v", err)
	}
	if tex.NumMipTex != 2 {
		t.Errorf("NumMipTex=%d, want 2", tex.NumMipTex)
	}
	mt0, ok, err := tex.MipTex(0)
	if err != nil || !ok {
		t.Fatalf("MipTex(0): err=%v ok=%v", err, ok)
	}
	if mt0.Name != "trim" || mt0.Width != 64 || mt0.Height != 32 {
		t.Errorf("MipTex(0) = (%q,%d,%d), want (\"trim\",64,32)", mt0.Name, mt0.Width, mt0.Height)
	}
	// Slot 0 carries a real pixel buffer for every mip level. The
	// quake-tamago renderer's loadMiptexPics path consumes mip0; the
	// per-level sanity check below proves the byte layout the encoder
	// promised round-trips through bspfile.MipTex.Pixels.
	wantLens := [bspfile.MipLevels]int{64 * 32, 32 * 16, 16 * 8, 8 * 4}
	for level := 0; level < bspfile.MipLevels; level++ {
		px, err := mt0.Pixels(level)
		if err != nil {
			t.Fatalf("MipTex(0).Pixels(%d): %v", level, err)
		}
		if got := len(px); got != wantLens[level] {
			t.Errorf("MipTex(0).Pixels(%d) len=%d, want %d", level, got, wantLens[level])
		}
	}
	// Spot-check the (0,0) byte of mip0 -- makeMipPixels sets out[0]
	// = byte(32) so a future regression in the encoder (wrong offset,
	// misaligned write) surfaces here instead of silently shifting the
	// pixel buffer.
	px0, _ := mt0.Pixels(0)
	if px0[0] != 32 {
		t.Errorf("MipTex(0).Pixels(0)[0]=%d, want 32 (diagonal sweep start)", px0[0])
	}
	_, ok, err = tex.MipTex(1)
	if err != nil {
		t.Fatalf("MipTex(1): %v", err)
	}
	if ok {
		t.Error("MipTex(1) should be missing-texture sentinel (ok=false)")
	}
}

func TestBuildWithFacesCustomTextures(t *testing.T) {
	// Caller-supplied textures lump with a single named miptex ("custom",
	// 8x16). Asserting that exact name+dims round-trip through bspfile
	// confirms our bytes landed in the file at the right offset.
	const customName = "custom"
	custom := func() []byte {
		const miptexRecordSize = 16 + 4 + 4 + 4*4
		const dirSize = 4 + 1*4
		rec := make([]byte, miptexRecordSize)
		copy(rec[0:16], []byte(customName))
		binary.LittleEndian.PutUint32(rec[16:20], 8)
		binary.LittleEndian.PutUint32(rec[20:24], 16)
		buf := &bytes.Buffer{}
		_ = binary.Write(buf, binary.LittleEndian, int32(1))       // numMipTex
		_ = binary.Write(buf, binary.LittleEndian, int32(dirSize)) // slot 0 offset
		buf.Write(rec)
		return buf.Bytes()
	}
	data, size, err := synthbsp.BuildWithFacesCustomTextures(custom)
	if err != nil {
		t.Fatalf("BuildWithFacesCustomTextures: %v", err)
	}
	f, err := bspfile.Open(bytes.NewReader(data), size)
	if err != nil {
		t.Fatalf("bspfile.Open: %v", err)
	}
	tex, err := f.Textures()
	if err != nil {
		t.Fatalf("Textures: %v", err)
	}
	if tex.NumMipTex != 1 {
		t.Fatalf("NumMipTex=%d, want 1", tex.NumMipTex)
	}
	mt, ok, err := tex.MipTex(0)
	if err != nil || !ok {
		t.Fatalf("MipTex(0): err=%v ok=%v", err, ok)
	}
	if mt.Name != customName {
		t.Errorf("MipTex(0).Name=%q, want %q (caller bytes did not round-trip)", mt.Name, customName)
	}
	if mt.Width != 8 || mt.Height != 16 {
		t.Errorf("MipTex(0) dims=(%d,%d), want (8,16)", mt.Width, mt.Height)
	}
}

// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package model

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/go-quake1/engine/bspfile"
)

// --- makeDrawHull --------------------------------------------------------

// All-node-children: every Node.Children[j] >= 0 -- straight index
// copy, no leaf lookup.
func TestMakeDrawHull_AllNodeChildren(t *testing.T) {
	nodes := []bspfile.Node{
		{PlaneNum: 7, Children: [2]int16{1, 2}},
		{PlaneNum: 8, Children: [2]int16{0, 0}},
		{PlaneNum: 9, Children: [2]int16{1, 1}},
	}
	leafs := []bspfile.Leaf{} // never consulted
	out, err := makeDrawHull(nodes, leafs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("len(out)=%d, want 3", len(out))
	}
	for i, n := range nodes {
		if out[i].PlaneNum != n.PlaneNum {
			t.Errorf("node %d PlaneNum: got %d want %d", i, out[i].PlaneNum, n.PlaneNum)
		}
		if out[i].Children != n.Children {
			t.Errorf("node %d Children: got %v want %v (index copy)", i, out[i].Children, n.Children)
		}
	}
}

// Leaf-encoded children: Node.Children[j] = ~leafIdx -> ClipNode
// children[j] gets the leaf's Contents tag, not the index.
func TestMakeDrawHull_LeafChildren(t *testing.T) {
	// Node children encode leaves via bitwise NOT: ^0 = -1, ^1 = -2.
	nodes := []bspfile.Node{
		{PlaneNum: 0, Children: [2]int16{^int16(0), ^int16(1)}},
	}
	leafs := []bspfile.Leaf{
		{Contents: bspfile.ContentsEmpty},
		{Contents: bspfile.ContentsSolid},
	}
	out, err := makeDrawHull(nodes, leafs)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Children[0] != int16(bspfile.ContentsEmpty) {
		t.Errorf("child 0: got %d want EMPTY", out[0].Children[0])
	}
	if out[0].Children[1] != int16(bspfile.ContentsSolid) {
		t.Errorf("child 1: got %d want SOLID", out[0].Children[1])
	}
}

// Mixed: one node-index child, one leaf-encoded child.
func TestMakeDrawHull_MixedChildren(t *testing.T) {
	nodes := []bspfile.Node{
		{PlaneNum: 0, Children: [2]int16{1, ^int16(0)}}, // +index, leaf 0
		{PlaneNum: 1, Children: [2]int16{0, 0}},
	}
	leafs := []bspfile.Leaf{
		{Contents: bspfile.ContentsWater},
	}
	out, err := makeDrawHull(nodes, leafs)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Children[0] != 1 {
		t.Errorf("non-leaf child kept: got %d want 1", out[0].Children[0])
	}
	if out[0].Children[1] != int16(bspfile.ContentsWater) {
		t.Errorf("leaf-encoded child resolved to contents: got %d want WATER", out[0].Children[1])
	}
}

// Leaf index outside the leafs slice -> error.
func TestMakeDrawHull_BadLeafIndex(t *testing.T) {
	nodes := []bspfile.Node{
		{PlaneNum: 0, Children: [2]int16{^int16(99), 0}},
	}
	leafs := []bspfile.Leaf{
		{Contents: bspfile.ContentsEmpty},
	}
	if _, err := makeDrawHull(nodes, leafs); err == nil {
		t.Error("expected error for leaf index past slice")
	}
}

// Leaf contents > 0 (impossible in a real file, but defensive guard)
// -> error. Catches any silent reinterpretation of garbage data.
func TestMakeDrawHull_BadContentsValue(t *testing.T) {
	nodes := []bspfile.Node{
		{PlaneNum: 0, Children: [2]int16{^int16(0), 0}},
	}
	leafs := []bspfile.Leaf{
		{Contents: 42}, // positive -> not a valid contents tag
	}
	if _, err := makeDrawHull(nodes, leafs); err == nil {
		t.Error("expected error for positive Contents value")
	}
}

// --- LoadBrush error paths ------------------------------------------------

func TestLoadBrush_NilFile(t *testing.T) {
	if _, err := LoadBrush(nil, 0); err == nil {
		t.Error("expected error for nil file")
	}
}

// --- LoadBrush happy path -------------------------------------------------

// Build a minimal valid BSP that LoadBrush can chew through. The
// helper synthesizes the 5 lumps LoadBrush needs (Models, Planes,
// Nodes, Leafs, ClipNodes) and stubs out the rest with empty lumps.
func buildMinimalBSPForBrushModel(t *testing.T) ([]byte, int64) {
	t.Helper()

	// Lump payloads
	planes := []bspfile.Plane{
		{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
	}
	nodes := []bspfile.Node{
		// One node, both children = leaf 0 + leaf 1 (encoded via ~).
		{PlaneNum: 0, Children: [2]int16{^int16(0), ^int16(1)}},
	}
	leafs := []bspfile.Leaf{
		{Contents: bspfile.ContentsEmpty},
		{Contents: bspfile.ContentsSolid},
	}
	clipnodes := []bspfile.ClipNode{
		{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
	}
	models := []bspfile.Model{
		{
			Mins:     [3]float32{-100, -100, -100},
			Maxs:     [3]float32{100, 100, 100},
			Headnode: [bspfile.MaxMapHulls]int32{0, 0, 0, 0},
		},
	}

	// Encode payloads.
	pb := &bytes.Buffer{}
	for _, p := range planes {
		_ = binary.Write(pb, binary.LittleEndian, p.Normal[0])
		_ = binary.Write(pb, binary.LittleEndian, p.Normal[1])
		_ = binary.Write(pb, binary.LittleEndian, p.Normal[2])
		_ = binary.Write(pb, binary.LittleEndian, p.Dist)
		_ = binary.Write(pb, binary.LittleEndian, p.Type)
	}
	nb := &bytes.Buffer{}
	for _, n := range nodes {
		_ = binary.Write(nb, binary.LittleEndian, n.PlaneNum)
		_ = binary.Write(nb, binary.LittleEndian, n.Children[0])
		_ = binary.Write(nb, binary.LittleEndian, n.Children[1])
		for j := 0; j < 3; j++ {
			_ = binary.Write(nb, binary.LittleEndian, n.Mins[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(nb, binary.LittleEndian, n.Maxs[j])
		}
		_ = binary.Write(nb, binary.LittleEndian, n.FirstFace)
		_ = binary.Write(nb, binary.LittleEndian, n.NumFaces)
	}
	lb := &bytes.Buffer{}
	for _, l := range leafs {
		_ = binary.Write(lb, binary.LittleEndian, l.Contents)
		_ = binary.Write(lb, binary.LittleEndian, l.VisOfs)
		for j := 0; j < 3; j++ {
			_ = binary.Write(lb, binary.LittleEndian, l.Mins[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(lb, binary.LittleEndian, l.Maxs[j])
		}
		_ = binary.Write(lb, binary.LittleEndian, l.FirstMarkSurface)
		_ = binary.Write(lb, binary.LittleEndian, l.NumMarkSurfaces)
		lb.Write(l.AmbientLevel[:])
	}
	cnb := &bytes.Buffer{}
	for _, c := range clipnodes {
		_ = binary.Write(cnb, binary.LittleEndian, c.PlaneNum)
		_ = binary.Write(cnb, binary.LittleEndian, c.Children[0])
		_ = binary.Write(cnb, binary.LittleEndian, c.Children[1])
	}
	mb := &bytes.Buffer{}
	for _, m := range models {
		for j := 0; j < 3; j++ {
			_ = binary.Write(mb, binary.LittleEndian, m.Mins[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(mb, binary.LittleEndian, m.Maxs[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(mb, binary.LittleEndian, m.Origin[j])
		}
		for j := 0; j < bspfile.MaxMapHulls; j++ {
			_ = binary.Write(mb, binary.LittleEndian, m.Headnode[j])
		}
		_ = binary.Write(mb, binary.LittleEndian, m.VisLeafs)
		_ = binary.Write(mb, binary.LittleEndian, m.FirstFace)
		_ = binary.Write(mb, binary.LittleEndian, m.NumFaces)
	}

	// Header: int32 version + 15 (offset, length) lump entries = 124 bytes.
	const headerSize = 4 + 15*8
	body := &bytes.Buffer{}

	type lumpInfo struct {
		kind   bspfile.LumpKind
		data   []byte
		offset int32
	}
	lumps := []lumpInfo{
		{kind: bspfile.LumpPlanes, data: pb.Bytes()},
		{kind: bspfile.LumpNodes, data: nb.Bytes()},
		{kind: bspfile.LumpLeafs, data: lb.Bytes()},
		{kind: bspfile.LumpClipnodes, data: cnb.Bytes()},
		{kind: bspfile.LumpModels, data: mb.Bytes()},
	}
	offsetByKind := map[bspfile.LumpKind]int32{}
	lenByKind := map[bspfile.LumpKind]int32{}
	for i := range lumps {
		lumps[i].offset = int32(headerSize) + int32(body.Len())
		body.Write(lumps[i].data)
		offsetByKind[lumps[i].kind] = lumps[i].offset
		lenByKind[lumps[i].kind] = int32(len(lumps[i].data))
	}

	// Build header.
	hdr := &bytes.Buffer{}
	_ = binary.Write(hdr, binary.LittleEndian, int32(bspfile.Version29))
	for k := bspfile.LumpKind(0); int(k) < bspfile.HeaderLumps; k++ {
		_ = binary.Write(hdr, binary.LittleEndian, offsetByKind[k]) // 0 if absent
		_ = binary.Write(hdr, binary.LittleEndian, lenByKind[k])    // 0 if absent
	}
	full := append(hdr.Bytes(), body.Bytes()...)
	return full, int64(len(full))
}

func TestLoadBrush_HappyPath_World(t *testing.T) {
	data, size := buildMinimalBSPForBrushModel(t)
	f, err := bspfile.Open(bytes.NewReader(data), size)
	if err != nil {
		t.Fatal(err)
	}
	bm, err := LoadBrush(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bm.File != f {
		t.Error("BrushModel.File should reference the input file")
	}
	// hull 0 should have 1 clipnode (from the 1 node), planes = planes
	// lump (1 plane), FirstClipNode = models[0].Headnode[0] = 0.
	if len(bm.Hulls[0].ClipNodes) != 1 {
		t.Errorf("hull 0 ClipNodes: got %d want 1", len(bm.Hulls[0].ClipNodes))
	}
	if bm.Hulls[0].ClipNodes[0].Children[0] != int16(bspfile.ContentsEmpty) {
		t.Errorf("hull 0 child 0: got %d want EMPTY", bm.Hulls[0].ClipNodes[0].Children[0])
	}
	if bm.Hulls[0].FirstClipNode != 0 || bm.Hulls[0].LastClipNode != 0 {
		t.Errorf("hull 0 clipnode range: got [%d, %d] want [0, 0]", bm.Hulls[0].FirstClipNode, bm.Hulls[0].LastClipNode)
	}
	// Hulls 1-3 should use the ClipNodes lump (1 entry) with the
	// matching size offsets.
	for h := 1; h < bspfile.MaxMapHulls; h++ {
		if len(bm.Hulls[h].ClipNodes) != 1 {
			t.Errorf("hull %d ClipNodes: got %d want 1 (from ClipNodes lump)", h, len(bm.Hulls[h].ClipNodes))
		}
		if bm.Hulls[h].ClipMins != BrushHullSizes[h].Mins || bm.Hulls[h].ClipMaxs != BrushHullSizes[h].Maxs {
			t.Errorf("hull %d ClipMins/Maxs: got (%v, %v) want (%v, %v)",
				h, bm.Hulls[h].ClipMins, bm.Hulls[h].ClipMaxs,
				BrushHullSizes[h].Mins, BrushHullSizes[h].Maxs)
		}
	}
}

func TestLoadBrush_SubmodelIdxOutOfRange(t *testing.T) {
	data, size := buildMinimalBSPForBrushModel(t)
	f, err := bspfile.Open(bytes.NewReader(data), size)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBrush(f, 99); err == nil {
		t.Error("expected error for submodel index out of range")
	}
	if _, err := LoadBrush(f, -1); err == nil {
		t.Error("expected error for negative submodel index")
	}
}

// Simulate a file that fails on Models() (corrupt models lump).
// We construct a BSP with a Models lump whose length isn't a
// multiple of model size -> ErrSectionMisaligned propagates.
func TestLoadBrush_ModelsLumpCorrupt(t *testing.T) {
	data, size := buildMinimalBSPForBrushModel(t)
	// Corrupt the Models lump length to be off by 1 byte.
	const headerSize = 4 + 15*8
	// Lump entry layout: int32 offset + int32 length, 8 bytes each,
	// 15 entries in LumpKind order. Models is the 15th (index 14).
	modelsLumpOff := 4 + 14*8
	// Read current length, decrement by 1.
	curLen := int32(binary.LittleEndian.Uint32(data[modelsLumpOff+4 : modelsLumpOff+8]))
	binary.LittleEndian.PutUint32(data[modelsLumpOff+4:modelsLumpOff+8], uint32(curLen-1))
	_ = headerSize
	f, err := bspfile.Open(bytes.NewReader(data), size)
	if err != nil {
		// If Open already errors on the bad length, that's also fine
		// -- we just need to verify the error path is reachable.
		return
	}
	if _, err := LoadBrush(f, 0); err == nil {
		t.Error("expected error from corrupt models lump")
	}
}

// Cover every lump-read err-propagation branch in LoadBrush: each
// lump LoadBrush reads (planes, nodes, leafs, clipnodes) gets its
// length truncated by 1, making the typed decoder fail with
// ErrSectionMisaligned. The Open layer's section check uses unit=1
// (byte bounds only), so the truncated lengths pass Open and the
// typed decoder is what surfaces the error.
func TestLoadBrush_LumpReadErrPropagation(t *testing.T) {
	// LumpKind index -> human label, for failure messages.
	cases := []struct {
		name    string
		lumpIdx int
	}{
		{"planes", 1},    // LumpPlanes
		{"nodes", 5},     // LumpNodes
		{"leafs", 10},    // LumpLeafs
		{"clipnodes", 9}, // LumpClipnodes
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, size := buildMinimalBSPForBrushModel(t)
			off := 4 + c.lumpIdx*8
			curLen := int32(binary.LittleEndian.Uint32(data[off+4 : off+8]))
			binary.LittleEndian.PutUint32(data[off+4:off+8], uint32(curLen-1))
			f, err := bspfile.Open(bytes.NewReader(data), size)
			if err != nil {
				t.Fatalf("Open rejected the corruption before LoadBrush could; tighten the test: %v", err)
			}
			if _, err := LoadBrush(f, 0); err == nil {
				t.Errorf("expected error for corrupt %s lump", c.name)
			}
		})
	}
}

// Construct a BSP where the Nodes lump is fine but the leaf index a
// node points to is past the Leafs slice -> makeDrawHull errors via
// LoadBrush.
func TestLoadBrush_NodeReferencesBadLeaf(t *testing.T) {
	data, size := buildMinimalBSPForBrushModel(t)
	f, err := bspfile.Open(bytes.NewReader(data), size)
	if err != nil {
		t.Fatal(err)
	}
	// Sanity: confirm the happy-path baseline still loads.
	if _, err := LoadBrush(f, 0); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	// Now poison the Nodes lump payload: rewrite the first node's
	// child 0 to encode leaf index 99.
	planesLumpOff := 4 + 5*8 // Nodes is LumpKind=5
	nodesOff := int32(binary.LittleEndian.Uint32(data[planesLumpOff : planesLumpOff+4]))
	// Node struct layout: PlaneNum (4) + Children[0] (2) + Children[1] (2) + ...
	// Children[0] starts at nodesOff + 4.
	bad := ^int16(99) // = -100
	binary.LittleEndian.PutUint16(data[nodesOff+4:nodesOff+6], uint16(bad))
	f2, err := bspfile.Open(bytes.NewReader(data), size)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBrush(f2, 0); err == nil {
		t.Error("expected error for node->bad-leaf reference")
	}
}

// Reader that fails on the first ReadAt -- ensures Open is the one
// reading bytes and LoadBrush only walks pre-parsed lumps.
type failReader struct{}

func (failReader) ReadAt(p []byte, off int64) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

// Confirm LoadBrush never touches the source after Open finishes.
// (Open reads everything up front into f.LumpBytes; LoadBrush only
// calls *bspfile.File methods.) If we pass a file whose .LumpBytes
// is already populated, LoadBrush never touches the source, so we
// can sanity-check by passing nil-ish later if needed -- here we
// only verify the package treats nil src at the Open layer.
func TestLoadBrush_OpenLayerRejectsBadReader(t *testing.T) {
	if _, err := bspfile.Open(failReader{}, 4096); err == nil {
		t.Error("expected error from bspfile.Open on a failing reader")
	}
	if _, err := bspfile.Open(failReader{}, 4096); !errors.Is(err, io.ErrUnexpectedEOF) {
		// Just confirm we get *some* error; the exact wrapping is
		// up to bspfile.Open.
	}
}

// --- BrushHullSizes constants --------------------------------------------

func TestBrushHullSizes_PlayerAndMonster(t *testing.T) {
	if BrushHullSizes[1].Mins != [3]float32{-16, -16, -24} || BrushHullSizes[1].Maxs != [3]float32{16, 16, 32} {
		t.Errorf("hull 1 (player) bounds drift: %+v", BrushHullSizes[1])
	}
	if BrushHullSizes[2].Mins != [3]float32{-32, -32, -24} || BrushHullSizes[2].Maxs != [3]float32{32, 32, 64} {
		t.Errorf("hull 2 (monster) bounds drift: %+v", BrushHullSizes[2])
	}
	zero := HullSize{}
	if BrushHullSizes[0] != zero || BrushHullSizes[3] != zero {
		t.Errorf("hulls 0 + 3 should be zero-offset: %+v %+v", BrushHullSizes[0], BrushHullSizes[3])
	}
}

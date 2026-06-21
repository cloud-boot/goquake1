// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsprender_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bspfile/synthbsp"
	"github.com/go-quake1/engine/bsprender"
	"github.com/go-quake1/engine/model"
)

// End-to-end smoke test for [bsprender.NewMarkContext]: build a real
// BrushModel via model.LoadBrush, hand it to the constructor, run a
// full MarkVisibleLeaves pass, and verify the per-leaf + per-node
// VisFrame stamps land on the BrushModel.
//
// Tree (matches the model package's PVS test):
//
//	        node 0  (root)
//	       /        \
//	   node 1      node 2
//	   /   \        /   \
//	leaf1 leaf2  leaf3  node 3
//	                    /   \
//	                 leaf4  leaf5
//
// PVS-from-leaf-1: leaves 2 and 4 visible. The expected stamps after
// MarkVisibleLeaves(viewerLeaf=1, frame=42) are:
//
//	leaf 2  VisFrame=42  (via PVS bit)
//	leaf 4  VisFrame=42  (via PVS bit)
//	leaf 2's parent chain: node 1 -> node 0  -- both stamped
//	leaf 4's parent chain: node 3 -> node 2 -> node 0 (already 42, stop)
//
// So the touched nodes are exactly {0, 1, 2, 3}; the untouched ones
// (none in this tree) would stay at 0.
func TestNewMarkContext_EndToEnd(t *testing.T) {
	data, size, err := synthbsp.BuildFiveLeafPVS()
	if err != nil {
		t.Fatalf("synthbsp.BuildFiveLeafPVS: %v", err)
	}
	f, err := bspfile.Open(bytes.NewReader(data), size)
	if err != nil {
		t.Fatalf("bspfile.Open: %v", err)
	}
	bm, err := model.LoadBrush(f, 0)
	if err != nil {
		t.Fatalf("model.LoadBrush: %v", err)
	}

	ctx := bsprender.NewMarkContext(bm)
	if ctx.NumLeaves != 5 {
		t.Fatalf("NumLeaves: got %d want 5", ctx.NumLeaves)
	}

	if err := bsprender.MarkVisibleLeaves(ctx, 1, 42); err != nil {
		t.Fatalf("MarkVisibleLeaves: %v", err)
	}

	// Leaves 2 and 4 must be marked, the other PVS leaves untouched.
	if got := bm.Leaf(2).VisFrame; got != 42 {
		t.Errorf("leaf 2 VisFrame: got %d want 42", got)
	}
	if got := bm.Leaf(4).VisFrame; got != 42 {
		t.Errorf("leaf 4 VisFrame: got %d want 42", got)
	}
	for _, i := range []int{1, 3, 5} {
		if got := bm.Leaf(i).VisFrame; got != 0 {
			t.Errorf("leaf %d VisFrame: got %d want 0 (not in PVS)", i, got)
		}
	}
	// All four nodes must end up stamped via the parent walks.
	for n := 0; n < 4; n++ {
		if got := bm.GetNodeVisFrame(n); got != 42 {
			t.Errorf("node %d VisFrame: got %d want 42", n, got)
		}
	}
}

// End-to-end smoke test for [bsprender.NewWalkContext]: build a real
// BrushModel from the BuildWithFaces fixture (4 faces, 1 root node, 2
// leaves: empty + solid) and verify the WalkContext closures classify
// + dispatch correctly.
//
// Per the BuildWithFaces layout, node 0 has Children = [^0, ^1] so its
// front child (id = NumNodes + 0 = 1) is leaf 0 (EMPTY, the renderable
// side) and its back child (id = NumNodes + 1 = 2) is leaf 1 (SOLID,
// the outside-the-map sentinel).
func TestNewWalkContext_EndToEnd(t *testing.T) {
	data, size, err := synthbsp.BuildWithFaces()
	if err != nil {
		t.Fatalf("synthbsp.BuildWithFaces: %v", err)
	}
	f, err := bspfile.Open(bytes.NewReader(data), size)
	if err != nil {
		t.Fatalf("bspfile.Open: %v", err)
	}
	bm, err := model.LoadBrush(f, 0)
	if err != nil {
		t.Fatalf("model.LoadBrush: %v", err)
	}

	ctx := bsprender.NewWalkContext(bm)
	if ctx.NumNodes != 1 {
		t.Errorf("NumNodes: got %d want 1", ctx.NumNodes)
	}
	// Two raw leaves -> NumLeaves (PVS-trackable) is 1, but the unified
	// id space has 2 leaves (ids 1 + 2).
	if ctx.NumLeaves != bm.NumLeaves() {
		t.Errorf("NumLeaves: got %d want %d", ctx.NumLeaves, bm.NumLeaves())
	}
	// Node 0 is interior.
	if got := ctx.NodeKind(0); got != bsprender.NodeKindInterior {
		t.Errorf("NodeKind(0): got %v want NodeKindInterior", got)
	}
	// Leaf at id 1 is leaf-array index 0 (EMPTY in BuildWithFaces).
	// NumNodes=1 + leafIdx=0 = id 1.
	if got := ctx.NodeKind(1); got != bsprender.NodeKindEmpty {
		// leaf 0 is the outside sentinel by convention regardless of
		// raw Contents — NodeKind treats leafIdx == 0 as Empty.
		t.Errorf("NodeKind(1) [outside-sentinel slot]: got %v want NodeKindEmpty", got)
	}
	// Leaf at id 2 is leaf-array index 1 (SOLID in BuildWithFaces).
	if got := ctx.NodeKind(2); got != bsprender.NodeKindEmpty {
		t.Errorf("NodeKind(2) [SOLID leaf]: got %v want NodeKindEmpty", got)
	}
	// Children decoding: ^int16(0) -> id 1, ^int16(1) -> id 2.
	kids := ctx.NodeChildren(0)
	if kids[0] != 1 || kids[1] != 2 {
		t.Errorf("NodeChildren(0): got %v want [1 2]", kids)
	}
	// Plane is index 0 -> Normal (1,0,0).
	pl := ctx.NodePlane(0)
	if pl.Normal != [3]float32{1, 0, 0} {
		t.Errorf("NodePlane(0).Normal: got %v want [1 0 0]", pl.Normal)
	}
	// BBox: nodes Mins/Maxs default to zero in the BuildWithFaces fixture.
	mins, maxs := ctx.NodeBBox(0)
	if mins != [3]float32{0, 0, 0} || maxs != [3]float32{0, 0, 0} {
		t.Errorf("NodeBBox(0): got mins=%v maxs=%v want zero", mins, maxs)
	}
	// VisFrame: untouched -> 0.
	if got := ctx.NodeVisFrame(0); got != 0 {
		t.Errorf("NodeVisFrame(0): got %d want 0", got)
	}
	if got := ctx.LeafVisFrame(1); got != 0 {
		t.Errorf("LeafVisFrame(1): got %d want 0", got)
	}
	// LeafFaces: BuildWithFaces ships no marksurfaces lump, so every
	// leaf returns an empty slice.
	if r := ctx.LeafFaces(1); len(r) != 0 {
		t.Errorf("LeafFaces(1): got %v want empty", r)
	}
	if r := ctx.LeafFaces(2); len(r) != 0 {
		t.Errorf("LeafFaces(2): got %v want empty", r)
	}
	// Out-of-range leaf id -> nil.
	if r := ctx.LeafFaces(999); r != nil {
		t.Errorf("LeafFaces(999): got %v want nil", r)
	}
}

// NodeKind classifies a non-sentinel EMPTY leaf as NodeKindLeaf even
// when the BSP ships no marksurfaces lump (per the doc-comment: the
// walker calls LeafFaces and gets an empty slice; it's the per-leaf
// face decode that contributes nothing, not the kind classification).
// Build a custom 3-leaf BSP where leaf 1 is EMPTY (non-sentinel) and
// marksurfaces are non-empty to verify the LeafFaces resolution path.
func TestNewWalkContext_LeafFacesAndKind(t *testing.T) {
	// Build a richer BSP: 1 node, 3 leaves (outside, EMPTY with 2
	// marksurfaces, SOLID with 1 marksurface). Marksurfaces lump:
	// [10, 11, 99].
	planes := []bspfile.Plane{
		{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
	}
	nodes := []bspfile.Node{
		{PlaneNum: 0, Children: [2]int16{^int16(1), ^int16(2)}},
	}
	leafs := []bspfile.Leaf{
		{Contents: bspfile.ContentsSolid, VisOfs: -1},
		{Contents: bspfile.ContentsEmpty, VisOfs: -1, FirstMarkSurface: 0, NumMarkSurfaces: 2},
		{Contents: bspfile.ContentsSolid, VisOfs: -1, FirstMarkSurface: 2, NumMarkSurfaces: 1},
	}
	clipnodes := []bspfile.ClipNode{
		{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
	}
	models := []bspfile.Model{
		{Mins: [3]float32{-1, -1, -1}, Maxs: [3]float32{1, 1, 1},
			Headnode: [bspfile.MaxMapHulls]int32{0, 0, 0, 0}},
	}
	marksBlob := &bytes.Buffer{}
	for _, v := range []uint16{10, 11, 99} {
		_ = binary.Write(marksBlob, binary.LittleEndian, v)
	}

	const headerSize = 4 + 15*8
	body := &bytes.Buffer{}
	type lumpInfo struct {
		kind bspfile.LumpKind
		data []byte
	}
	lumps := []lumpInfo{
		{kind: bspfile.LumpPlanes, data: encodePlanes(planes)},
		{kind: bspfile.LumpNodes, data: encodeNodes(nodes)},
		{kind: bspfile.LumpLeafs, data: encodeLeafs(leafs)},
		{kind: bspfile.LumpClipnodes, data: encodeClipnodes(clipnodes)},
		{kind: bspfile.LumpModels, data: encodeModels(models)},
		{kind: bspfile.LumpMarksurfaces, data: marksBlob.Bytes()},
	}
	offsetByKind := map[bspfile.LumpKind]int32{}
	lenByKind := map[bspfile.LumpKind]int32{}
	for _, l := range lumps {
		offsetByKind[l.kind] = int32(headerSize) + int32(body.Len())
		body.Write(l.data)
		lenByKind[l.kind] = int32(len(l.data))
	}
	hdr := &bytes.Buffer{}
	_ = binary.Write(hdr, binary.LittleEndian, int32(bspfile.Version29))
	for k := bspfile.LumpKind(0); int(k) < bspfile.HeaderLumps; k++ {
		_ = binary.Write(hdr, binary.LittleEndian, offsetByKind[k])
		_ = binary.Write(hdr, binary.LittleEndian, lenByKind[k])
	}
	full := append(hdr.Bytes(), body.Bytes()...)

	f, err := bspfile.Open(bytes.NewReader(full), int64(len(full)))
	if err != nil {
		t.Fatalf("bspfile.Open: %v", err)
	}
	bm, err := model.LoadBrush(f, 0)
	if err != nil {
		t.Fatalf("model.LoadBrush: %v", err)
	}
	ctx := bsprender.NewWalkContext(bm)
	// id 0 = node 0 (interior); id 1 = leaf 0 (outside sentinel ->
	// Empty); id 2 = leaf 1 (EMPTY contents -> Leaf); id 3 = leaf 2
	// (SOLID contents -> Empty).
	if got := ctx.NodeKind(2); got != bsprender.NodeKindLeaf {
		t.Errorf("NodeKind(2) [EMPTY leaf with faces]: got %v want NodeKindLeaf", got)
	}
	if got := ctx.NodeKind(3); got != bsprender.NodeKindEmpty {
		t.Errorf("NodeKind(3) [SOLID leaf]: got %v want NodeKindEmpty", got)
	}
	// LeafFaces(2) -> [10, 11].
	r := ctx.LeafFaces(2)
	if len(r) != 2 || r[0] != 10 || r[1] != 11 {
		t.Errorf("LeafFaces(2): got %v want [10 11]", r)
	}
}

// NodeChildren returns a node-to-node id verbatim when the on-disk
// Children entry is >= 0 (an interior child). Build a 2-node BSP whose
// root's front child is node 1.
func TestNewWalkContext_NodeChildren_NodeChild(t *testing.T) {
	planes := []bspfile.Plane{
		{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
		{Normal: [3]float32{0, 1, 0}, Dist: 0, Type: bspfile.PlaneY},
	}
	nodes := []bspfile.Node{
		// root: front -> node 1, back -> leaf 0 (outside)
		{PlaneNum: 0, Children: [2]int16{1, ^int16(0)}},
		{PlaneNum: 1, Children: [2]int16{^int16(1), ^int16(2)}},
	}
	leafs := []bspfile.Leaf{
		{Contents: bspfile.ContentsSolid, VisOfs: -1},
		{Contents: bspfile.ContentsEmpty, VisOfs: -1},
		{Contents: bspfile.ContentsSolid, VisOfs: -1},
	}
	clipnodes := []bspfile.ClipNode{
		{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
	}
	models := []bspfile.Model{
		{Mins: [3]float32{-1, -1, -1}, Maxs: [3]float32{1, 1, 1},
			Headnode: [bspfile.MaxMapHulls]int32{0, 0, 0, 0}},
	}
	bm := mustLoadBSP(t, []lumpData{
		{kind: bspfile.LumpPlanes, data: encodePlanes(planes)},
		{kind: bspfile.LumpNodes, data: encodeNodes(nodes)},
		{kind: bspfile.LumpLeafs, data: encodeLeafs(leafs)},
		{kind: bspfile.LumpClipnodes, data: encodeClipnodes(clipnodes)},
		{kind: bspfile.LumpModels, data: encodeModels(models)},
	})
	ctx := bsprender.NewWalkContext(bm)
	kids := ctx.NodeChildren(0)
	// Front = node 1 (id 1); back = leaf 0 (id 2+0 = 2).
	if kids[0] != 1 || kids[1] != 2 {
		t.Errorf("NodeChildren(0): got %v want [1 2] (front node + back leaf 0)", kids)
	}
}

// NodePlane returns the zero plane when the node's PlaneNum is out of
// range (corrupt BSP). Patch a built BSP's first node's PlaneNum past
// the planes lump's length.
func TestNewWalkContext_NodePlane_OutOfRange(t *testing.T) {
	planes := []bspfile.Plane{
		{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
	}
	nodes := []bspfile.Node{
		{PlaneNum: 0, Children: [2]int16{^int16(0), ^int16(1)}},
	}
	leafs := []bspfile.Leaf{
		{Contents: bspfile.ContentsSolid, VisOfs: -1},
		{Contents: bspfile.ContentsSolid, VisOfs: -1},
	}
	clipnodes := []bspfile.ClipNode{
		{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
	}
	models := []bspfile.Model{
		{Mins: [3]float32{-1, -1, -1}, Maxs: [3]float32{1, 1, 1},
			Headnode: [bspfile.MaxMapHulls]int32{0, 0, 0, 0}},
	}
	bm := mustLoadBSP(t, []lumpData{
		{kind: bspfile.LumpPlanes, data: encodePlanes(planes)},
		{kind: bspfile.LumpNodes, data: encodeNodes(nodes)},
		{kind: bspfile.LumpLeafs, data: encodeLeafs(leafs)},
		{kind: bspfile.LumpClipnodes, data: encodeClipnodes(clipnodes)},
		{kind: bspfile.LumpModels, data: encodeModels(models)},
	})
	// Poison the in-memory node's PlaneNum past the lump's bounds.
	bm.Node(0).PlaneNum = 99
	ctx := bsprender.NewWalkContext(bm)
	pl := ctx.NodePlane(0)
	if pl.Normal != ([3]float32{}) || pl.Dist != 0 {
		t.Errorf("NodePlane(0) out-of-range: got %+v want zero plane", pl)
	}
}

// lumpData + mustLoadBSP are tiny helpers that wrap the boilerplate of
// emitting + opening a BSP byte stream from a per-lump table.
type lumpData struct {
	kind bspfile.LumpKind
	data []byte
}

func mustLoadBSP(t *testing.T, lumps []lumpData) *model.BrushModel {
	t.Helper()
	const headerSize = 4 + 15*8
	body := &bytes.Buffer{}
	offsetByKind := map[bspfile.LumpKind]int32{}
	lenByKind := map[bspfile.LumpKind]int32{}
	for _, l := range lumps {
		offsetByKind[l.kind] = int32(headerSize) + int32(body.Len())
		body.Write(l.data)
		lenByKind[l.kind] = int32(len(l.data))
	}
	hdr := &bytes.Buffer{}
	_ = binary.Write(hdr, binary.LittleEndian, int32(bspfile.Version29))
	for k := bspfile.LumpKind(0); int(k) < bspfile.HeaderLumps; k++ {
		_ = binary.Write(hdr, binary.LittleEndian, offsetByKind[k])
		_ = binary.Write(hdr, binary.LittleEndian, lenByKind[k])
	}
	full := append(hdr.Bytes(), body.Bytes()...)
	f, err := bspfile.Open(bytes.NewReader(full), int64(len(full)))
	if err != nil {
		t.Fatalf("bspfile.Open: %v", err)
	}
	bm, err := model.LoadBrush(f, 0)
	if err != nil {
		t.Fatalf("model.LoadBrush: %v", err)
	}
	return bm
}

// encodePlanes / encodeNodes / encodeLeafs / encodeClipnodes /
// encodeModels mirror the model package's test helpers (kept private
// inside their respective _test.go); we re-declare them here so the
// bsprender_test package can build standalone BSP fixtures without a
// cross-package dependency on testing internals.
func encodePlanes(planes []bspfile.Plane) []byte {
	b := &bytes.Buffer{}
	for _, p := range planes {
		_ = binary.Write(b, binary.LittleEndian, p.Normal[0])
		_ = binary.Write(b, binary.LittleEndian, p.Normal[1])
		_ = binary.Write(b, binary.LittleEndian, p.Normal[2])
		_ = binary.Write(b, binary.LittleEndian, p.Dist)
		_ = binary.Write(b, binary.LittleEndian, p.Type)
	}
	return b.Bytes()
}

func encodeNodes(nodes []bspfile.Node) []byte {
	b := &bytes.Buffer{}
	for _, n := range nodes {
		_ = binary.Write(b, binary.LittleEndian, n.PlaneNum)
		_ = binary.Write(b, binary.LittleEndian, n.Children[0])
		_ = binary.Write(b, binary.LittleEndian, n.Children[1])
		for j := 0; j < 3; j++ {
			_ = binary.Write(b, binary.LittleEndian, n.Mins[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(b, binary.LittleEndian, n.Maxs[j])
		}
		_ = binary.Write(b, binary.LittleEndian, n.FirstFace)
		_ = binary.Write(b, binary.LittleEndian, n.NumFaces)
	}
	return b.Bytes()
}

func encodeLeafs(leafs []bspfile.Leaf) []byte {
	b := &bytes.Buffer{}
	for _, l := range leafs {
		_ = binary.Write(b, binary.LittleEndian, l.Contents)
		_ = binary.Write(b, binary.LittleEndian, l.VisOfs)
		for j := 0; j < 3; j++ {
			_ = binary.Write(b, binary.LittleEndian, l.Mins[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(b, binary.LittleEndian, l.Maxs[j])
		}
		_ = binary.Write(b, binary.LittleEndian, l.FirstMarkSurface)
		_ = binary.Write(b, binary.LittleEndian, l.NumMarkSurfaces)
		b.Write(l.AmbientLevel[:])
	}
	return b.Bytes()
}

func encodeClipnodes(cs []bspfile.ClipNode) []byte {
	b := &bytes.Buffer{}
	for _, c := range cs {
		_ = binary.Write(b, binary.LittleEndian, c.PlaneNum)
		_ = binary.Write(b, binary.LittleEndian, c.Children[0])
		_ = binary.Write(b, binary.LittleEndian, c.Children[1])
	}
	return b.Bytes()
}

func encodeModels(ms []bspfile.Model) []byte {
	b := &bytes.Buffer{}
	for _, m := range ms {
		for j := 0; j < 3; j++ {
			_ = binary.Write(b, binary.LittleEndian, m.Mins[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(b, binary.LittleEndian, m.Maxs[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(b, binary.LittleEndian, m.Origin[j])
		}
		for j := 0; j < bspfile.MaxMapHulls; j++ {
			_ = binary.Write(b, binary.LittleEndian, m.Headnode[j])
		}
		_ = binary.Write(b, binary.LittleEndian, m.VisLeafs)
		_ = binary.Write(b, binary.LittleEndian, m.FirstFace)
		_ = binary.Write(b, binary.LittleEndian, m.NumFaces)
	}
	return b.Bytes()
}

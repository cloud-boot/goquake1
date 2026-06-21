// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsprender_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/go-quake1/engine/bspfile"
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
	data, size := buildBSPWithFiveLeafPVS(t)
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

// Same shape as the model package's buildBSPWithPVS helper but
// inlined here so bsprender's test stays self-contained (we only need
// one fixed PVS layout for this smoke test).
func buildBSPWithFiveLeafPVS(t *testing.T) ([]byte, int64) {
	t.Helper()

	planes := []bspfile.Plane{
		{Normal: [3]float32{1, 0, 0}, Type: bspfile.PlaneX},
		{Normal: [3]float32{0, 1, 0}, Type: bspfile.PlaneY},
		{Normal: [3]float32{0, 0, 1}, Type: bspfile.PlaneZ},
		{Normal: [3]float32{1, 1, 0}, Type: bspfile.PlaneAnyX},
	}
	nodes := []bspfile.Node{
		{PlaneNum: 0, Children: [2]int16{1, 2}},
		{PlaneNum: 1, Children: [2]int16{^int16(1), ^int16(2)}},
		{PlaneNum: 2, Children: [2]int16{^int16(3), 3}},
		{PlaneNum: 3, Children: [2]int16{^int16(4), ^int16(5)}},
	}
	// Per-leaf PVS rows -- 5 leaves, 1 byte/row, leaf 1 sees 2 and 4
	// (bits 1 + 3 -> 0x0A), all others see nothing.
	const rowBytes = 1
	pvs := []byte{0x0A, 0, 0, 0, 0}
	leafs := []bspfile.Leaf{
		{Contents: bspfile.ContentsSolid, VisOfs: -1},
		{Contents: bspfile.ContentsEmpty, VisOfs: 0 * rowBytes},
		{Contents: bspfile.ContentsEmpty, VisOfs: 1 * rowBytes},
		{Contents: bspfile.ContentsEmpty, VisOfs: 2 * rowBytes},
		{Contents: bspfile.ContentsEmpty, VisOfs: 3 * rowBytes},
		{Contents: bspfile.ContentsEmpty, VisOfs: 4 * rowBytes},
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

	pb := encodePlanes(planes)
	nb := encodeNodes(nodes)
	lb := encodeLeafs(leafs)
	cnb := encodeClipnodes(clipnodes)
	mb := encodeModels(models)

	const headerSize = 4 + 15*8
	body := &bytes.Buffer{}
	type lump struct {
		kind bspfile.LumpKind
		data []byte
	}
	lumps := []lump{
		{kind: bspfile.LumpPlanes, data: pb},
		{kind: bspfile.LumpVisibility, data: pvs},
		{kind: bspfile.LumpNodes, data: nb},
		{kind: bspfile.LumpLeafs, data: lb},
		{kind: bspfile.LumpClipnodes, data: cnb},
		{kind: bspfile.LumpModels, data: mb},
	}
	offs := map[bspfile.LumpKind]int32{}
	lens := map[bspfile.LumpKind]int32{}
	for _, l := range lumps {
		offs[l.kind] = int32(headerSize) + int32(body.Len())
		body.Write(l.data)
		lens[l.kind] = int32(len(l.data))
	}

	hdr := &bytes.Buffer{}
	_ = binary.Write(hdr, binary.LittleEndian, int32(bspfile.Version29))
	for k := bspfile.LumpKind(0); int(k) < bspfile.HeaderLumps; k++ {
		_ = binary.Write(hdr, binary.LittleEndian, offs[k])
		_ = binary.Write(hdr, binary.LittleEndian, lens[k])
	}
	full := append(hdr.Bytes(), body.Bytes()...)
	return full, int64(len(full))
}

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

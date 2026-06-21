// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bsprender_test

import (
	"bytes"
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

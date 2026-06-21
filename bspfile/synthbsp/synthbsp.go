// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package synthbsp

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/go-quake1/engine/bspfile"
)

// ErrInvalidPlan is reserved for future builders that accept caller-
// supplied plans (vertex counts, face counts, etc.) and need to reject
// inconsistent inputs. The current builders are plan-free and never
// fail, so they never return this sentinel; it ships so callers can
// switch on it without an import cycle once the parameterised builders
// land.
var ErrInvalidPlan = errors.New("synthbsp: invalid build plan")

// BuildFiveLeafPVS produces a synthetic BSP with 5 leaves arranged in
// a depth-3 tree + a 4-byte PVS row per leaf. Used as the canonical
// minimal-but-valid BSP for vis/walk testing. Returns the raw bytes +
// size (so callers can wrap in [bytes.NewReader] for [bspfile.Open]).
//
// Tree layout (matches the model package's PVS fixture):
//
//	        node 0  (root)
//	       /        \
//	   node 1      node 2
//	   /   \        /   \
//	leaf1 leaf2  leaf3  node 3
//	                    /   \
//	                 leaf4  leaf5
//
// Promoted verbatim from bsprender/integration_test.go's
// buildBSPWithFiveLeafPVS helper, minus the *testing.T dependency.
func BuildFiveLeafPVS() ([]byte, int64, error) {
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

	full := assembleBSP([]lump{
		{kind: bspfile.LumpPlanes, data: encodePlanes(planes)},
		{kind: bspfile.LumpVisibility, data: pvs},
		{kind: bspfile.LumpNodes, data: encodeNodes(nodes)},
		{kind: bspfile.LumpLeafs, data: encodeLeafs(leafs)},
		{kind: bspfile.LumpClipnodes, data: encodeClipnodes(clipnodes)},
		{kind: bspfile.LumpModels, data: encodeModels(models)},
	})
	return full, int64(len(full)), nil
}

// BuildWithFaces produces a synthetic BSP with 4 faces arranged so
// every accessor in [bspfile] (Faces / Edges / Surfedges / Vertexes
// / TexInfos / Textures / Planes / Nodes / Leafs) returns non-empty,
// valid data. Used for end-to-end rendering tests + production demos
// that need a renderable BSP.
//
// Face layout:
//
//	face 0: 3 verts, surfedges (1,2,3) all positive       -> verts 0,1,2
//	face 1: 3 verts, surfedges (-1,-2,-3) all negative    -> verts 2,0,1
//	face 2: 3 verts, surfedges (1,2,3); TexInfo -> missing-texture slot
//	face 3: 3 verts, surfedges (1,2,3); TexInfo index 99 (out of range)
//
// Vertex table (4 entries): (0,0,0), (10,0,0), (0,10,0), (5,5,0).
// Edge table (4 entries): index 0 is the "null" edge tyrquake reserves;
// indices 1..3 link (0,1), (1,2), (2,0).
//
// Promoted from bsprender/face_xform_test.go's buildBSPWithFaces.
func BuildWithFaces() ([]byte, int64, error) {
	return BuildWithFacesCustomTextures(defaultTextures)
}

// BuildWithFacesCustomTextures is [BuildWithFaces] with a caller-
// supplied textures lump (use to inject a non-trivial miptex
// layout for texturing tests).
func BuildWithFacesCustomTextures(makeTextures func() []byte) ([]byte, int64, error) {
	planes := []bspfile.Plane{
		{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
	}
	nodes := []bspfile.Node{
		// one node, both children = leaf 0 + leaf 1
		{PlaneNum: 0, Children: [2]int16{^int16(0), ^int16(1)}},
	}
	leafs := []bspfile.Leaf{
		{Contents: bspfile.ContentsEmpty, VisOfs: -1},
		{Contents: bspfile.ContentsSolid, VisOfs: -1},
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
	vertices := []bspfile.Vertex{
		{X: 0, Y: 0, Z: 0},  // 0
		{X: 10, Y: 0, Z: 0}, // 1
		{X: 0, Y: 10, Z: 0}, // 2
		{X: 5, Y: 5, Z: 0},  // 3 (unused)
	}
	edges := []bspfile.Edge{
		{V0: 0, V1: 0}, // 0: reserved null edge
		{V0: 0, V1: 2}, // 1: V0=0, V1=2 (positive=V0=0, negative=V1=2)
		{V0: 1, V1: 0}, // 2: V0=1, V1=0
		{V0: 2, V1: 1}, // 3: V0=2, V1=1
	}
	surfedges := []bspfile.Surfedge{
		// face 0 (positive): 1, 2, 3 -> V0 of edge 1,2,3 -> verts 0,1,2
		1, 2, 3,
		// face 1 (negative): -1,-2,-3 -> V1 of edge 1,2,3 -> verts 2,0,1
		-1, -2, -3,
		// face 2 + face 3 reuse the first triple (1,2,3)
	}
	texinfos := []bspfile.TexInfo{
		{Vecs: [2][4]float32{{1, 0, 0, 0}, {0, 1, 0, 0}}, MipTex: 0, Flags: 0},
		{Vecs: [2][4]float32{{2, 0, 0, 0}, {0, 2, 0, 0}}, MipTex: 1, Flags: 0},
		{Vecs: [2][4]float32{{3, 0, 0, 0}, {0, 3, 0, 0}}, MipTex: 0, Flags: 0},
	}
	faces := []bspfile.Face{
		{PlaneNum: 0, Side: 0, FirstEdge: 0, NumEdges: 3, TexInfo: 0, LightOfs: -1},
		{PlaneNum: 0, Side: 0, FirstEdge: 3, NumEdges: 3, TexInfo: 0, LightOfs: -1},
		{PlaneNum: 0, Side: 0, FirstEdge: 0, NumEdges: 3, TexInfo: 1, LightOfs: -1},
		{PlaneNum: 0, Side: 0, FirstEdge: 0, NumEdges: 3, TexInfo: 99, LightOfs: -1},
	}

	full := assembleBSP([]lump{
		{kind: bspfile.LumpPlanes, data: encodePlanes(planes)},
		{kind: bspfile.LumpNodes, data: encodeNodes(nodes)},
		{kind: bspfile.LumpLeafs, data: encodeLeafs(leafs)},
		{kind: bspfile.LumpClipnodes, data: encodeClipnodes(clipnodes)},
		{kind: bspfile.LumpModels, data: encodeModels(models)},
		{kind: bspfile.LumpVertexes, data: encodeVertices(vertices)},
		{kind: bspfile.LumpEdges, data: encodeEdges(edges)},
		{kind: bspfile.LumpSurfedges, data: encodeSurfedges(surfedges)},
		{kind: bspfile.LumpTexInfo, data: encodeTexInfos(texinfos)},
		{kind: bspfile.LumpFaces, data: encodeFaces(faces)},
		{kind: bspfile.LumpTextures, data: makeTextures()},
	})
	return full, int64(len(full)), nil
}

// defaultTextures returns the textures-lump bytes [BuildWithFaces]
// uses by default: 2 miptex entries, slot 1 is missing (offset = -1).
func defaultTextures() []byte {
	// MipTex record is 40 bytes (name[16] + W + H + 4*offset).
	const miptexRecordSize = 16 + 4 + 4 + 4*4
	// Directory: int32 numMipTex + numMipTex * int32 offset.
	const dirSize = 4 + 2*4
	miptex0 := make([]byte, miptexRecordSize)
	copy(miptex0[0:16], []byte("trim"))
	binary.LittleEndian.PutUint32(miptex0[16:20], 64) // width
	binary.LittleEndian.PutUint32(miptex0[20:24], 32) // height
	// mip offsets all zero (we don't decode the pixels).

	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, int32(2))       // numMipTex
	_ = binary.Write(buf, binary.LittleEndian, int32(dirSize)) // slot 0 offset
	_ = binary.Write(buf, binary.LittleEndian, int32(-1))      // slot 1 missing
	buf.Write(miptex0)
	return buf.Bytes()
}

// --- header + lump assembly -------------------------------------------------

type lump struct {
	kind bspfile.LumpKind
	data []byte
}

// assembleBSP stitches the header + each lump's bytes into a complete
// BSP byte stream. Lumps the caller doesn't supply are emitted as
// empty (offset 0, length 0) entries in the header.
func assembleBSP(lumps []lump) []byte {
	const headerSize = 4 + 15*8
	body := &bytes.Buffer{}
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
	return append(hdr.Bytes(), body.Bytes()...)
}

// --- per-lump encoders ------------------------------------------------------

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

func encodeVertices(vs []bspfile.Vertex) []byte {
	b := &bytes.Buffer{}
	for _, v := range vs {
		_ = binary.Write(b, binary.LittleEndian, v.X)
		_ = binary.Write(b, binary.LittleEndian, v.Y)
		_ = binary.Write(b, binary.LittleEndian, v.Z)
	}
	return b.Bytes()
}

func encodeEdges(es []bspfile.Edge) []byte {
	b := &bytes.Buffer{}
	for _, e := range es {
		_ = binary.Write(b, binary.LittleEndian, e.V0)
		_ = binary.Write(b, binary.LittleEndian, e.V1)
	}
	return b.Bytes()
}

func encodeSurfedges(ses []bspfile.Surfedge) []byte {
	b := &bytes.Buffer{}
	for _, se := range ses {
		_ = binary.Write(b, binary.LittleEndian, int32(se))
	}
	return b.Bytes()
}

func encodeTexInfos(ts []bspfile.TexInfo) []byte {
	b := &bytes.Buffer{}
	for _, t := range ts {
		for vec := 0; vec < 2; vec++ {
			for c := 0; c < 4; c++ {
				_ = binary.Write(b, binary.LittleEndian, t.Vecs[vec][c])
			}
		}
		_ = binary.Write(b, binary.LittleEndian, t.MipTex)
		_ = binary.Write(b, binary.LittleEndian, t.Flags)
	}
	return b.Bytes()
}

func encodeFaces(fs []bspfile.Face) []byte {
	b := &bytes.Buffer{}
	for _, f := range fs {
		_ = binary.Write(b, binary.LittleEndian, f.PlaneNum)
		_ = binary.Write(b, binary.LittleEndian, f.Side)
		_ = binary.Write(b, binary.LittleEndian, f.FirstEdge)
		_ = binary.Write(b, binary.LittleEndian, f.NumEdges)
		_ = binary.Write(b, binary.LittleEndian, f.TexInfo)
		b.Write(f.Styles[:])
		_ = binary.Write(b, binary.LittleEndian, f.LightOfs)
	}
	return b.Bytes()
}

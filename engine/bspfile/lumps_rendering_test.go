// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bspfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

// buildWithLumps extends the buildSpec from bspfile_test.go by
// populating any of the newly-supported lumps. We can't reuse build()
// directly (it ignores the new fields) so we write a fresh helper
// that encodes everything.
type buildSpecExt struct {
	buildSpec
	nodes        []Node
	clipnodes    []ClipNode
	leafs        []Leaf
	faces        []Face
	texinfos     []TexInfo
	marksurfaces []MarkSurface
	textures     []byte // already-encoded MipTexLump bytes (caller responsibility)
	visibility   []byte
	lighting     []byte
}

func encVec(w *bytes.Buffer, x, y, z float32) {
	_ = binary.Write(w, binary.LittleEndian, x)
	_ = binary.Write(w, binary.LittleEndian, y)
	_ = binary.Write(w, binary.LittleEndian, z)
}

func encNode(w *bytes.Buffer, n Node) {
	_ = binary.Write(w, binary.LittleEndian, n.PlaneNum)
	for _, c := range n.Children {
		_ = binary.Write(w, binary.LittleEndian, c)
	}
	for _, m := range n.Mins {
		_ = binary.Write(w, binary.LittleEndian, m)
	}
	for _, m := range n.Maxs {
		_ = binary.Write(w, binary.LittleEndian, m)
	}
	_ = binary.Write(w, binary.LittleEndian, n.FirstFace)
	_ = binary.Write(w, binary.LittleEndian, n.NumFaces)
}

func encClipNode(w *bytes.Buffer, c ClipNode) {
	_ = binary.Write(w, binary.LittleEndian, c.PlaneNum)
	_ = binary.Write(w, binary.LittleEndian, c.Children[0])
	_ = binary.Write(w, binary.LittleEndian, c.Children[1])
}

func encLeaf(w *bytes.Buffer, l Leaf) {
	_ = binary.Write(w, binary.LittleEndian, l.Contents)
	_ = binary.Write(w, binary.LittleEndian, l.VisOfs)
	for _, m := range l.Mins {
		_ = binary.Write(w, binary.LittleEndian, m)
	}
	for _, m := range l.Maxs {
		_ = binary.Write(w, binary.LittleEndian, m)
	}
	_ = binary.Write(w, binary.LittleEndian, l.FirstMarkSurface)
	_ = binary.Write(w, binary.LittleEndian, l.NumMarkSurfaces)
	w.Write(l.AmbientLevel[:])
}

func encFace(w *bytes.Buffer, f Face) {
	_ = binary.Write(w, binary.LittleEndian, f.PlaneNum)
	_ = binary.Write(w, binary.LittleEndian, f.Side)
	_ = binary.Write(w, binary.LittleEndian, f.FirstEdge)
	_ = binary.Write(w, binary.LittleEndian, f.NumEdges)
	_ = binary.Write(w, binary.LittleEndian, f.TexInfo)
	w.Write(f.Styles[:])
	_ = binary.Write(w, binary.LittleEndian, f.LightOfs)
}

func encTexInfo(w *bytes.Buffer, t TexInfo) {
	for vec := 0; vec < 2; vec++ {
		for c := 0; c < 4; c++ {
			_ = binary.Write(w, binary.LittleEndian, t.Vecs[vec][c])
		}
	}
	_ = binary.Write(w, binary.LittleEndian, t.MipTex)
	_ = binary.Write(w, binary.LittleEndian, t.Flags)
}

func buildExt(s buildSpecExt) ([]byte, int64) {
	v := s.version
	if v == 0 {
		v = Version29
	}

	type placed struct{ off, sz int32 }
	body := &bytes.Buffer{}
	put := func(b []byte) placed {
		p := placed{off: int32(headerSize) + int32(body.Len()), sz: int32(len(b))}
		body.Write(b)
		return p
	}

	// Entities
	ent := put(s.entities)

	// Vertexes
	vb := &bytes.Buffer{}
	for _, v := range s.vertices {
		encVec(vb, v.X, v.Y, v.Z)
	}
	verts := put(vb.Bytes())

	// Edges
	eb := &bytes.Buffer{}
	for _, e := range s.edges {
		_ = binary.Write(eb, binary.LittleEndian, e.V0)
		_ = binary.Write(eb, binary.LittleEndian, e.V1)
	}
	edges := put(eb.Bytes())

	// Surfedges
	sb := &bytes.Buffer{}
	for _, se := range s.surfedges {
		_ = binary.Write(sb, binary.LittleEndian, int32(se))
	}
	surfedges := put(sb.Bytes())

	// Planes
	pb := &bytes.Buffer{}
	for _, p := range s.planes {
		encVec(pb, p.Normal[0], p.Normal[1], p.Normal[2])
		_ = binary.Write(pb, binary.LittleEndian, p.Dist)
		_ = binary.Write(pb, binary.LittleEndian, p.Type)
	}
	planes := put(pb.Bytes())

	// Models
	mb := &bytes.Buffer{}
	for _, m := range s.models {
		encVec(mb, m.Mins[0], m.Mins[1], m.Mins[2])
		encVec(mb, m.Maxs[0], m.Maxs[1], m.Maxs[2])
		encVec(mb, m.Origin[0], m.Origin[1], m.Origin[2])
		for _, h := range m.Headnode {
			_ = binary.Write(mb, binary.LittleEndian, h)
		}
		_ = binary.Write(mb, binary.LittleEndian, m.VisLeafs)
		_ = binary.Write(mb, binary.LittleEndian, m.FirstFace)
		_ = binary.Write(mb, binary.LittleEndian, m.NumFaces)
	}
	models := put(mb.Bytes())

	// Nodes
	nb := &bytes.Buffer{}
	for _, n := range s.nodes {
		encNode(nb, n)
	}
	nodes := put(nb.Bytes())

	// Clipnodes
	cb := &bytes.Buffer{}
	for _, c := range s.clipnodes {
		encClipNode(cb, c)
	}
	clipnodes := put(cb.Bytes())

	// Leafs
	lb := &bytes.Buffer{}
	for _, l := range s.leafs {
		encLeaf(lb, l)
	}
	leafs := put(lb.Bytes())

	// Faces
	fb := &bytes.Buffer{}
	for _, f := range s.faces {
		encFace(fb, f)
	}
	faces := put(fb.Bytes())

	// TexInfos
	tib := &bytes.Buffer{}
	for _, ti := range s.texinfos {
		encTexInfo(tib, ti)
	}
	texinfos := put(tib.Bytes())

	// MarkSurfaces
	msb := &bytes.Buffer{}
	for _, ms := range s.marksurfaces {
		_ = binary.Write(msb, binary.LittleEndian, uint16(ms))
	}
	marks := put(msb.Bytes())

	// Textures (pre-encoded MipTexLump bytes)
	textures := put(s.textures)

	// Visibility / Lighting
	vis := put(s.visibility)
	lite := put(s.lighting)

	hdr := &bytes.Buffer{}
	_ = binary.Write(hdr, binary.LittleEndian, v)
	for i := 0; i < HeaderLumps; i++ {
		l := placed{}
		switch LumpKind(i) {
		case LumpEntities:
			l = ent
		case LumpVertexes:
			l = verts
		case LumpEdges:
			l = edges
		case LumpSurfedges:
			l = surfedges
		case LumpPlanes:
			l = planes
		case LumpModels:
			l = models
		case LumpNodes:
			l = nodes
		case LumpClipnodes:
			l = clipnodes
		case LumpLeafs:
			l = leafs
		case LumpFaces:
			l = faces
		case LumpTexInfo:
			l = texinfos
		case LumpMarksurfaces:
			l = marks
		case LumpTextures:
			l = textures
		case LumpVisibility:
			l = vis
		case LumpLighting:
			l = lite
		}
		_ = binary.Write(hdr, binary.LittleEndian, l.off)
		_ = binary.Write(hdr, binary.LittleEndian, l.sz)
	}
	out := append(hdr.Bytes(), body.Bytes()...)
	return out, int64(len(out))
}

// --- happy path -------------------------------------------------------------

func TestRenderingLumps_DecodeAll(t *testing.T) {
	raw, sz := buildExt(buildSpecExt{
		nodes: []Node{
			{PlaneNum: 1, Children: [2]int16{2, -3}, Mins: [3]int16{-32, -32, 0}, Maxs: [3]int16{32, 32, 64}, FirstFace: 0, NumFaces: 4},
		},
		clipnodes: []ClipNode{
			{PlaneNum: 5, Children: [2]int16{-2, 7}},
		},
		leafs: []Leaf{
			{Contents: ContentsSolid, VisOfs: -1, Mins: [3]int16{0, 0, 0}, Maxs: [3]int16{16, 16, 16}, FirstMarkSurface: 0, NumMarkSurfaces: 2, AmbientLevel: [NumAmbients]byte{0, 0, 0, 0}},
		},
		faces: []Face{
			{PlaneNum: 1, Side: 0, FirstEdge: 0, NumEdges: 3, TexInfo: 0, Styles: [MaxLightmaps]byte{0, 255, 255, 255}, LightOfs: -1},
		},
		texinfos: []TexInfo{
			{Vecs: [2][4]float32{{1, 0, 0, 0}, {0, 1, 0, 0}}, MipTex: 0, Flags: 0},
		},
		marksurfaces: []MarkSurface{0, 1, 2},
	})

	f, err := Open(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}

	ns, err := f.Nodes()
	if err != nil || len(ns) != 1 || ns[0].PlaneNum != 1 || ns[0].NumFaces != 4 {
		t.Errorf("Nodes: %v %+v", err, ns)
	}
	cs, err := f.ClipNodes()
	if err != nil || len(cs) != 1 || cs[0].Children[0] != -2 {
		t.Errorf("ClipNodes: %v %+v", err, cs)
	}
	ls, err := f.Leafs()
	if err != nil || len(ls) != 1 || ls[0].Contents != ContentsSolid {
		t.Errorf("Leafs: %v %+v", err, ls)
	}
	fs, err := f.Faces()
	if err != nil || len(fs) != 1 || fs[0].NumEdges != 3 {
		t.Errorf("Faces: %v %+v", err, fs)
	}
	tis, err := f.TexInfos()
	if err != nil || len(tis) != 1 || tis[0].Vecs[0][0] != 1 {
		t.Errorf("TexInfos: %v %+v", err, tis)
	}
	ms, err := f.MarkSurfaces()
	if err != nil || len(ms) != 3 || ms[2] != 2 {
		t.Errorf("MarkSurfaces: %v %v", err, ms)
	}
}

// --- visibility + lighting blobs come back verbatim -----------------------

func TestRenderingLumps_OpaqueBlobs(t *testing.T) {
	raw, sz := buildExt(buildSpecExt{
		visibility: []byte{1, 2, 3, 4, 5},
		lighting:   []byte{0xAA, 0xBB, 0xCC},
	})
	f, err := Open(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	if vb := f.Visibility(); !bytes.Equal(vb, []byte{1, 2, 3, 4, 5}) {
		t.Errorf("Visibility: %v", vb)
	}
	if lb := f.Lighting(); !bytes.Equal(lb, []byte{0xAA, 0xBB, 0xCC}) {
		t.Errorf("Lighting: %v", lb)
	}
}

// --- misalignment branches -------------------------------------------------

func TestNodes_Misaligned(t *testing.T) {
	raw, sz := buildExt(buildSpecExt{
		nodes: []Node{{PlaneNum: 1}},
	})
	patchLumpLen(raw, LumpNodes, nodeSize-1)
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.Nodes(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

func TestClipNodes_Misaligned(t *testing.T) {
	raw, sz := buildExt(buildSpecExt{clipnodes: []ClipNode{{}}})
	patchLumpLen(raw, LumpClipnodes, clipnodeSize-1)
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.ClipNodes(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

func TestLeafs_Misaligned(t *testing.T) {
	raw, sz := buildExt(buildSpecExt{leafs: []Leaf{{Contents: -1}}})
	patchLumpLen(raw, LumpLeafs, leafSize-1)
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.Leafs(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

func TestFaces_Misaligned(t *testing.T) {
	raw, sz := buildExt(buildSpecExt{faces: []Face{{}}})
	patchLumpLen(raw, LumpFaces, faceSize-1)
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.Faces(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

func TestTexInfos_Misaligned(t *testing.T) {
	raw, sz := buildExt(buildSpecExt{texinfos: []TexInfo{{}}})
	patchLumpLen(raw, LumpTexInfo, texInfoSize-1)
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.TexInfos(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

func TestMarkSurfaces_Misaligned(t *testing.T) {
	// 2 marksurfaces = 4 bytes of payload; patch the length to 3
	// (still fits inside the file, but 3 is not a multiple of
	// marksurfaceSize=2). Open must therefore succeed + MarkSurfaces
	// must surface ErrSectionMisaligned.
	raw, sz := buildExt(buildSpecExt{marksurfaces: []MarkSurface{1, 2}})
	patchLumpLen(raw, LumpMarksurfaces, marksurfaceSize+1)
	f, err := Open(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.MarkSurfaces(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

// --- MipTexLump ------------------------------------------------------------

// encodeMipTexLump builds an in-memory MipTexLump byte block: header
// (NumMipTex + Offsets[]) followed by N back-to-back miptex_t
// records. Offsets are relative to the start of the block, so a -1
// in the Offsets table marks a "missing" texture slot.
func encodeMipTexLump(textures []MipTex, missing []bool) []byte {
	buf := &bytes.Buffer{}
	n := len(textures)
	_ = binary.Write(buf, binary.LittleEndian, int32(n))
	// Offsets table -- 4 bytes * n.
	// We'll patch the real offsets after writing each miptex record.
	for i := 0; i < n; i++ {
		_ = binary.Write(buf, binary.LittleEndian, int32(0))
	}
	out := buf.Bytes()
	for i, mt := range textures {
		if missing != nil && missing[i] {
			// Patch this offset to -1.
			binary.LittleEndian.PutUint32(out[4+i*4:8+i*4], 0xFFFFFFFF)
			continue
		}
		off := int32(len(out))
		binary.LittleEndian.PutUint32(out[4+i*4:8+i*4], uint32(off))
		nameBuf := make([]byte, 16)
		copy(nameBuf, mt.Name)
		out = append(out, nameBuf...)
		var u [4]byte
		binary.LittleEndian.PutUint32(u[:], mt.Width)
		out = append(out, u[:]...)
		binary.LittleEndian.PutUint32(u[:], mt.Height)
		out = append(out, u[:]...)
		for j := 0; j < MipLevels; j++ {
			binary.LittleEndian.PutUint32(u[:], mt.Offsets[j])
			out = append(out, u[:]...)
		}
	}
	return out
}

func TestTextures_HappyPath(t *testing.T) {
	textures := []MipTex{
		{Name: "lava1", Width: 64, Height: 64, Offsets: [4]uint32{40, 40 + 64*64, 0, 0}},
		{Name: "rock", Width: 32, Height: 32, Offsets: [4]uint32{40, 0, 0, 0}},
	}
	raw, sz := buildExt(buildSpecExt{textures: encodeMipTexLump(textures, nil)})
	f, _ := Open(bytes.NewReader(raw), sz)
	mtl, err := f.Textures()
	if err != nil {
		t.Fatal(err)
	}
	if mtl.NumMipTex != 2 {
		t.Errorf("NumMipTex: %d", mtl.NumMipTex)
	}
	mt, ok, err := mtl.MipTex(0)
	if err != nil || !ok || mt.Name != "lava1" || mt.Width != 64 {
		t.Errorf("MipTex(0): %v %v %+v", err, ok, mt)
	}
	mt, ok, err = mtl.MipTex(1)
	if err != nil || !ok || mt.Name != "rock" {
		t.Errorf("MipTex(1): %v %v %+v", err, ok, mt)
	}
}

func TestTextures_MissingSlot(t *testing.T) {
	textures := []MipTex{
		{Name: "real", Width: 16, Height: 16},
		{Name: "ignored"},
	}
	missing := []bool{false, true}
	raw, sz := buildExt(buildSpecExt{textures: encodeMipTexLump(textures, missing)})
	f, _ := Open(bytes.NewReader(raw), sz)
	mtl, _ := f.Textures()
	_, ok, err := mtl.MipTex(1)
	if err != nil || ok {
		t.Errorf("missing slot: ok=%v err=%v want (false, nil)", ok, err)
	}
}

func TestTextures_EmptyLump(t *testing.T) {
	raw, sz := buildExt(buildSpecExt{})
	f, _ := Open(bytes.NewReader(raw), sz)
	mtl, err := f.Textures()
	if err != nil {
		t.Fatal(err)
	}
	if mtl.NumMipTex != 0 {
		t.Errorf("empty lump: NumMipTex=%d", mtl.NumMipTex)
	}
}

func TestTextures_TruncatedHeader(t *testing.T) {
	// 2-byte lump: not enough for the 4-byte NumMipTex.
	raw, sz := buildExt(buildSpecExt{textures: []byte{1, 2}})
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.Textures(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v want ErrSectionMisaligned", err)
	}
}

func TestTextures_NegativeNumMipTex(t *testing.T) {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, int32(-1))
	raw, sz := buildExt(buildSpecExt{textures: buf.Bytes()})
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.Textures(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

func TestTextures_OffsetsTableTruncated(t *testing.T) {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, int32(10)) // claims 10 entries
	buf.Write([]byte{1, 2, 3, 4}) // only one int32 of offset table
	raw, sz := buildExt(buildSpecExt{textures: buf.Bytes()})
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.Textures(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

func TestMipTex_OutOfRange(t *testing.T) {
	raw, sz := buildExt(buildSpecExt{textures: encodeMipTexLump([]MipTex{{Name: "x", Width: 1, Height: 1}}, nil)})
	f, _ := Open(bytes.NewReader(raw), sz)
	mtl, _ := f.Textures()
	if _, _, err := mtl.MipTex(-1); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v", err)
	}
	if _, _, err := mtl.MipTex(99); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v", err)
	}
}

func TestMipTex_OffsetPastEOF(t *testing.T) {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, int32(1))
	_ = binary.Write(buf, binary.LittleEndian, int32(1<<30)) // huge offset
	raw, sz := buildExt(buildSpecExt{textures: buf.Bytes()})
	f, _ := Open(bytes.NewReader(raw), sz)
	mtl, _ := f.Textures()
	if _, _, err := mtl.MipTex(0); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v", err)
	}
}

func TestMipTex_NegativeOffsetThatIsntSentinel(t *testing.T) {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, int32(1))
	// -2: negative but NOT the -1 sentinel; treated as out-of-range.
	_ = binary.Write(buf, binary.LittleEndian, int32(-2))
	raw, sz := buildExt(buildSpecExt{textures: buf.Bytes()})
	f, _ := Open(bytes.NewReader(raw), sz)
	mtl, _ := f.Textures()
	if _, _, err := mtl.MipTex(0); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v", err)
	}
}

// MipTex with a NAME slot that runs to the full 16 bytes (no inner
// NUL) is read correctly.
func TestMipTex_NameFillsSlot(t *testing.T) {
	mt := MipTex{Name: "aaaaaaaaaaaaaaaa", Width: 1, Height: 1} // 16 'a's
	raw, sz := buildExt(buildSpecExt{textures: encodeMipTexLump([]MipTex{mt}, nil)})
	f, _ := Open(bytes.NewReader(raw), sz)
	mtl, _ := f.Textures()
	got, _, _ := mtl.MipTex(0)
	if got.Name != "aaaaaaaaaaaaaaaa" {
		t.Errorf("name fills slot: got %q", got.Name)
	}
}

// Sanity: float precision in TexInfo's 8 floats round-trips.
func TestTexInfos_FloatRoundTrip(t *testing.T) {
	raw, sz := buildExt(buildSpecExt{
		texinfos: []TexInfo{{Vecs: [2][4]float32{{float32(math.Pi), 0, 0, 0}, {0, float32(math.E), 0, 0}}}},
	})
	f, _ := Open(bytes.NewReader(raw), sz)
	tis, _ := f.TexInfos()
	if tis[0].Vecs[0][0] != float32(math.Pi) || tis[0].Vecs[1][1] != float32(math.E) {
		t.Errorf("float precision: %+v", tis[0].Vecs)
	}
}

// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bspfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"
)

// buildSpec describes a minimal BSP for the byte synthesiser below.
// Only the lumps we ship typed decoders for need entries; others
// default to empty.
type buildSpec struct {
	version   int32 // 0 -> Version29
	vertices  []Vertex
	edges     []Edge
	surfedges []Surfedge
	planes    []Plane
	models    []Model
	entities  []byte // raw entity string
}

// build serialises a buildSpec into a valid BSP byte stream. Returns
// the bytes + total size.
func build(s buildSpec) ([]byte, int64) {
	v := s.version
	if v == 0 {
		v = Version29
	}
	body := &bytes.Buffer{}
	// Place each lump sequentially after the header.
	type placed struct {
		off, sz int32
	}
	put := func(b []byte) placed {
		p := placed{off: int32(headerSize) + int32(body.Len()), sz: int32(len(b))}
		body.Write(b)
		return p
	}

	encVec := func(w *bytes.Buffer, x, y, z float32) {
		_ = binary.Write(w, binary.LittleEndian, x)
		_ = binary.Write(w, binary.LittleEndian, y)
		_ = binary.Write(w, binary.LittleEndian, z)
	}

	// Entities lump first (free-form bytes).
	ent := put(s.entities)

	// Vertexes.
	vb := &bytes.Buffer{}
	for _, v := range s.vertices {
		encVec(vb, v.X, v.Y, v.Z)
	}
	verts := put(vb.Bytes())

	// Edges (bsp29: 2x uint16).
	eb := &bytes.Buffer{}
	for _, e := range s.edges {
		_ = binary.Write(eb, binary.LittleEndian, e.V0)
		_ = binary.Write(eb, binary.LittleEndian, e.V1)
	}
	edges := put(eb.Bytes())

	// Surfedges (int32).
	sb := &bytes.Buffer{}
	for _, se := range s.surfedges {
		_ = binary.Write(sb, binary.LittleEndian, int32(se))
	}
	surfedges := put(sb.Bytes())

	// Planes.
	pb := &bytes.Buffer{}
	for _, p := range s.planes {
		encVec(pb, p.Normal[0], p.Normal[1], p.Normal[2])
		_ = binary.Write(pb, binary.LittleEndian, p.Dist)
		_ = binary.Write(pb, binary.LittleEndian, p.Type)
	}
	planes := put(pb.Bytes())

	// Models.
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

	// Assemble header + body.
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
		}
		_ = binary.Write(hdr, binary.LittleEndian, l.off)
		_ = binary.Write(hdr, binary.LittleEndian, l.sz)
	}
	out := append(hdr.Bytes(), body.Bytes()...)
	return out, int64(len(out))
}

func TestOpen_MinimalValid(t *testing.T) {
	raw, sz := build(buildSpec{
		vertices:  []Vertex{{1, 2, 3}, {-4, -5, -6}},
		edges:     []Edge{{V0: 0, V1: 1}},
		surfedges: []Surfedge{1, -1},
		planes: []Plane{{
			Normal: [3]float32{1, 0, 0}, Dist: 5, Type: PlaneX,
		}},
		models: []Model{{
			Mins:     [3]float32{-16, -16, -32},
			Maxs:     [3]float32{16, 16, 32},
			Headnode: [MaxMapHulls]int32{0, 1, 2, 3},
			VisLeafs: 42, FirstFace: 5, NumFaces: 9,
		}},
		entities: []byte("{ \"classname\" \"worldspawn\" }\x00"),
	})
	f, err := Open(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	if f.Header.Version != Version29 {
		t.Errorf("Version: %d", f.Header.Version)
	}
	if got := f.Entities(); !bytes.HasPrefix(got, []byte("{ \"classname\"")) {
		t.Errorf("Entities: %q", got)
	}
	verts, err := f.Vertexes()
	if err != nil || len(verts) != 2 || verts[0].X != 1 || verts[1].Z != -6 {
		t.Errorf("Vertexes: %v %v", err, verts)
	}
	edges, err := f.Edges()
	if err != nil || len(edges) != 1 || edges[0].V1 != 1 {
		t.Errorf("Edges: %v %v", err, edges)
	}
	ses, err := f.Surfedges()
	if err != nil || len(ses) != 2 || ses[0] != 1 || ses[1] != -1 {
		t.Errorf("Surfedges: %v %v", err, ses)
	}
	planes, err := f.Planes()
	if err != nil || len(planes) != 1 || planes[0].Dist != 5 {
		t.Errorf("Planes: %v %v", err, planes)
	}
	models, err := f.Models()
	if err != nil || len(models) != 1 || models[0].VisLeafs != 42 || models[0].Headnode[3] != 3 {
		t.Errorf("Models: %v %v", err, models)
	}
}

func TestOpen_NilSrc(t *testing.T) {
	if _, err := Open(nil, 0); err == nil {
		t.Error("expected nil-src error")
	}
}

func TestOpen_SizeTooSmall(t *testing.T) {
	if _, err := Open(bytes.NewReader([]byte{1, 2}), 2); !errors.Is(err, ErrShortRead) {
		t.Errorf("got %v want ErrShortRead", err)
	}
}

type errReader struct{}

func (errReader) ReadAt([]byte, int64) (int, error) { return 0, errors.New("io fail") }

func TestOpen_ReadFails(t *testing.T) {
	if _, err := Open(errReader{}, 200); err == nil {
		t.Error("expected io error")
	}
}

type shortReader struct{}

func (shortReader) ReadAt(p []byte, _ int64) (int, error) { return len(p) / 2, io.EOF }

func TestOpen_ShortRead(t *testing.T) {
	if _, err := Open(shortReader{}, 200); !errors.Is(err, ErrShortRead) {
		t.Errorf("got %v want ErrShortRead", err)
	}
}

func TestOpen_BadVersion(t *testing.T) {
	raw, sz := build(buildSpec{version: 30})
	if _, err := Open(bytes.NewReader(raw), sz); !errors.Is(err, ErrBadVersion) {
		t.Errorf("got %v want ErrBadVersion", err)
	}
}

func TestOpen_LumpOutOfRange(t *testing.T) {
	raw, sz := build(buildSpec{vertices: []Vertex{{1, 2, 3}}})
	// Patch Vertexes lump fileofs to point past EOF.
	off := 4 + int(LumpVertexes)*lumpEntrySize
	binary.LittleEndian.PutUint32(raw[off:off+4], 1<<30)
	if _, err := Open(bytes.NewReader(raw), sz); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

func TestOpen_NegativeLumpFields(t *testing.T) {
	raw, sz := build(buildSpec{})
	// Patch entities lump fileofs to a negative value.
	off := 4 + int(LumpEntities)*lumpEntrySize
	binary.LittleEndian.PutUint32(raw[off:off+4], 0xFFFFFFFF) // -1
	binary.LittleEndian.PutUint32(raw[off+4:off+8], 16)       // non-empty len
	if _, err := Open(bytes.NewReader(raw), sz); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

// --- Misalignment in each typed decoder ------------------------------------

// patchLumpLen rewrites the FileLen of one lump to a value that
// breaks alignment for that lump's typed decoder.
func patchLumpLen(raw []byte, k LumpKind, newLen int32) {
	off := 4 + int(k)*lumpEntrySize + 4
	binary.LittleEndian.PutUint32(raw[off:off+4], uint32(newLen))
}

func TestVertexes_Misaligned(t *testing.T) {
	raw, sz := build(buildSpec{vertices: []Vertex{{1, 2, 3}}})
	patchLumpLen(raw, LumpVertexes, vertexSize-1)
	f, err := Open(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Vertexes(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v want ErrSectionMisaligned", err)
	}
}

func TestEdges_Misaligned(t *testing.T) {
	raw, sz := build(buildSpec{edges: []Edge{{V0: 1, V1: 2}}})
	patchLumpLen(raw, LumpEdges, edgeSize-1)
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.Edges(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

func TestSurfedges_Misaligned(t *testing.T) {
	raw, sz := build(buildSpec{surfedges: []Surfedge{1}})
	patchLumpLen(raw, LumpSurfedges, surfedgeSize-1)
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.Surfedges(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

func TestPlanes_Misaligned(t *testing.T) {
	raw, sz := build(buildSpec{planes: []Plane{{Type: PlaneX}}})
	patchLumpLen(raw, LumpPlanes, planeSize-1)
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.Planes(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

func TestModels_Misaligned(t *testing.T) {
	raw, sz := build(buildSpec{models: []Model{{VisLeafs: 1}}})
	patchLumpLen(raw, LumpModels, modelSize-1)
	f, _ := Open(bytes.NewReader(raw), sz)
	if _, err := f.Models(); !errors.Is(err, ErrSectionMisaligned) {
		t.Errorf("got %v", err)
	}
}

// --- checkSection edge cases ------------------------------------------------

func TestCheckSection_Empty(t *testing.T) {
	if err := checkSection(0, 0, 0, 4); err != nil {
		t.Errorf("empty section should be valid: %v", err)
	}
}

func TestCheckSection_NegativeArgs(t *testing.T) {
	if err := checkSection(1024, 0, -1, 4); !errors.Is(err, ErrSectionOutOfRange) {
		t.Error("negative length should fail")
	}
}

func TestCheckSection_OfsPastEnd(t *testing.T) {
	if err := checkSection(100, 200, 1, 4); !errors.Is(err, ErrSectionOutOfRange) {
		t.Error("ofs > fileSize should fail")
	}
}

// --- Empty BSP (only header) decodes to zero-length lumps -------------------

func TestEmptyBSP_DecodesToEmptySlices(t *testing.T) {
	raw, sz := build(buildSpec{})
	f, err := Open(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.Vertexes()
	if err != nil || len(v) != 0 {
		t.Errorf("Vertexes empty: %v len=%d", err, len(v))
	}
	if e, err := f.Edges(); err != nil || len(e) != 0 {
		t.Errorf("Edges empty: %v len=%d", err, len(e))
	}
	if s, err := f.Surfedges(); err != nil || len(s) != 0 {
		t.Errorf("Surfedges empty: %v len=%d", err, len(s))
	}
	if p, err := f.Planes(); err != nil || len(p) != 0 {
		t.Errorf("Planes empty: %v len=%d", err, len(p))
	}
	if m, err := f.Models(); err != nil || len(m) != 0 {
		t.Errorf("Models empty: %v len=%d", err, len(m))
	}
	if e := f.Entities(); len(e) != 0 {
		t.Errorf("Entities empty: len=%d", len(e))
	}
}

// Sanity: round-trip a float that's tricky for naive int casts.
func TestPlanes_FloatPrecision(t *testing.T) {
	want := float32(math.Pi)
	raw, sz := build(buildSpec{planes: []Plane{{Normal: [3]float32{want, 0, 0}, Dist: -want, Type: PlaneAnyX}}})
	f, _ := Open(bytes.NewReader(raw), sz)
	planes, _ := f.Planes()
	if planes[0].Normal[0] != want || planes[0].Dist != -want {
		t.Errorf("float precision: %+v want %v", planes[0], want)
	}
}

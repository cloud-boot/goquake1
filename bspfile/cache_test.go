// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bspfile

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"unsafe"
)

// fullBSP returns a BSP with at least one element in every typed
// lump so the cache tests can verify pointer-equality via &slice[0].
func fullBSP(t *testing.T) *File {
	t.Helper()
	raw, sz := buildExt(buildSpecExt{
		buildSpec: buildSpec{
			vertices:  []Vertex{{1, 2, 3}, {4, 5, 6}},
			edges:     []Edge{{V0: 0, V1: 1}},
			surfedges: []Surfedge{1, -1},
			planes:    []Plane{{Normal: [3]float32{1, 0, 0}, Dist: 5, Type: PlaneX}},
			models:    []Model{{Mins: [3]float32{-16, -16, -32}, Maxs: [3]float32{16, 16, 32}, VisLeafs: 1}},
		},
		nodes:        []Node{{PlaneNum: 1, Children: [2]int16{1, -2}, NumFaces: 1}},
		clipnodes:    []ClipNode{{PlaneNum: 1, Children: [2]int16{-2, -3}}},
		leafs:        []Leaf{{Contents: ContentsSolid, VisOfs: -1, NumMarkSurfaces: 1}},
		faces:        []Face{{PlaneNum: 0, NumEdges: 3}},
		texinfos:     []TexInfo{{Vecs: [2][4]float32{{1, 0, 0, 0}, {0, 1, 0, 0}}}},
		marksurfaces: []MarkSurface{0},
		textures:     encodeMipTexLump([]MipTex{{Name: "x", Width: 1, Height: 1}}, nil),
	})
	f, err := Open(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestCache_RepeatedCallsSameBackingArray verifies every typed-lump
// accessor returns the SAME backing array on subsequent calls -- the
// cache is doing its job. We compare &slice[0] (the address of the
// underlying element-0 storage); identical pointers mean no
// re-decode + no re-allocation.
func TestCache_RepeatedCallsSameBackingArray(t *testing.T) {
	f := fullBSP(t)

	// Each entry: a name + a thunk that returns the address of the
	// element-0 storage on each call. The thunks deliberately
	// re-invoke the accessor every time so we measure live cache
	// behaviour, not a captured slice.
	cases := []struct {
		name string
		ptr  func() uintptr
	}{
		{"Vertexes", func() uintptr {
			s, _ := f.Vertexes()
			return uintptr(unsafe.Pointer(&s[0]))
		}},
		{"Edges", func() uintptr {
			s, _ := f.Edges()
			return uintptr(unsafe.Pointer(&s[0]))
		}},
		{"Surfedges", func() uintptr {
			s, _ := f.Surfedges()
			return uintptr(unsafe.Pointer(&s[0]))
		}},
		{"Planes", func() uintptr {
			s, _ := f.Planes()
			return uintptr(unsafe.Pointer(&s[0]))
		}},
		{"Models", func() uintptr {
			s, _ := f.Models()
			return uintptr(unsafe.Pointer(&s[0]))
		}},
		{"Nodes", func() uintptr {
			s, _ := f.Nodes()
			return uintptr(unsafe.Pointer(&s[0]))
		}},
		{"ClipNodes", func() uintptr {
			s, _ := f.ClipNodes()
			return uintptr(unsafe.Pointer(&s[0]))
		}},
		{"Leafs", func() uintptr {
			s, _ := f.Leafs()
			return uintptr(unsafe.Pointer(&s[0]))
		}},
		{"Faces", func() uintptr {
			s, _ := f.Faces()
			return uintptr(unsafe.Pointer(&s[0]))
		}},
		{"TexInfos", func() uintptr {
			s, _ := f.TexInfos()
			return uintptr(unsafe.Pointer(&s[0]))
		}},
		{"MarkSurfaces", func() uintptr {
			s, _ := f.MarkSurfaces()
			return uintptr(unsafe.Pointer(&s[0]))
		}},
		{"Textures", func() uintptr {
			mtl, _ := f.Textures()
			return uintptr(unsafe.Pointer(mtl))
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			first := c.ptr()
			for i := 0; i < 3; i++ {
				if got := c.ptr(); got != first {
					t.Fatalf("%s: call %d returned a different backing address: first=0x%x got=0x%x (cache not honoured)",
						c.name, i+1, first, got)
				}
			}
		})
	}
}

// TestCache_ErrorIsNotCached -- a decode error from the first call
// must be returned, but the entry must NOT be cached: a follow-up
// call decodes again. We exercise this by mutating the underlying raw
// bytes BETWEEN calls so the same accessor sees a misaligned lump on
// call 1 and a valid lump on call 2.
func TestCache_ErrorIsNotCached(t *testing.T) {
	// Build a valid BSP with a populated Vertexes lump.
	raw, sz := build(buildSpec{vertices: []Vertex{{1, 2, 3}}})
	// Patch the Vertexes lump length to a misaligned value.
	origOff := 4 + int(LumpVertexes)*lumpEntrySize + 4
	origLen := uint32(vertexSize)
	patchLumpLen(raw, LumpVertexes, vertexSize-1)

	f, err := Open(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}

	// Call 1: misalignment -> error, must NOT be cached.
	if _, err := f.Vertexes(); !errors.Is(err, ErrSectionMisaligned) {
		t.Fatalf("call 1: want ErrSectionMisaligned, got %v", err)
	}

	// Repair the raw bytes (f.raw aliases raw via Open, so this
	// flips the live lump back to valid). Restore the correct len.
	// Open copies src into its own []byte, so we have to write into
	// f.raw via the LumpBytes view's underlying array; instead,
	// re-build a fresh File around repaired bytes -- the point of
	// the test is whether the SAME *File retries on next call, so
	// we fix the cache's input by patching the header via raw + f.
	// Since Open copies, we patch f.raw directly through the
	// accessor's view: rebuild the file from raw with the length
	// repaired.
	patchLumpLen(raw, LumpVertexes, int32(origLen))
	// Re-Open to obtain a fresh internal copy with the repaired
	// header. We then transplant the repaired raw + header onto
	// the original *File so the cache state from call 1 is
	// preserved (no cache, since the first decode failed).
	f2, err := Open(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	f.raw = f2.raw
	f.Header = f2.Header
	_ = origOff

	// Call 2: cache miss (no entry was stored) -> decode succeeds.
	got, err := f.Vertexes()
	if err != nil {
		t.Fatalf("call 2 (retry): want success, got %v", err)
	}
	if len(got) != 1 || got[0] != (Vertex{1, 2, 3}) {
		t.Fatalf("call 2 returned wrong data: %+v", got)
	}

	// Call 3: now cached -- same backing array.
	got2, _ := f.Vertexes()
	if &got[0] != &got2[0] {
		t.Fatalf("call 3: cache not populated after successful retry (different backing arrays)")
	}
}

// TestCache_ConcurrentReadsRaceFree fires N goroutines through every
// accessor in parallel. With -race the mutex coverage is verified by
// the runtime detector; without -race we still get a basic
// reads-don't-corrupt sanity check via slice-len comparison after the
// barrier.
func TestCache_ConcurrentReadsRaceFree(t *testing.T) {
	f := fullBSP(t)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			// Hit every accessor; cycle through them so the cache
			// is built up from many directions at once.
			if v, err := f.Vertexes(); err != nil || len(v) != 2 {
				t.Errorf("Vertexes: %v len=%d", err, len(v))
			}
			if e, err := f.Edges(); err != nil || len(e) != 1 {
				t.Errorf("Edges: %v len=%d", err, len(e))
			}
			if s, err := f.Surfedges(); err != nil || len(s) != 2 {
				t.Errorf("Surfedges: %v len=%d", err, len(s))
			}
			if p, err := f.Planes(); err != nil || len(p) != 1 {
				t.Errorf("Planes: %v len=%d", err, len(p))
			}
			if m, err := f.Models(); err != nil || len(m) != 1 {
				t.Errorf("Models: %v len=%d", err, len(m))
			}
			if n, err := f.Nodes(); err != nil || len(n) != 1 {
				t.Errorf("Nodes: %v len=%d", err, len(n))
			}
			if c, err := f.ClipNodes(); err != nil || len(c) != 1 {
				t.Errorf("ClipNodes: %v len=%d", err, len(c))
			}
			if l, err := f.Leafs(); err != nil || len(l) != 1 {
				t.Errorf("Leafs: %v len=%d", err, len(l))
			}
			if fs, err := f.Faces(); err != nil || len(fs) != 1 {
				t.Errorf("Faces: %v len=%d", err, len(fs))
			}
			if ti, err := f.TexInfos(); err != nil || len(ti) != 1 {
				t.Errorf("TexInfos: %v len=%d", err, len(ti))
			}
			if ms, err := f.MarkSurfaces(); err != nil || len(ms) != 1 {
				t.Errorf("MarkSurfaces: %v len=%d", err, len(ms))
			}
			if mtl, err := f.Textures(); err != nil || mtl == nil || mtl.NumMipTex != 1 {
				t.Errorf("Textures: %v %+v", err, mtl)
			}
		}()
	}
	wg.Wait()
}

// TestCache_AllAccessorsErrorPath drives the (return nil, err) branch
// of every cached accessor by patching each lump's length to a
// misaligned value before the first call. Without this each accessor
// keeps an uncovered "decode error -> return nil, err" line.
func TestCache_AllAccessorsErrorPath(t *testing.T) {
	type acc struct {
		name string
		k    LumpKind
		bad  int32
		call func(*File) error
	}
	cases := []acc{
		// Vertexes/Edges/Surfedges/Planes/Models error paths are
		// already covered by the per-decoder misalignment tests in
		// bspfile_test.go; they also drive the cache's error
		// return because the accessors share a single mutex'd
		// path. We re-cover them here only for the rendering
		// lumps that were not previously exercised through the
		// cache wrapper.
		{"Nodes", LumpNodes, nodeSize - 1, func(f *File) error {
			_, err := f.Nodes()
			return err
		}},
		{"ClipNodes", LumpClipnodes, clipnodeSize - 1, func(f *File) error {
			_, err := f.ClipNodes()
			return err
		}},
		{"Leafs", LumpLeafs, leafSize - 1, func(f *File) error {
			_, err := f.Leafs()
			return err
		}},
		{"Faces", LumpFaces, faceSize - 1, func(f *File) error {
			_, err := f.Faces()
			return err
		}},
		{"TexInfos", LumpTexInfo, texInfoSize - 1, func(f *File) error {
			_, err := f.TexInfos()
			return err
		}},
		{"MarkSurfaces", LumpMarksurfaces, marksurfaceSize + 1, func(f *File) error {
			_, err := f.MarkSurfaces()
			return err
		}},
		{"Textures", LumpTextures, 2, func(f *File) error {
			_, err := f.Textures()
			return err
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, sz := buildExt(buildSpecExt{
				nodes:        []Node{{}},
				clipnodes:    []ClipNode{{}},
				leafs:        []Leaf{{}},
				faces:        []Face{{}},
				texinfos:     []TexInfo{{}},
				marksurfaces: []MarkSurface{1, 2},
				textures:     []byte{1, 2}, // <4 bytes for Textures
			})
			// For non-Textures lumps, patch length to misaligned.
			// Textures gets its short-header set above.
			if c.k != LumpTextures {
				patchLumpLen(raw, c.k, c.bad)
			}
			f, err := Open(bytes.NewReader(raw), sz)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.call(f); !errors.Is(err, ErrSectionMisaligned) {
				t.Errorf("%s: want ErrSectionMisaligned, got %v", c.name, err)
			}
		})
	}
}

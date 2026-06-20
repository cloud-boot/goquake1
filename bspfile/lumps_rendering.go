// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bspfile

import (
	"encoding/binary"
	"math"
)

// Renderer-coupled bsp29 lump decoders. Only the bsp29 variant is
// implemented here; the BSP2 / BSP2RMQ extensions (uint32 indices +
// float bbox in Node / Leaf / Face) wait for the renderer port that
// actually traverses these structures.

// On-wire constants for the new lumps.
const (
	nodeSize       = 24 // bsp29_dnode_t  (planenum + 2*int16 + 6*int16 + 2*uint16)
	clipnodeSize   = 8  // bsp29_dclipnode_t (int32 planenum + 2*int16 children)
	leafSize       = 28 // bsp29_dleaf_t (contents + visofs + 6*int16 + 2*uint16 + 4*byte)
	faceSize       = 20 // bsp29_dface_t (planenum + side + firstedge + numedges + texinfo + 4*byte + lightofs)
	texInfoSize    = 40 // texinfo_t (2*4*float + miptex + flags)
	marksurfaceSize = 2 // uint16
)

// Node is one bsp29_dnode_t. Children[i] >= 0 indexes Nodes
// recursively; Children[i] < 0 means -(leafnum+1) -- the special
// "negative encodes a leaf" trick. tyrquake: bsp29_dnode_t.
type Node struct {
	PlaneNum  int32
	Children  [2]int16
	Mins      [3]int16
	Maxs      [3]int16
	FirstFace uint16
	NumFaces  uint16
}

// ClipNode is one bsp29_dclipnode_t. The simpler tree used by player
// + monster collision (3 hulls per map). Children encoding is the
// same negative-trick + CONTENTS_* sentinels. tyrquake:
// bsp29_dclipnode_t.
type ClipNode struct {
	PlaneNum int32
	Children [2]int16
}

// Leaf is one bsp29_dleaf_t. Contains a contents-tag (CONTENTS_*),
// a vis-data offset (-1 = no visibility info), a culling bbox, a
// span into the Marksurfaces lump, and four ambient-sound levels.
// tyrquake: bsp29_dleaf_t.
type Leaf struct {
	Contents         int32
	VisOfs           int32
	Mins             [3]int16
	Maxs             [3]int16
	FirstMarkSurface uint16
	NumMarkSurfaces  uint16
	AmbientLevel     [NumAmbients]byte
}

// Face is one bsp29_dface_t. Spans an entry into the Surfedges lump
// (FirstEdge..FirstEdge+NumEdges), references a TexInfo, carries up
// to MaxLightmaps lightstyle bytes + a lightmap-data offset.
// tyrquake: bsp29_dface_t.
type Face struct {
	PlaneNum  int16
	Side      int16
	FirstEdge int32
	NumEdges  int16
	TexInfo   int16
	Styles    [MaxLightmaps]byte
	LightOfs  int32
}

// TexInfo is one texinfo_t. The two 4-component vectors define the
// (s,t) texture-space basis (X*x + Y*y + Z*z + W). MipTex indexes
// the MipTexLump table; Flags is the TexSpecial bit.
// tyrquake: texinfo_t.
type TexInfo struct {
	Vecs    [2][4]float32
	MipTex  int32
	Flags   int32
}

// MarkSurface is one uint16 face index. tyrquake doesn't give it a
// dedicated typedef; we wrap it so the Surfedge-vs-MarkSurface
// distinction stays type-safe.
type MarkSurface uint16

// Nodes decodes LUMP_NODES.
func (f *File) Nodes() ([]Node, error) {
	raw := f.LumpBytes(LumpNodes)
	if len(raw)%nodeSize != 0 {
		return nil, ErrSectionMisaligned
	}
	n := len(raw) / nodeSize
	out := make([]Node, n)
	for i := 0; i < n; i++ {
		off := i * nodeSize
		out[i].PlaneNum = int32(binary.LittleEndian.Uint32(raw[off : off+4]))
		out[i].Children[0] = int16(binary.LittleEndian.Uint16(raw[off+4 : off+6]))
		out[i].Children[1] = int16(binary.LittleEndian.Uint16(raw[off+6 : off+8]))
		for j := 0; j < 3; j++ {
			out[i].Mins[j] = int16(binary.LittleEndian.Uint16(raw[off+8+j*2 : off+10+j*2]))
		}
		for j := 0; j < 3; j++ {
			out[i].Maxs[j] = int16(binary.LittleEndian.Uint16(raw[off+14+j*2 : off+16+j*2]))
		}
		out[i].FirstFace = binary.LittleEndian.Uint16(raw[off+20 : off+22])
		out[i].NumFaces = binary.LittleEndian.Uint16(raw[off+22 : off+24])
	}
	return out, nil
}

// ClipNodes decodes LUMP_CLIPNODES.
func (f *File) ClipNodes() ([]ClipNode, error) {
	raw := f.LumpBytes(LumpClipnodes)
	if len(raw)%clipnodeSize != 0 {
		return nil, ErrSectionMisaligned
	}
	n := len(raw) / clipnodeSize
	out := make([]ClipNode, n)
	for i := 0; i < n; i++ {
		off := i * clipnodeSize
		out[i].PlaneNum = int32(binary.LittleEndian.Uint32(raw[off : off+4]))
		out[i].Children[0] = int16(binary.LittleEndian.Uint16(raw[off+4 : off+6]))
		out[i].Children[1] = int16(binary.LittleEndian.Uint16(raw[off+6 : off+8]))
	}
	return out, nil
}

// Leafs decodes LUMP_LEAFS.
func (f *File) Leafs() ([]Leaf, error) {
	raw := f.LumpBytes(LumpLeafs)
	if len(raw)%leafSize != 0 {
		return nil, ErrSectionMisaligned
	}
	n := len(raw) / leafSize
	out := make([]Leaf, n)
	for i := 0; i < n; i++ {
		off := i * leafSize
		out[i].Contents = int32(binary.LittleEndian.Uint32(raw[off : off+4]))
		out[i].VisOfs = int32(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
		for j := 0; j < 3; j++ {
			out[i].Mins[j] = int16(binary.LittleEndian.Uint16(raw[off+8+j*2 : off+10+j*2]))
		}
		for j := 0; j < 3; j++ {
			out[i].Maxs[j] = int16(binary.LittleEndian.Uint16(raw[off+14+j*2 : off+16+j*2]))
		}
		out[i].FirstMarkSurface = binary.LittleEndian.Uint16(raw[off+20 : off+22])
		out[i].NumMarkSurfaces = binary.LittleEndian.Uint16(raw[off+22 : off+24])
		copy(out[i].AmbientLevel[:], raw[off+24:off+24+NumAmbients])
	}
	return out, nil
}

// Faces decodes LUMP_FACES.
func (f *File) Faces() ([]Face, error) {
	raw := f.LumpBytes(LumpFaces)
	if len(raw)%faceSize != 0 {
		return nil, ErrSectionMisaligned
	}
	n := len(raw) / faceSize
	out := make([]Face, n)
	for i := 0; i < n; i++ {
		off := i * faceSize
		out[i].PlaneNum = int16(binary.LittleEndian.Uint16(raw[off : off+2]))
		out[i].Side = int16(binary.LittleEndian.Uint16(raw[off+2 : off+4]))
		out[i].FirstEdge = int32(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
		out[i].NumEdges = int16(binary.LittleEndian.Uint16(raw[off+8 : off+10]))
		out[i].TexInfo = int16(binary.LittleEndian.Uint16(raw[off+10 : off+12]))
		copy(out[i].Styles[:], raw[off+12:off+12+MaxLightmaps])
		out[i].LightOfs = int32(binary.LittleEndian.Uint32(raw[off+16 : off+20]))
	}
	return out, nil
}

// TexInfos decodes LUMP_TEXINFO.
func (f *File) TexInfos() ([]TexInfo, error) {
	raw := f.LumpBytes(LumpTexInfo)
	if len(raw)%texInfoSize != 0 {
		return nil, ErrSectionMisaligned
	}
	n := len(raw) / texInfoSize
	out := make([]TexInfo, n)
	for i := 0; i < n; i++ {
		off := i * texInfoSize
		for vec := 0; vec < 2; vec++ {
			for c := 0; c < 4; c++ {
				inner := off + vec*16 + c*4
				out[i].Vecs[vec][c] = math.Float32frombits(binary.LittleEndian.Uint32(raw[inner : inner+4]))
			}
		}
		out[i].MipTex = int32(binary.LittleEndian.Uint32(raw[off+32 : off+36]))
		out[i].Flags = int32(binary.LittleEndian.Uint32(raw[off+36 : off+40]))
	}
	return out, nil
}

// MarkSurfaces decodes LUMP_MARKSURFACES (a flat array of uint16
// face indices the leafs slice into).
func (f *File) MarkSurfaces() ([]MarkSurface, error) {
	raw := f.LumpBytes(LumpMarksurfaces)
	if len(raw)%marksurfaceSize != 0 {
		return nil, ErrSectionMisaligned
	}
	n := len(raw) / marksurfaceSize
	out := make([]MarkSurface, n)
	for i := 0; i < n; i++ {
		out[i] = MarkSurface(binary.LittleEndian.Uint16(raw[i*2 : i*2+2]))
	}
	return out, nil
}

// --- Variable-length lumps ---------------------------------------------------

// MipTexLump is the LUMP_TEXTURES decoded form: a directory table
// followed by N miptex records of varying size. The directory's
// OffsetsToMipTex[i] is a byte offset INTO the lump's start, so
// callers index back via f.LumpBytes(LumpTextures)[OffsetsToMipTex[i]]
// or use the convenience MipTex(i) below.
type MipTexLump struct {
	NumMipTex int32
	Offsets   []int32 // length == NumMipTex; -1 = "no miptex at this slot"
	raw       []byte  // backing slice for MipTex resolution
}

// MipTex is one decoded miptex_t entry. Width + Height are in
// pixels; Offsets[i] is the byte offset to mipmap level i (0 = full
// resolution, 1 = half, 2 = quarter, 3 = eighth) RELATIVE to the
// start of this miptex_t record. tyrquake: miptex_t.
type MipTex struct {
	Name    string // 16-byte slot, NUL-trimmed
	Width   uint32
	Height  uint32
	Offsets [MipLevels]uint32
}

// Textures decodes LUMP_TEXTURES into its directory table. The
// per-miptex blocks are accessed via (*MipTexLump).MipTex(i).
func (f *File) Textures() (*MipTexLump, error) {
	raw := f.LumpBytes(LumpTextures)
	if len(raw) == 0 {
		return &MipTexLump{}, nil
	}
	if len(raw) < 4 {
		return nil, ErrSectionMisaligned
	}
	n := int32(binary.LittleEndian.Uint32(raw[0:4]))
	if n < 0 {
		return nil, ErrSectionMisaligned
	}
	expected := 4 + int(n)*4
	if expected > len(raw) {
		return nil, ErrSectionMisaligned
	}
	offs := make([]int32, n)
	for i := int32(0); i < n; i++ {
		off := 4 + int(i)*4
		offs[i] = int32(binary.LittleEndian.Uint32(raw[off : off+4]))
	}
	return &MipTexLump{
		NumMipTex: n,
		Offsets:   offs,
		raw:       raw,
	}, nil
}

// MipTex returns the miptex_t at index i, or nil + false when the
// slot's offset is -1 (the upstream's "missing texture" sentinel).
// Errors out when i is out of range or the offset points past the
// lump.
func (m *MipTexLump) MipTex(i int) (*MipTex, bool, error) {
	if i < 0 || i >= len(m.Offsets) {
		return nil, false, ErrSectionOutOfRange
	}
	off := m.Offsets[i]
	if off == -1 {
		return nil, false, nil // missing-texture sentinel
	}
	if off < 0 || int(off)+40 > len(m.raw) {
		return nil, false, ErrSectionOutOfRange
	}
	end := off + 16
	name := string(m.raw[off:end])
	for j := int32(0); j < 16; j++ {
		if m.raw[off+j] == 0 {
			name = string(m.raw[off : off+j])
			break
		}
	}
	w := binary.LittleEndian.Uint32(m.raw[off+16 : off+20])
	h := binary.LittleEndian.Uint32(m.raw[off+20 : off+24])
	mt := &MipTex{Name: name, Width: w, Height: h}
	for j := 0; j < MipLevels; j++ {
		inner := off + 24 + int32(j)*4
		mt.Offsets[j] = binary.LittleEndian.Uint32(m.raw[inner : inner+4])
	}
	return mt, true, nil
}

// --- Opaque blob lumps -------------------------------------------------------

// Visibility returns the raw LUMP_VISIBILITY bytes (an opaque
// run-length-encoded PVS blob the renderer's leaf-to-leaf
// visibility query decompresses on demand).
func (f *File) Visibility() []byte { return f.LumpBytes(LumpVisibility) }

// Lighting returns the raw LUMP_LIGHTING bytes (a flat array of
// lightmap-sample bytes the renderer indexes via Face.LightOfs).
func (f *File) Lighting() []byte { return f.LumpBytes(LumpLighting) }

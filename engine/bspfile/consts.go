// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package bspfile

// BSP version magic. Only Version29 is parsed by this package; the
// BSP2 / BSP2RMQ extensions land alongside the renderer port.
const (
	Version29     = 29
	Version2      = ('B' | ('S' << 8) | ('P' << 16) | ('2' << 24))
	Version2RMQ   = (('B' << 24) | ('S' << 16) | ('P' << 8) | '2')
)

// LumpKind indexes a header slot. tyrquake: LUMP_*.
type LumpKind int

const (
	LumpEntities LumpKind = iota
	LumpPlanes
	LumpTextures
	LumpVertexes
	LumpVisibility
	LumpNodes
	LumpTexInfo
	LumpFaces
	LumpLighting
	LumpClipnodes
	LumpLeafs
	LumpMarksurfaces
	LumpEdges
	LumpSurfedges
	LumpModels
	HeaderLumps = 15
)

// Map design bounds taken verbatim from tyrquake's bspfile.h. These
// are caps the BSP compiler enforces; the loader validates lump
// counts against them so a corrupted file can't trigger absurd
// allocations.
const (
	MaxMapHulls         = 4
	MaxMapModels        = 256
	MaxMapBrushes       = 4096
	MaxMapEntities      = 1024
	MaxMapEntString     = 65536
	MaxMapPlanes        = 16384
	MaxMapNodes         = 32767
	MaxMapClipnodes     = 32767
	MaxMapLeafs         = 32767
	MaxMapVerts         = 65535
	MaxMapFaces         = 65535
	MaxMapMarksurfaces  = 65535
	MaxMapTexInfo       = 4096
	MaxMapEdges         = 256000
	MaxMapSurfedges     = 512000
	MaxMapTextures      = 512
	MaxMapMipTex        = 0x200000
	MaxMapLighting      = 0x100000
	MaxMapVisibility    = 0x100000
)

// Plane type tags. 0..2 are axially-aligned planes (the fast path);
// 3..5 are non-axial planes snapped to the nearest axis. tyrquake:
// PLANE_X..PLANE_ANYZ.
const (
	PlaneX    = 0
	PlaneY    = 1
	PlaneZ    = 2
	PlaneAnyX = 3
	PlaneAnyY = 4
	PlaneAnyZ = 5
)

// Contents tags carried by leafs + clip-tree negative children.
// tyrquake: CONTENTS_*. Values stay byte-equal to the C upstream so
// negative-child encoding (-content) in nodes still works.
const (
	ContentsEmpty     = -1
	ContentsSolid     = -2
	ContentsWater     = -3
	ContentsSlime     = -4
	ContentsLava      = -5
	ContentsSky       = -6
	ContentsOrigin    = -7
	ContentsClip      = -8
	ContentsCurrent0   = -9
	ContentsCurrent90  = -10
	ContentsCurrent180 = -11
	ContentsCurrent270 = -12
	ContentsCurrentUp  = -13
	ContentsCurrentDn  = -14
)

// TexSpecial set in texinfo_t.flags marks sky / liquid surfaces that
// the renderer skips lightmap + 256-subdivision for. tyrquake:
// TEX_SPECIAL.
const TexSpecial = 1

// MipLevels is the fixed mip count stored in a miptex_t. tyrquake:
// MIPLEVELS.
const MipLevels = 4

// MaxLightmaps is the per-face lightstyle slot count. tyrquake:
// MAXLIGHTMAPS.
const MaxLightmaps = 4

// Automatic ambient channels carried by every leaf. tyrquake:
// AMBIENT_* + NUM_AMBIENTS.
const (
	AmbientWater = 0
	AmbientSky   = 1
	AmbientSlime = 2
	AmbientLava  = 3
	NumAmbients  = 4
)

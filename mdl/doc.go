// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package mdl reads id Software's .mdl alias-model file format. The
// engine container for player + monster + weapon meshes: a single
// shared triangle list animated by per-frame vertex positions, with
// an 8-bit-indexed skin texture wrapped via (s,t) tex-coords stored
// alongside the verts.
//
// Upstream pin:
//   github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// On-disk layout (little-endian throughout):
//
//   mdl_t header (84 bytes)
//     ident          "IDPO"
//     version        6
//     scale          vec3 (per-axis quantisation scale for byte-packed verts)
//     scale_origin   vec3 (per-axis origin for byte-packed verts)
//     boundingradius float32
//     eyeposition    vec3
//     numskins       int32
//     skinwidth      int32
//     skinheight     int32
//     numverts       int32
//     numtris        int32
//     numframes      int32
//     synctype       int32 (ST_SYNC or ST_RAND)
//     flags          int32 (engine-specific flag bits)
//     size           float32
//
//   then in order:
//
//   numskins skin records, each prefixed by a 4-byte type tag:
//     SkinSingle: skinwidth * skinheight bytes (raw 8-bit indexed)
//     SkinGroup:  int32 N + N intervals (float32) + N single skins
//
//   numverts stvert_t records (12 bytes each: onseam int32 + s + t)
//
//   numtris dtriangle_t records (16 bytes each: facesfront int32 +
//     3 vertindex int32)
//
//   numframes frame records, each prefixed by a 4-byte type tag:
//     FrameSingle: daliasframe_t = bboxmin trivertx_t (4) +
//                  bboxmax trivertx_t (4) + name[16] + numverts
//                  trivertx_t (4 bytes each)
//     FrameGroup:  int32 N + bboxmin trivertx_t + bboxmax trivertx_t
//                  + N intervals (float32) + N single-frame records
//
//   trivertx_t is 4 bytes: 3 byte coords (quantised: real_coord =
//   coord * header.Scale + header.ScaleOrigin) + 1 byte
//   lightnormalindex into the engine's 162-entry anorms table.
//
// Vertex positions are stored as bytes (3 per vert per frame); the
// renderer multiplies by mdl_t.Scale and adds ScaleOrigin to
// reconstruct floats. This compression buys ~3x over storing floats
// directly -- a 200-vertex model with 60 frames is ~24 KiB instead
// of 72 KiB.
package mdl

// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package synthbsp builds tiny, valid BSP byte streams in memory so
// production code + tests can exercise the bspfile / model / bsprender
// stack without shipping pak0.pak data. Each builder returns a raw
// byte slice + its size so callers can wrap it in [bytes.NewReader]
// and hand it to [bspfile.Open].
//
// Two canonical layouts ship today:
//
//   - [BuildFiveLeafPVS]: a depth-3 BSP with 5 leaves + a 4-byte PVS
//     row per leaf. Minimal-but-valid; used for vis/walk smoke tests.
//   - [BuildWithFaces] / [BuildWithFacesCustomTextures]: a BSP with
//     4 faces wired up so every rendering accessor (Faces / Edges /
//     Surfedges / Vertexes / TexInfos / Textures / Planes / Nodes /
//     Leafs) returns non-empty, decodable data. Used by the
//     quake-tamago demo path and end-to-end rendering tests.
//
// These builders were promoted verbatim from bsprender's test code;
// the only behavioural change is the *testing.T dependency was
// replaced with error returns so production code can call them.
package synthbsp

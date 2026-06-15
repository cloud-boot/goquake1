// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package vfs is the Go port of tyrquake's search-path layer
// (com_searchpaths + COM_AddGameDirectory + COM_FindFile in
// common/common.c). The C version chains together raw directory
// listings and PAK archives via a singly-linked list of
// searchpath_t; the Go form wraps any sequence of [io/fs.FS] sources
// into a SearchPath value whose Open walks them first-hit-wins.
//
// Upstream pin:
//   github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// Override semantics: tyrquake adds new sources to the HEAD of the
// search list so they override earlier ones (the canonical pak1 over
// pak0 chain). SearchPath.Add preserves this -- it prepends, matching
// the tyrquake call shape `pack->next = com_searchpaths;
// com_searchpaths = search;`.
//
// Composition: a typical bare-metal probe builds the search path as
//
//   sp := vfs.New()
//   sp.Add(embedpak.PakFS)         // shareware pak0.pak in-tree
//   if userPak != nil {
//       sp.Add(userPak)            // operator's pak override
//   }
//   eng := engine.Run(sp, ...)
//
// and the harvest tool on the host wraps an os.DirFS in the equivalent
// shape. The engine itself never imports os; the FS abstraction does
// all the bridging.
package vfs

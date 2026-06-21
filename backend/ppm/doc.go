// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package ppm is a Backend implementation that writes each presented
// RGBA frame to disk as a binary-PPM (P6) file via a caller-supplied
// WriterFactory. It is the simplest backend that produces artifacts a
// human can open in any image viewer to visually verify the engine
// rendered something.
//
// PPM (binary, P6) is intentionally trivial:
//
//	"P6\n<width> <height>\n255\n"   ASCII header
//	<width*height*3 bytes>          raw RGB triplets (the A is
//	                                stripped from the engine's RGBA)
//
// PresentFrame writes one PPM per call; the caller chooses naming +
// container via WriterFactory. A convenience NumberedFileFactory
// opens "<prefix><N>.<ext>" with N zero-padded to a fixed width, so
// the typical recipe is:
//
//	b, _ := ppm.New(320, 200, ppm.NumberedFileFactory(
//	    "/tmp/quake_frame_", "ppm", 4))
//	host.RunFrame(b, ...)   // writes /tmp/quake_frame_0000.ppm
//
// Audio is captured in-memory (so tests may inspect it); Input
// always returns an empty snapshot; Now() is a deterministic
// monotonic-tick clock (TickIncrement seconds per call), so test
// timing is reproducible.
//
// tyrquake: this is what the C tree's vid_ppm.c / sw video targets
// would look like, collapsed to one ~200-line Go package.
package ppm

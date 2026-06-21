// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package ascii is a terminal ASCII-art Backend implementation. Each
// PresentFrame converts the RGBA frame to a luminance grid +
// downsamples to a target character resolution + writes the resulting
// ASCII characters to an io.Writer.
//
// Useful for headless visual verification without a graphical display
// available: a tail -f on the output file shows the engine actually
// producing frames + the rough shape of what's on screen.
//
// Resolution: configurable per call via NewBackend(cols, rows). At
// 80x25 a 320x200 input frame downsamples to 4x8 pixel blocks per
// character.
package ascii

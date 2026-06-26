// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package wasm

// mouseButtonCode maps a DOM MouseEvent.button value to the
// synthetic code MapDOMKey understands. Returns "" for buttons the
// engine does not bind.
//
// Per the DOM spec:
//
//	0 = primary (usually left)
//	1 = auxiliary (usually wheel click)
//	2 = secondary (usually right)
//
// Kept in a pure-Go file (no build tag) so the lookup table is unit-
// testable on host platforms without a JS runtime.
func mouseButtonCode(btn int) string {
	switch btn {
	case 0:
		return "Mouse1"
	case 2:
		return "Mouse2"
	default:
		return ""
	}
}

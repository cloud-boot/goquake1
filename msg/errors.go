// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package msg

import "errors"

// ErrControlHeaderNoSpace is returned by WriteControlHeader when the
// buffer is shorter than 4 bytes -- the upstream MSG_WriteControlHeader
// writes past the buffer end silently; the Go port surfaces it
// instead so the caller can decide whether to panic via sys.Error or
// extend the buffer first.
var ErrControlHeaderNoSpace = errors.New("msg: control-header write needs 4 byte slot")

// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sizebuf

import "errors"

// Buffer is the Go port of tyrquake's sizebuf_t (include/common.h):
// a fixed-capacity byte writer backed by a caller-provided slice.
// msg.MSG_Write* (next port) layers wire encoders on top of it.
//
// Field-by-field correspondence:
//
//	C qboolean allowoverflow -> AllowOverflow (exported)
//	C qboolean overflowed    -> overflowed    (read via Overflowed)
//	C byte    *data          -> data          (slice header)
//	C int     maxsize        -> implicit cap(data)
//	C int     cursize        -> cursize
//
// maxsize is not stored separately because cap(data) carries it; this
// matches the upstream invariant that maxsize never changes after
// SZ_HunkAlloc.
type Buffer struct {
	// AllowOverflow, when true, makes GetSpace truncate the buffer
	// (Clear + set Overflowed) instead of returning an error on a
	// request that would exceed the backing capacity. Mirrors
	// sizebuf_t.allowoverflow.
	AllowOverflow bool

	overflowed bool
	data       []byte
	cursize    int
}

// ErrSizeBufOverflow is returned by [Buffer.GetSpace] when the request
// would exceed the backing capacity and AllowOverflow is false. The
// upstream C calls Sys_Error in this case; the Go port surfaces the
// condition so callers can panic via [sys.Error] or recover at will.
// tyrquake SZ_GetSpace: "overflow without allowoverflow set".
var ErrSizeBufOverflow = errors.New("sizebuf: overflow without allowoverflow set")

// ErrSizeBufRequestTooLarge is returned by [Buffer.GetSpace] when a
// single request exceeds the backing capacity. tyrquake SZ_GetSpace:
// "%d is > full buffer size". Distinguished from ErrSizeBufOverflow
// because even AllowOverflow=true cannot satisfy such a request.
var ErrSizeBufRequestTooLarge = errors.New("sizebuf: request larger than full buffer")

// New wraps a caller-provided backing slice. The slice's length is
// reset to zero and its capacity becomes the buffer's maxsize.
// Replaces tyrquake SZ_HunkAlloc: the caller does the hunk allocation
// upstream (the port's allocator boundary is in [engine/zone]) and
// passes the resulting slice in. tyrquake clamps maxsize to >= 16;
// New does not, because the caller is the one sizing the hunk.
func New(buf []byte) *Buffer {
	return &Buffer{data: buf[:cap(buf)][:0:cap(buf)]}
}

// Clear resets the write cursor to zero and clears the overflowed
// flag. The backing slice is not zeroed (tyrquake doesn't zero
// either; consumers always advance cursize before reading). tyrquake:
// SZ_Clear.
func (b *Buffer) Clear() {
	b.cursize = 0
	b.overflowed = false
}

// GetSpace reserves length bytes at the current cursor and returns a
// slice aliasing that region. The returned slice is valid until the
// next Clear or GetSpace call. tyrquake: SZ_GetSpace.
//
// Behaviour on overflow (matching the upstream check order):
//
//   - cursize+length <= cap(data): allocates and returns.
//   - cursize+length > cap(data) and !AllowOverflow:
//     ErrSizeBufOverflow; the buffer is not modified. Mirrors the
//     upstream Sys_Error("overflow without allowoverflow set"), which
//     fires before the size check.
//   - cursize+length > cap(data) and AllowOverflow and
//     length > cap(data): ErrSizeBufRequestTooLarge; the buffer is
//     not modified. Mirrors Sys_Error("%d is > full buffer size").
//   - cursize+length > cap(data) and AllowOverflow and
//     length <= cap(data): the buffer is Clear()ed, Overflowed()
//     flips true, and the request is re-satisfied from offset 0.
func (b *Buffer) GetSpace(length int) ([]byte, error) {
	if b.cursize+length > cap(b.data) {
		if !b.AllowOverflow {
			return nil, ErrSizeBufOverflow
		}
		if length > cap(b.data) {
			return nil, ErrSizeBufRequestTooLarge
		}
		b.Clear()
		b.overflowed = true
	}
	start := b.cursize
	b.cursize += length
	b.data = b.data[:b.cursize]
	return b.data[start:b.cursize], nil
}

// Write appends p verbatim. tyrquake: SZ_Write (which is just a
// memcpy(SZ_GetSpace(buf, length), data, length)).
func (b *Buffer) Write(p []byte) error {
	dst, err := b.GetSpace(len(p))
	if err != nil {
		return err
	}
	copy(dst, p)
	return nil
}

// Print appends s as a C string -- s's bytes followed by a single NUL
// terminator. tyrquake: SZ_Print, including its consecutive-print
// quirk:
//
//	If the buffer is non-empty AND the byte at cursize-1 is already
//	NUL (i.e. the previous Print's terminator is still there), then
//	the new string overwrites that NUL with its first byte and
//	contributes only len(s) net bytes (its own NUL takes the slot the
//	previous one occupied). Otherwise len(s)+1 bytes are appended.
//
// This matters because demo and network streams depend on the exact
// byte count -- a naive "always append NUL" would shift every following
// MSG_Write* by one byte and break parity.
func (b *Buffer) Print(s string) error {
	n := len(s)
	reuse := b.cursize > 0 && b.data[b.cursize-1] == 0
	if !reuse {
		dst, err := b.GetSpace(n + 1)
		if err != nil {
			return err
		}
		copy(dst, s)
		dst[n] = 0
		return nil
	}
	// Reuse-branch: the buffer ends in a NUL from a previous Print,
	// so the new contribution is only n bytes (the new NUL takes the
	// prior NUL's slot). Empty input is a no-op.
	if n == 0 {
		return nil
	}
	// Determine whether the n-byte reservation will overflow the
	// backing store. If it does and AllowOverflow=true, the upstream
	// SZ_Print semantics break down (the previous NUL slot vanishes
	// when GetSpace truncates), so degrade to a fresh-branch
	// reservation of n+1 bytes against the (about to be truncated)
	// buffer.
	if b.cursize+n > cap(b.data) {
		// "n+1 itself exceeds cap" -- the request never fits the
		// backing store regardless of AllowOverflow. Surface the
		// structural sentinel before the soft-overflow check so the
		// caller can distinguish "would have fit if cleared" from
		// "never fits".
		if n+1 > cap(b.data) {
			return ErrSizeBufRequestTooLarge
		}
		if !b.AllowOverflow {
			return ErrSizeBufOverflow
		}
		// GetSpace cannot fail here: we just guarded `n+1 <= cap`, and
		// the AllowOverflow path will Clear before satisfying the
		// request. Treat the error path as an invariant violation.
		dst, _ := b.GetSpace(n + 1)
		copy(dst, s)
		dst[n] = 0
		return nil
	}
	prev := b.cursize
	// `cursize+n <= cap` is established by the outer check; GetSpace
	// cannot fail.
	_, _ = b.GetSpace(n)
	// Stable reuse path: overwrite data[prev-1 : prev-1+n] with s
	// and place the new NUL at data[prev-1+n] == data[b.cursize-1].
	b.data[prev-1] = s[0]
	copy(b.data[prev:b.cursize], s[1:])
	b.data[b.cursize-1] = 0
	return nil
}

// Overflowed reports whether GetSpace has had to discard buffered
// bytes due to an AllowOverflow=true overflow since the last Clear.
// tyrquake: read of sizebuf_t.overflowed.
func (b *Buffer) Overflowed() bool { return b.overflowed }

// Bytes returns the buffered bytes (the data[:cursize] view).
// The returned slice aliases the backing store; do not retain it
// across Clear or further writes. tyrquake equivalent: reading
// (buf->data, buf->cursize).
func (b *Buffer) Bytes() []byte { return b.data[:b.cursize] }

// Len returns the number of buffered bytes. tyrquake: sizebuf_t.cursize.
func (b *Buffer) Len() int { return b.cursize }

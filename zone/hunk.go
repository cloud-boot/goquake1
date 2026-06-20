// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package zone

import "encoding/binary"

// Hunk allocator constants. Preserved bit-exact from tyrquake zone.c.
const (
	// HunkSentinel guards each hunk header against trashing by an
	// over-running write to the preceding allocation. tyrquake:
	// HUNK_SENTINAL.
	HunkSentinel = 0x1df001ed

	// HunkNameLen is the per-allocation tag-name length used by the
	// `hunk print` debug command. tyrquake: HUNK_NAMELEN.
	HunkNameLen = 8

	// hunkAlign is the allocation granularity ("(size + 15) & ~15").
	hunkAlign = 16

	// hunkHeaderSize is sizeof(hunk_t): {sentinal int32, size int32,
	// name [8]byte} = 16. Already aligned to hunkAlign so the payload
	// that follows is naturally 16-byte-aligned.
	hunkHeaderSize = 16
)

// Hunk is the stack-style arena with independent low/high marks plus
// a temp-allocation slot at the high end. tyrquake: hunkstate.
type Hunk struct {
	buf       []byte
	lowbytes  int // bump pointer from the low end
	highbytes int // bump pointer from the high end (grows downward)
	tempmark  int // saved highbytes mark from the most recent AllocTemp;
	// 0 means "no temp allocation outstanding"
	peak int // historical peak of lowbytes+highbytes (for diagnostics)
}

type hunkHeader struct {
	sentinel int32
	size     int32
	name     [HunkNameLen]byte
}

func (h *Hunk) readHunkHeader(off int) hunkHeader {
	var hh hunkHeader
	hh.sentinel = int32(binary.LittleEndian.Uint32(h.buf[off : off+4]))
	hh.size = int32(binary.LittleEndian.Uint32(h.buf[off+4 : off+8]))
	copy(hh.name[:], h.buf[off+8:off+8+HunkNameLen])
	return hh
}

func (h *Hunk) writeHunkHeader(off int, hh hunkHeader) {
	binary.LittleEndian.PutUint32(h.buf[off:off+4], uint32(hh.sentinel))
	binary.LittleEndian.PutUint32(h.buf[off+4:off+8], uint32(hh.size))
	// Always zero the name field first, then copy up to HunkNameLen
	// bytes (tyrquake's `memset + memcpy(qmin(strlen(name), NAMELEN))`).
	for i := 0; i < HunkNameLen; i++ {
		h.buf[off+8+i] = 0
	}
	copy(h.buf[off+8:off+8+HunkNameLen], hh.name[:])
}

func copyName(name string) [HunkNameLen]byte {
	var n [HunkNameLen]byte
	for i := 0; i < HunkNameLen && i < len(name); i++ {
		n[i] = name[i]
	}
	return n
}

// NewHunk wraps buf as a Hunk arena. The buffer must be at least
// hunkHeaderSize bytes; smaller arenas can't host even an empty
// allocation and subsequent Alloc calls will panic with "hunk
// exhausted" via the C-parity Sys_Error path. tyrquake: Memory_Init
// (hunk portion only).
func NewHunk(buf []byte) *Hunk {
	return &Hunk{buf: buf}
}

// Reset returns the hunk to its post-NewHunk state: both marks at 0,
// no temp allocation outstanding. The backing buffer is not touched.
func (h *Hunk) Reset() {
	h.lowbytes = 0
	h.highbytes = 0
	h.tempmark = 0
	h.peak = 0
}

func (h *Hunk) updatePeak() {
	used := h.lowbytes + h.highbytes
	if used > h.peak {
		h.peak = used
	}
}

// Peak returns the historical maximum of lowbytes+highbytes since the
// last Reset. tyrquake: hunkstate.peak (read-only accessor).
func (h *Hunk) Peak() int { return h.peak }

// AllocLow reserves size bytes at the low end of the hunk, prefixed by
// a 16-byte header tagged with `name` (truncated to HunkNameLen).
// Returns a zero-filled slice. Panics with "hunk: low exhausted" if
// the arena lacks room. tyrquake: Hunk_AllocName.
func (h *Hunk) AllocLow(size int, name string) []byte {
	if size < 0 {
		panic("hunk: bad size")
	}
	total := hunkHeaderSize + ((size + hunkAlign - 1) &^ (hunkAlign - 1))
	if len(h.buf)-h.lowbytes-h.highbytes < total {
		panic("hunk: low exhausted")
	}

	off := h.lowbytes
	h.writeHunkHeader(off, hunkHeader{
		sentinel: HunkSentinel,
		size:     int32(total),
		name:     copyName(name),
	})
	h.lowbytes += total
	h.updatePeak()

	payloadOff := off + hunkHeaderSize
	payload := h.buf[payloadOff : payloadOff+size]
	for i := range payload {
		payload[i] = 0
	}
	return payload
}

// AllocHigh reserves size bytes at the high end of the hunk. Returns
// nil (rather than panicking) if there is no room: upstream's
// Hunk_HighAllocName returns NULL too, leaving the failure handling
// to the caller (typically the video subsystem). tyrquake:
// Hunk_HighAllocName.
func (h *Hunk) AllocHigh(size int, name string) []byte {
	if size < 0 {
		panic("hunk: bad size")
	}
	// A pending temp allocation must be discarded before any non-temp
	// high allocation, otherwise Cache_FreeHigh would walk into it.
	if h.tempmark != 0 {
		tm := h.tempmark
		h.tempmark = 0
		h.FreeToHighMark(tm)
	}

	total := hunkHeaderSize + ((size + hunkAlign - 1) &^ (hunkAlign - 1))
	if len(h.buf)-h.lowbytes-h.highbytes < total {
		return nil
	}

	h.highbytes += total
	off := len(h.buf) - h.highbytes

	// Match the C path: zero the header region then write the metadata.
	for i := 0; i < total; i++ {
		h.buf[off+i] = 0
	}
	h.writeHunkHeader(off, hunkHeader{
		sentinel: HunkSentinel,
		size:     int32(total),
		name:     copyName(name),
	})
	h.updatePeak()

	payloadOff := off + hunkHeaderSize
	return h.buf[payloadOff : payloadOff+size]
}

// AllocTemp grabs size bytes at the high end and records the prior
// high-mark so the next AllocTemp/AllocHigh can roll it back. The
// returned slice is valid only until the next high-end activity.
// tyrquake: Hunk_TempAlloc.
func (h *Hunk) AllocTemp(size int) []byte {
	if size < 0 {
		panic("hunk: bad size")
	}
	size = (size + hunkAlign - 1) &^ (hunkAlign - 1)

	if h.tempmark != 0 {
		tm := h.tempmark
		h.tempmark = 0
		h.FreeToHighMark(tm)
	}

	mark := h.HighMark()
	buf := h.AllocHigh(size, "temp")
	if buf == nil {
		return nil
	}
	h.tempmark = mark
	return buf
}

// LowMark returns the current low-end watermark. Passing this value
// to FreeToLowMark later will roll back every AllocLow performed in
// between. tyrquake: Hunk_LowMark.
func (h *Hunk) LowMark() int { return h.lowbytes }

// FreeToLowMark rolls the low end back to mark, discarding every
// AllocLow performed since mark was taken. Out-of-range marks panic
// (tyrquake Sys_Error parity). tyrquake: Hunk_FreeToLowMark.
func (h *Hunk) FreeToLowMark(mark int) {
	if mark < 0 || mark > h.lowbytes {
		panic("hunk: bad low mark")
	}
	// Zero the reclaimed region so a subsequent AllocLow doesn't see
	// stale header data that would confuse Check().
	for i := mark; i < h.lowbytes; i++ {
		h.buf[i] = 0
	}
	h.lowbytes = mark
}

// HighMark returns the current high-end watermark, discarding any
// outstanding temp allocation first (tyrquake parity). tyrquake:
// Hunk_HighMark.
func (h *Hunk) HighMark() int {
	if h.tempmark != 0 {
		tm := h.tempmark
		h.tempmark = 0
		h.FreeToHighMark(tm)
	}
	return h.highbytes
}

// FreeToHighMark rolls the high end back to mark. tyrquake:
// Hunk_FreeToHighMark.
func (h *Hunk) FreeToHighMark(mark int) {
	if h.tempmark != 0 {
		tm := h.tempmark
		h.tempmark = 0
		h.FreeToHighMark(tm)
	}
	if mark < 0 || mark > h.highbytes {
		panic("hunk: bad high mark")
	}
	base := len(h.buf) - h.highbytes
	for i := 0; i < h.highbytes-mark; i++ {
		h.buf[base+i] = 0
	}
	h.highbytes = mark
}

// CheckHunk walks the low and high stacks and verifies every header's
// sentinel + size. tyrquake: Hunk_Check.
func (h *Hunk) CheckHunk() error {
	if len(h.buf) == 0 {
		return nil
	}
	endlow := h.lowbytes
	endhigh := len(h.buf)
	starthigh := endhigh - h.highbytes

	off := 0
	for {
		if off == endlow {
			off = starthigh
		}
		if off == endhigh {
			break
		}
		hh := h.readHunkHeader(off)
		if hh.sentinel != HunkSentinel {
			return errHunkSentinel
		}
		if hh.size < hunkHeaderSize || off+int(hh.size) > endhigh {
			return errHunkSize
		}
		off += int(hh.size)
	}
	return nil
}

// errHunkSentinel / errHunkSize are returned by CheckHunk. They are
// unexported because callers compare via errors.Is; the public surface
// is "did Check pass or not".
var (
	errHunkSentinel = newHunkErr("hunk: trashed sentinel")
	errHunkSize     = newHunkErr("hunk: bad size")
)

type hunkErr string

func (e hunkErr) Error() string { return string(e) }
func newHunkErr(s string) error { return hunkErr(s) }

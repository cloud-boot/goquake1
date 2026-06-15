// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package zone

import (
	"encoding/binary"
	"errors"
	"unsafe"
)

// Zone allocator constants. Preserved bit-exact from tyrquake zone.c
// so the on-arena layout is binary-compatible with anyone reading a
// dumped arena image.
const (
	// ZoneID is the per-block sentinel ("ZONEID" in zone.c). It guards
	// against double-free and stray-pointer free.
	ZoneID = 0x1d4a11

	// MinFragment is the smallest tail-fragment Z_TagMalloc will leave
	// as a free block. Tails smaller than this are absorbed into the
	// allocation to avoid fragmentation. tyrquake: MINFRAGMENT.
	MinFragment = 64

	// zoneAlign is the allocation granularity (and block-header
	// alignment) used by tyrquake's `size = (size + 7) & ~7`.
	zoneAlign = 8

	// blockHeaderSize is the size of memblock_t laid out as
	// {id, size, tag, pad, next, prev} where pointers are stored as
	// int32 offsets into the backing buffer. 4*6 = 24, padded to the
	// 8-byte alignment quantum -> 24.
	blockHeaderSize = 24

	// zoneHeaderSize is the size of memzone_t laid out as
	// {size, blocklist (blockHeader), rover (offset)} = 4 + 24 + 4 = 32.
	zoneHeaderSize = 32

	// blocklistOffset is where the sentinel cap block lives inside the
	// backing buffer (right after the 4-byte `size` field).
	blocklistOffset = 4

	// nilOffset is the sentinel "no such block" offset. Real block
	// offsets are always >= 0 and < len(buf), so -1 cannot collide.
	nilOffset = -1
)

// Errors returned by the Zone allocator. tyrquake's C version calls
// Sys_Error() and aborts; the Go port returns explicit errors so the
// host loop (which is still being ported) can choose its own policy.
var (
	ErrZoneOOM        = errors.New("zone: out of memory")
	ErrZoneNilFree    = errors.New("zone: free of nil pointer")
	ErrZoneBadFree    = errors.New("zone: free of pointer without ZONEID")
	ErrZoneDoubleFree = errors.New("zone: double free")
	ErrZoneNotInArena = errors.New("zone: pointer not in this arena")
	ErrZoneBadSize    = errors.New("zone: bad allocation size")
	ErrZoneCorrupt    = errors.New("zone: block list corrupt")
)

// Zone is the free-list allocator. The on-disk layout (matching
// memzone_t + memblock_t in zone.c) is stored inside the caller-
// supplied buffer; the Zone struct only holds a reference to that
// buffer. tyrquake: memzone_t.
type Zone struct {
	buf []byte
}

// blockHeader mirrors C `memblock_t`. Fields are packed at offsets
// {0, 4, 8, 12, 16, 20} in the backing buffer. Pointers (next, prev)
// are stored as signed int32 byte-offsets relative to the start of
// the buffer rather than absolute addresses; this keeps the arena
// position-independent (it can be relocated whole, dumped, restored).
type blockHeader struct {
	id   int32 // ZoneID sentinel; 0 on the cap block
	size int32 // total block size including the 24-byte header
	tag  int32 // 0 = free; nonzero = in use
	pad  int32 // alignment pad (kept for layout parity)
	next int32 // byte offset of next block
	prev int32 // byte offset of previous block
}

func (z *Zone) readHeader(off int) blockHeader {
	b := z.buf[off:]
	return blockHeader{
		id:   int32(binary.LittleEndian.Uint32(b[0:4])),
		size: int32(binary.LittleEndian.Uint32(b[4:8])),
		tag:  int32(binary.LittleEndian.Uint32(b[8:12])),
		pad:  int32(binary.LittleEndian.Uint32(b[12:16])),
		next: int32(binary.LittleEndian.Uint32(b[16:20])),
		prev: int32(binary.LittleEndian.Uint32(b[20:24])),
	}
}

func (z *Zone) writeHeader(off int, h blockHeader) {
	b := z.buf[off:]
	binary.LittleEndian.PutUint32(b[0:4], uint32(h.id))
	binary.LittleEndian.PutUint32(b[4:8], uint32(h.size))
	binary.LittleEndian.PutUint32(b[8:12], uint32(h.tag))
	binary.LittleEndian.PutUint32(b[12:16], uint32(h.pad))
	binary.LittleEndian.PutUint32(b[16:20], uint32(h.next))
	binary.LittleEndian.PutUint32(b[20:24], uint32(h.prev))
}

func (z *Zone) setZoneSize(n int32) {
	binary.LittleEndian.PutUint32(z.buf[0:4], uint32(n))
}

func (z *Zone) rover() int {
	// The rover offset is stored as the last 4 bytes of the zone header
	// (after the cap block).
	return int(int32(binary.LittleEndian.Uint32(z.buf[blocklistOffset+blockHeaderSize : blocklistOffset+blockHeaderSize+4])))
}

func (z *Zone) setRover(off int) {
	binary.LittleEndian.PutUint32(z.buf[blocklistOffset+blockHeaderSize:blocklistOffset+blockHeaderSize+4], uint32(int32(off)))
}

// offsetOf returns the block-header offset of an Alloc-returned user
// slice. Exposed for whitebox tests that need to force the rover onto
// a specific block; not part of the public API.
func (z *Zone) offsetOf(userSlice []byte) int {
	if len(userSlice) == 0 {
		return -1
	}
	for i := 0; i < len(z.buf); i++ {
		if &z.buf[i] == &userSlice[0] {
			return i - blockHeaderSize
		}
	}
	return -1
}

// NewZone wraps buf as a Zone arena and clears it to one big free
// block. The buffer must be at least zoneHeaderSize + blockHeaderSize
// bytes; smaller arenas can never satisfy even a zero-byte Alloc.
// tyrquake: Z_ClearZone.
func NewZone(buf []byte) *Zone {
	if len(buf) < zoneHeaderSize+blockHeaderSize {
		// Too small to host even a single empty allocation. We still
		// return a Zone so the caller can introspect; subsequent Alloc
		// calls will return ErrZoneOOM.
		return &Zone{buf: buf}
	}
	z := &Zone{buf: buf}
	z.Reset()
	return z
}

// Reset clears the zone to a single free block, discarding all
// previous allocations without touching the backing storage. tyrquake:
// Z_ClearZone (called on a previously-cleared zone).
func (z *Zone) Reset() {
	if len(z.buf) < zoneHeaderSize+blockHeaderSize {
		return
	}
	size := len(z.buf)
	z.setZoneSize(int32(size))

	// First real block sits immediately after the zone header.
	blockOff := zoneHeaderSize
	// Cap "sentinel" block lives at blocklistOffset; its tag is 1 so
	// the rover loop treats it as "in use" and never returns it.
	cap := blockHeader{
		id:   0,
		size: 0,
		tag:  1, // in-use cap
		next: int32(blockOff),
		prev: int32(blockOff),
	}
	z.writeHeader(blocklistOffset, cap)

	// The single huge free block covers the rest of the buffer.
	free := blockHeader{
		id:   ZoneID,
		size: int32(size - zoneHeaderSize),
		tag:  0,
		next: blocklistOffset,
		prev: blocklistOffset,
	}
	z.writeHeader(blockOff, free)
	z.setRover(blockOff)
}

// Alloc returns a size-byte slice from the zone, zero-filled.
// tyrquake: Z_Malloc (= Z_TagMalloc with tag=1 + memset to zero).
func (z *Zone) Alloc(size int) ([]byte, error) {
	if size < 0 {
		return nil, ErrZoneBadSize
	}
	if len(z.buf) < zoneHeaderSize+blockHeaderSize {
		return nil, ErrZoneOOM
	}

	// Round up to alignment + add header.
	totalSize := size + blockHeaderSize
	totalSize = (totalSize + zoneAlign - 1) &^ (zoneAlign - 1)

	// Skip ahead to the first free block from where the rover sits.
	rover := z.rover()
	start := z.readHeader(rover).prev
	for z.readHeader(rover).tag != 0 && int32(rover) != start {
		rover = int(z.readHeader(rover).next)
	}
	z.setRover(rover)

	base := rover
	for {
		baseHdr := z.readHeader(base)
		if int32(base) == start && (baseHdr.tag != 0 || baseHdr.size < int32(totalSize)) {
			// Walked the whole ring without finding a fit.
			return nil, ErrZoneOOM
		}
		if baseHdr.tag != 0 {
			base = int(baseHdr.next)
			continue
		}
		if baseHdr.size < int32(totalSize) {
			base = int(baseHdr.next)
			continue
		}
		break
	}

	baseHdr := z.readHeader(base)
	extra := int(baseHdr.size) - totalSize
	if extra > MinFragment {
		// Split off a free fragment past the new allocation.
		newOff := base + totalSize
		newHdr := blockHeader{
			id:   ZoneID,
			size: int32(extra),
			tag:  0,
			prev: int32(base),
			next: baseHdr.next,
		}
		z.writeHeader(newOff, newHdr)

		nextHdr := z.readHeader(int(baseHdr.next))
		nextHdr.prev = int32(newOff)
		z.writeHeader(int(baseHdr.next), nextHdr)

		baseHdr.next = int32(newOff)
		baseHdr.size = int32(totalSize)
	}

	baseHdr.tag = 1
	baseHdr.id = ZoneID
	z.writeHeader(base, baseHdr)

	// Advance the rover past this allocation so the next Alloc starts
	// scanning further along the ring.
	if base == z.rover() {
		z.setRover(int(baseHdr.next))
	}

	payloadOff := base + blockHeaderSize
	payload := z.buf[payloadOff : payloadOff+size]
	// Z_Malloc returns zero-filled memory; we explicitly clear in case
	// this block previously held a freed allocation's contents.
	for i := range payload {
		payload[i] = 0
	}
	return payload, nil
}

// sliceOffset returns the byte offset of b's underlying-array pointer
// relative to the Zone's backing buffer. Returns -1 if b is not a
// sub-slice of z.buf. The single use of unsafe in this file is
// confined to this address-arithmetic helper; callers never see raw
// pointers.
func (z *Zone) sliceOffset(b []byte) int {
	if len(z.buf) == 0 || len(b) == 0 {
		return -1
	}
	bp := unsafe.SliceData(b)
	zp := unsafe.SliceData(z.buf)
	off := int(uintptr(unsafe.Pointer(bp)) - uintptr(unsafe.Pointer(zp)))
	if off < 0 || off >= len(z.buf) {
		return -1
	}
	return off
}

// Free releases the block whose payload starts at the beginning of b.
// b must be a slice returned by an earlier Alloc on the same Zone.
// tyrquake: Z_Free.
func (z *Zone) Free(b []byte) error {
	if b == nil {
		return ErrZoneNilFree
	}
	payloadOff := z.sliceOffset(b)
	if payloadOff < 0 || payloadOff < blockHeaderSize {
		return ErrZoneNotInArena
	}
	blockOff := payloadOff - blockHeaderSize
	h := z.readHeader(blockOff)
	if h.id != ZoneID {
		return ErrZoneBadFree
	}
	if h.tag == 0 {
		return ErrZoneDoubleFree
	}

	h.tag = 0
	z.writeHeader(blockOff, h)

	// Merge with previous block if it's free.
	prevOff := int(h.prev)
	prev := z.readHeader(prevOff)
	if prev.tag == 0 && prevOff != blocklistOffset {
		prev.size += h.size
		prev.next = h.next
		nx := z.readHeader(int(prev.next))
		nx.prev = int32(prevOff)
		z.writeHeader(int(prev.next), nx)
		if blockOff == z.rover() {
			z.setRover(prevOff)
		}
		z.writeHeader(prevOff, prev)
		blockOff = prevOff
		h = prev
	}

	// Merge with next block if it's free.
	nextOff := int(h.next)
	nx := z.readHeader(nextOff)
	if nx.tag == 0 && nextOff != blocklistOffset {
		h.size += nx.size
		h.next = nx.next
		nn := z.readHeader(int(h.next))
		nn.prev = int32(blockOff)
		z.writeHeader(int(h.next), nn)
		if nextOff == z.rover() {
			z.setRover(blockOff)
		}
		z.writeHeader(blockOff, h)
	}

	// Always start looking from the lowest free block we know of, to
	// keep fragmentation down. tyrquake's "slower, but not too bad"
	// comment in Z_Free.
	if blockOff < z.rover() {
		z.setRover(blockOff)
	}
	return nil
}

// Check walks the block list and verifies the on-arena bookkeeping is
// internally consistent. Useful for tests and the `zone print` debug
// command. tyrquake: Z_CheckHeap (compiled in only under -DDEBUG).
func (z *Zone) Check() error {
	if len(z.buf) < zoneHeaderSize+blockHeaderSize {
		return nil
	}
	cap := z.readHeader(blocklistOffset)
	off := int(cap.next)
	for {
		h := z.readHeader(off)
		if int32(off) != cap.next && off != blocklistOffset && h.id != ZoneID {
			return ErrZoneCorrupt
		}
		if h.next == int32(blocklistOffset) {
			break
		}
		nextOff := int(h.next)
		if off+int(h.size) != nextOff {
			return ErrZoneCorrupt
		}
		nx := z.readHeader(nextOff)
		if nx.prev != int32(off) {
			return ErrZoneCorrupt
		}
		if h.tag == 0 && nx.tag == 0 {
			return ErrZoneCorrupt
		}
		off = nextOff
	}
	return nil
}

// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package msg

import (
	"encoding/binary"
	"math"

	"github.com/cloud-boot/goquake1/engine/sizebuf"
)

// NETFLAG_* are NetQuake unreliable-protocol header bits, hoisted from
// NQ/net.h so this package stays self-contained.
const (
	NETFLAG_LENGTH_MASK = 0x0000ffff
	NETFLAG_DATA        = 0x00010000
	NETFLAG_ACK         = 0x00020000
	NETFLAG_NAK         = 0x00040000
	NETFLAG_EOM         = 0x00080000
	NETFLAG_UNRELIABLE  = 0x00100000
	NETFLAG_CTL         = 0x80000000
)

// --- write side ---------------------------------------------------------------

// WriteChar appends c as a signed 8-bit byte. tyrquake: MSG_WriteChar.
func WriteChar(b *sizebuf.Buffer, c int) error {
	dst, err := b.GetSpace(1)
	if err != nil {
		return err
	}
	dst[0] = byte(c)
	return nil
}

// WriteByte appends c as an unsigned 8-bit byte. tyrquake:
// MSG_WriteByte.
func WriteByte(b *sizebuf.Buffer, c int) error {
	dst, err := b.GetSpace(1)
	if err != nil {
		return err
	}
	dst[0] = byte(c)
	return nil
}

// WriteShort appends c as a little-endian signed 16-bit short.
// tyrquake: MSG_WriteShort.
func WriteShort(b *sizebuf.Buffer, c int) error {
	dst, err := b.GetSpace(2)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint16(dst, uint16(int16(c)))
	return nil
}

// WriteLong appends c as a little-endian signed 32-bit long.
// tyrquake: MSG_WriteLong.
func WriteLong(b *sizebuf.Buffer, c int32) error {
	dst, err := b.GetSpace(4)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(dst, uint32(c))
	return nil
}

// WriteFloat appends f as a little-endian IEEE-754 single-precision
// 4-byte value. tyrquake: MSG_WriteFloat (the C version uses a
// float/int union + LittleLong; the Go port goes through math.Float32
// bits which is endian-portable on the read side).
func WriteFloat(b *sizebuf.Buffer, f float32) error {
	dst, err := b.GetSpace(4)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(dst, math.Float32bits(f))
	return nil
}

// WriteString appends s as a NUL-terminated C string. Empty input
// still writes the lone NUL byte (matches tyrquake's `if (!s)
// SZ_Write(sb,"",1)` branch). tyrquake: MSG_WriteString.
func WriteString(b *sizebuf.Buffer, s string) error {
	buf := make([]byte, len(s)+1)
	copy(buf, s)
	buf[len(s)] = 0
	return b.Write(buf)
}

// WriteCoord appends f as a 16-bit fixed-point coordinate (3 fractional
// bits, so the on-wire range is +/- 4096 units in 0.125 increments).
// tyrquake: MSG_WriteCoord.
func WriteCoord(b *sizebuf.Buffer, f float32) error {
	return WriteShort(b, int(f*(1<<3)))
}

// WriteAngle appends f (degrees) as a single byte (360/256 per step).
// tyrquake: MSG_WriteAngle.
func WriteAngle(b *sizebuf.Buffer, f float32) error {
	v := int(math.Floor(float64(f)*(256.0/360.0)+0.5)) & 0xff
	return WriteByte(b, v)
}

// WriteAngle16 appends f (degrees) as a 16-bit unsigned angle
// (360/65536 per step). tyrquake: MSG_WriteAngle16.
func WriteAngle16(b *sizebuf.Buffer, f float32) error {
	v := int(math.Floor(float64(f)*(65536.0/360.0)+0.5)) & 0xffff
	return WriteShort(b, v)
}

// WriteControlHeader stamps the first 4 bytes of the buffer with the
// NQ unreliable-protocol control header (NETFLAG_CTL OR'd with the
// current cursize), in BIG-endian byte order (the only big-endian
// write in the protocol -- the rest are little-endian). The caller
// must have already reserved the first 4 bytes via SZ_GetSpace.
// tyrquake: MSG_WriteControlHeader. Returns an error when the buffer
// is shorter than 4 bytes.
func WriteControlHeader(b *sizebuf.Buffer) error {
	data := b.Bytes()
	if len(data) < 4 {
		return ErrControlHeaderNoSpace
	}
	c := uint32(NETFLAG_CTL) | (uint32(b.Len()) & NETFLAG_LENGTH_MASK)
	binary.BigEndian.PutUint32(data[:4], c)
	return nil
}

// --- read side ----------------------------------------------------------------

// Reader is the streaming MSG_Read* state replacing tyrquake's
// net_message + msg_readcount + msg_badread globals.
type Reader struct {
	data []byte
	pos  int
	bad  bool
}

// NewReader returns a Reader over data. The caller retains ownership;
// the Reader does not copy.
func NewReader(data []byte) *Reader {
	return &Reader{data: data}
}

// Begin resets the read cursor and the bad-read flag. tyrquake:
// MSG_BeginReading.
func (r *Reader) Begin() {
	r.pos = 0
	r.bad = false
}

// Bad reports whether any read past end-of-data has happened since
// the last Begin(). tyrquake: msg_badread.
func (r *Reader) Bad() bool { return r.bad }

// Pos returns the current read offset. tyrquake: msg_readcount /
// MSG_GetReadCount.
func (r *Reader) Pos() int { return r.pos }

// ReadChar returns the next byte as a signed int (-128..127) and
// advances the cursor; returns -1 and sets Bad on EOF. tyrquake:
// MSG_ReadChar.
func (r *Reader) ReadChar() int {
	if r.pos+1 > len(r.data) {
		r.bad = true
		return -1
	}
	c := int(int8(r.data[r.pos]))
	r.pos++
	return c
}

// ReadByte returns the next byte as an unsigned int (0..255). -1 +
// Bad on EOF. tyrquake: MSG_ReadByte.
func (r *Reader) ReadByte() int {
	if r.pos+1 > len(r.data) {
		r.bad = true
		return -1
	}
	c := int(r.data[r.pos])
	r.pos++
	return c
}

// ReadShort returns the next little-endian int16. -1 + Bad on EOF.
// tyrquake: MSG_ReadShort.
func (r *Reader) ReadShort() int {
	if r.pos+2 > len(r.data) {
		r.bad = true
		return -1
	}
	v := int(int16(binary.LittleEndian.Uint16(r.data[r.pos:])))
	r.pos += 2
	return v
}

// ReadLong returns the next little-endian int32. -1 + Bad on EOF.
// tyrquake: MSG_ReadLong.
func (r *Reader) ReadLong() int32 {
	if r.pos+4 > len(r.data) {
		r.bad = true
		return -1
	}
	v := int32(binary.LittleEndian.Uint32(r.data[r.pos:]))
	r.pos += 4
	return v
}

// ReadFloat returns the next little-endian IEEE-754 single. On EOF,
// returns 0 and sets Bad. tyrquake: MSG_ReadFloat (the upstream is a
// union read that returns garbage past EOF and does NOT set
// msg_badread -- the Go port closes that defect because every other
// Read* surfaces EOF via Bad).
func (r *Reader) ReadFloat() float32 {
	if r.pos+4 > len(r.data) {
		r.bad = true
		return 0
	}
	v := math.Float32frombits(binary.LittleEndian.Uint32(r.data[r.pos:]))
	r.pos += 4
	return v
}

// ReadString reads a NUL-terminated string. Stops at the first 0 byte
// OR at EOF (in which case Bad is set). The terminator is consumed.
// tyrquake: MSG_ReadString. The C version routes through
// COM_GetStrBuf's 2048-byte rotating buffer and silently truncates at
// 2047 chars; the Go port returns whatever fits in memory without an
// implicit cap.
func (r *Reader) ReadString() string {
	start := r.pos
	for {
		if r.pos >= len(r.data) {
			r.bad = true
			return string(r.data[start:r.pos])
		}
		c := r.data[r.pos]
		r.pos++
		if c == 0 {
			return string(r.data[start : r.pos-1])
		}
	}
}

// ReadCoord reads a 16-bit fixed-point coordinate (3 fractional bits).
// tyrquake: MSG_ReadCoord.
func (r *Reader) ReadCoord() float32 {
	return float32(r.ReadShort()) * (1.0 / (1 << 3))
}

// ReadAngle reads a single-byte angle (degrees, 360/256 per step).
// tyrquake: MSG_ReadAngle.
func (r *Reader) ReadAngle() float32 {
	return float32(r.ReadChar()) * (360.0 / 256.0)
}

// ReadAngle16 reads a 16-bit angle (degrees, 360/65536 per step).
// tyrquake: MSG_ReadAngle16.
func (r *Reader) ReadAngle16() float32 {
	return float32(r.ReadShort()) * (360.0 / 65536.0)
}

// ReadControlHeader reads the NQ unreliable-protocol control header
// from the current cursor in BIG-endian byte order. Returns -1 + sets
// Bad on EOF. tyrquake: MSG_ReadControlHeader.
func (r *Reader) ReadControlHeader() int32 {
	if r.pos+4 > len(r.data) {
		r.bad = true
		return -1
	}
	v := int32(binary.BigEndian.Uint32(r.data[r.pos:]))
	r.pos += 4
	return v
}

// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// --- writeSoundNum (per-protocol dispatch) ------------------------------

func TestWriteSoundNum_NQ_OneByte(t *testing.T) {
	buf := sizebuf.New(make([]byte, 16))
	if err := writeSoundNum(buf, 42, 0, protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1 {
		t.Errorf("NQ should write 1 byte, got %d", buf.Len())
	}
	if buf.Bytes()[0] != 42 {
		t.Errorf("NQ byte: got %d want 42", buf.Bytes()[0])
	}
}

func TestWriteSoundNum_BJP_OneByte(t *testing.T) {
	buf := sizebuf.New(make([]byte, 16))
	if err := writeSoundNum(buf, 42, 0, protocol.VersionBJP); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1 {
		t.Errorf("BJP should write 1 byte, got %d", buf.Len())
	}
}

func TestWriteSoundNum_BJP2_TwoBytes(t *testing.T) {
	buf := sizebuf.New(make([]byte, 16))
	if err := writeSoundNum(buf, 500, 0, protocol.VersionBJP2); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 2 {
		t.Errorf("BJP2 should write 2 bytes, got %d", buf.Len())
	}
}

func TestWriteSoundNum_BJP3_TwoBytes(t *testing.T) {
	buf := sizebuf.New(make([]byte, 16))
	if err := writeSoundNum(buf, 500, 0, protocol.VersionBJP3); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 2 {
		t.Errorf("BJP3 should write 2 bytes, got %d", buf.Len())
	}
}

func TestWriteSoundNum_FITZ_ByteByDefault(t *testing.T) {
	buf := sizebuf.New(make([]byte, 16))
	if err := writeSoundNum(buf, 42, 0, protocol.VersionFitz); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1 {
		t.Errorf("FITZ default: got %d want 1 byte", buf.Len())
	}
}

func TestWriteSoundNum_FITZ_ShortWithLargeFlag(t *testing.T) {
	buf := sizebuf.New(make([]byte, 16))
	if err := writeSoundNum(buf, 500, protocol.SndFitzLargeSound, protocol.VersionFitz); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 2 {
		t.Errorf("FITZ + LargeSound: got %d want 2 bytes", buf.Len())
	}
}

func TestWriteSoundNum_UnknownProtoErrors(t *testing.T) {
	buf := sizebuf.New(make([]byte, 16))
	err := writeSoundNum(buf, 42, 0, 99999)
	if !errors.Is(err, ErrSoundProtoUnknown) {
		t.Errorf("got %v want ErrSoundProtoUnknown", err)
	}
}

// --- EncodeSound: input validation --------------------------------------

func TestEncodeSound_NilBufErrors(t *testing.T) {
	if err := EncodeSound(nil, 1, 0, 1, [3]float32{}, 255, 1.0, protocol.VersionNQ); err == nil {
		t.Error("expected nil-buf error")
	}
}

func TestEncodeSound_VolumeOutOfRange(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	for _, v := range []int{-1, 256, 1000} {
		err := EncodeSound(buf, 1, 0, 1, [3]float32{}, v, 1.0, protocol.VersionNQ)
		if !errors.Is(err, ErrSoundVolumeRange) {
			t.Errorf("volume=%d: got %v want ErrSoundVolumeRange", v, err)
		}
	}
}

func TestEncodeSound_AttenOutOfRange(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	for _, a := range []float32{-0.1, 4.1, 100} {
		err := EncodeSound(buf, 1, 0, 1, [3]float32{}, 255, a, protocol.VersionNQ)
		if !errors.Is(err, ErrSoundAttenRange) {
			t.Errorf("atten=%v: got %v want ErrSoundAttenRange", a, err)
		}
	}
}

func TestEncodeSound_ChannelNegativeErrors(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	err := EncodeSound(buf, 1, -1, 1, [3]float32{}, 255, 1.0, protocol.VersionNQ)
	if !errors.Is(err, ErrSoundChannelRange) {
		t.Errorf("got %v want ErrSoundChannelRange", err)
	}
}

// --- EncodeSound: protocol gating ---------------------------------------

// entIdx >= 8192 only works on FITZ.
func TestEncodeSound_LargeEntityRejectedOnNQ(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	err := EncodeSound(buf, 10000, 0, 1, [3]float32{}, 255, 1.0, protocol.VersionNQ)
	if !errors.Is(err, ErrSoundProtoUnencodable) {
		t.Errorf("got %v want ErrSoundProtoUnencodable", err)
	}
}

// soundNum >= 256 only works on FITZ (for the LargeSound mask path).
// BJP2/BJP3 always write 2-byte sound -- the LargeSound flag check
// happens BEFORE writeSoundNum, so BJP2/BJP3 with soundNum >= 256 +
// channel < 8 should also be rejected (matches the C "if proto !=
// FITZ return" gate).
func TestEncodeSound_LargeSoundRejectedOnBJP2(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	err := EncodeSound(buf, 1, 0, 500, [3]float32{}, 255, 1.0, protocol.VersionBJP2)
	if !errors.Is(err, ErrSoundProtoUnencodable) {
		t.Errorf("got %v want ErrSoundProtoUnencodable", err)
	}
}

// Channel >= 8 also triggers LargeSound path (gated on FITZ).
func TestEncodeSound_LargeChannelRejectedOnNQ(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	err := EncodeSound(buf, 1, 8, 1, [3]float32{}, 255, 1.0, protocol.VersionNQ)
	if !errors.Is(err, ErrSoundProtoUnencodable) {
		t.Errorf("got %v want ErrSoundProtoUnencodable", err)
	}
}

// --- EncodeSound: happy path roundtrips --------------------------------

// Vanilla NQ: default volume + attenuation -> no SND_VOLUME or
// SND_ATTENUATION bits, minimal 7-byte wire (cmd + mask + entity
// short + sound byte + 3 coords... wait, 1+1+2+1+6 = 11 bytes).
func TestEncodeSound_NQ_DefaultsRoundtrip(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	err := EncodeSound(buf, 5, 3, 7, [3]float32{100, 200, 300}, protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation, protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	// Expected wire: cmd (1) + mask (1) + entity_channel_short (2) + sound (1) + 3*coord (6) = 11
	if buf.Len() != 11 {
		t.Errorf("default NQ wire: got %d bytes want 11", buf.Len())
	}

	r := msg.NewReader(buf.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcSound {
		t.Errorf("cmd: got %d want SvcSound", cmd)
	}
	if mask := r.ReadU8(); mask != 0 {
		t.Errorf("mask: got %d want 0 (defaults)", mask)
	}
	// (ent << 3) | channel = (5 << 3) | 3 = 43
	if pkt := r.ReadShort(); pkt != 43 {
		t.Errorf("ent_channel: got %d want 43", pkt)
	}
	if sn := r.ReadU8(); sn != 7 {
		t.Errorf("sound: got %d want 7", sn)
	}
	for axis, want := range [3]float32{100, 200, 300} {
		if got := r.ReadCoord(); got != want {
			t.Errorf("coord[%d]: got %v want %v", axis, got, want)
		}
	}
}

// Non-default volume + attenuation set both flag bits.
func TestEncodeSound_NQ_NonDefaultsSetFlags(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	err := EncodeSound(buf, 1, 0, 1, [3]float32{}, 128, 2.0, protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8() // cmd
	mask := r.ReadU8()
	if mask&protocol.SndVolume == 0 {
		t.Error("expected SndVolume bit set")
	}
	if mask&protocol.SndAttenuation == 0 {
		t.Error("expected SndAttenuation bit set")
	}
	if vol := r.ReadU8(); vol != 128 {
		t.Errorf("volume: got %d want 128", vol)
	}
	if attn := r.ReadU8(); attn != 128 { // 2.0 * 64 = 128
		t.Errorf("attenuation: got %d want 128", attn)
	}
}

// FITZ + large entity: wire shape becomes (cmd, mask, ent_short,
// channel_byte, sound_byte, 3*coord).
func TestEncodeSound_FITZ_LargeEntityWire(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	err := EncodeSound(buf, 10000, 5, 1, [3]float32{1, 2, 3}, protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation, protocol.VersionFitz)
	if err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8() // cmd
	mask := r.ReadU8()
	if mask&protocol.SndFitzLargeEntity == 0 {
		t.Error("expected LargeEntity flag")
	}
	if ent := r.ReadShort(); ent != 10000 {
		t.Errorf("entity: got %d want 10000", ent)
	}
	if ch := r.ReadU8(); ch != 5 {
		t.Errorf("channel: got %d want 5", ch)
	}
}

// FITZ + large sound: writes 2-byte sound index.
func TestEncodeSound_FITZ_LargeSoundWire(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	err := EncodeSound(buf, 1, 0, 500, [3]float32{}, protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation, protocol.VersionFitz)
	if err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	_ = r.ReadU8() // cmd
	mask := r.ReadU8()
	if mask&protocol.SndFitzLargeSound == 0 {
		t.Error("expected LargeSound flag")
	}
	// Skip the entity+channel short to reach the 2-byte sound index.
	_ = r.ReadShort()
	if sn := r.ReadShort(); sn != 500 {
		t.Errorf("sound: got %d want 500", sn)
	}
}

// --- EncodeSound: overflow + unknown proto -----------------------------

func TestEncodeSound_DatagramFull(t *testing.T) {
	buf := sizebuf.New(make([]byte, MaxDatagram))
	if err := buf.Write(make([]byte, MaxDatagram-soundReserve+1)); err != nil {
		t.Fatal(err)
	}
	err := EncodeSound(buf, 1, 0, 1, [3]float32{}, 255, 1.0, protocol.VersionNQ)
	if !errors.Is(err, ErrDatagramFull) {
		t.Errorf("got %v want ErrDatagramFull", err)
	}
}

func TestEncodeSound_UnknownProtoErrors(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	err := EncodeSound(buf, 1, 0, 1, [3]float32{}, 255, 1.0, 99999)
	if !errors.Is(err, ErrSoundProtoUnknown) {
		t.Errorf("got %v want ErrSoundProtoUnknown", err)
	}
}

// Per-write overflow propagation: walks through each msg.Write*
// site by sizing the buffer just short of the next write.
// Layout for vanilla NQ (default vol/attn): cmd(1) + mask(1) +
// ent_short(2) + sound_byte(1) + 3*coord(6) = 11 bytes.
//
// caps that fail at successive writes: 0 (cmd), 1 (mask), 2 (ent_short),
// 4 (sound), 5+2k (coord k).
func TestEncodeSound_PerWriteOverflowPropagates(t *testing.T) {
	for _, cap := range []int{0, 1, 2, 4, 5, 7, 9} {
		t.Run("", func(t *testing.T) {
			buf := sizebuf.New(make([]byte, cap))
			err := EncodeSound(buf, 1, 0, 1, [3]float32{}, protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation, protocol.VersionNQ)
			if err == nil || errors.Is(err, ErrDatagramFull) {
				t.Errorf("cap=%d: expected propagated write error, got %v", cap, err)
			}
		})
	}
	// Vol + attn flags add 2 bytes; with non-default vol/attn we have:
	// cmd(1) + mask(1) + vol(1) + attn(1) + ent(2) + sound(1) + 3*coord(6) = 13 bytes.
	// caps that fail mid-flag-writes:
	for _, cap := range []int{2, 3} {
		t.Run("", func(t *testing.T) {
			buf := sizebuf.New(make([]byte, cap))
			err := EncodeSound(buf, 1, 0, 1, [3]float32{}, 128, 2.0, protocol.VersionNQ)
			if err == nil || errors.Is(err, ErrDatagramFull) {
				t.Errorf("cap=%d (vol+attn): expected propagated error, got %v", cap, err)
			}
		})
	}
	// FITZ LargeEntity: cmd(1) + mask(1) + ent_short(2) + channel(1) + sound(1) + coord(6) = 12 bytes.
	// caps that fail mid-LargeEntity write:
	for _, cap := range []int{2, 4} {
		t.Run("", func(t *testing.T) {
			buf := sizebuf.New(make([]byte, cap))
			err := EncodeSound(buf, 10000, 0, 1, [3]float32{}, protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation, protocol.VersionFitz)
			if err == nil || errors.Is(err, ErrDatagramFull) {
				t.Errorf("cap=%d (FITZ LargeEntity): expected propagated error, got %v", cap, err)
			}
		})
	}
}

// soundReserve drift detector.
func TestSoundReserve_TyrquakeValue(t *testing.T) {
	if soundReserve != 14 {
		t.Errorf("soundReserve drift: got %d want 14 (tyrquake)", soundReserve)
	}
}

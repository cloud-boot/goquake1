// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"errors"
	"math"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/sizebuf"
)

// newReader is a one-liner to build a *SvcReader over raw bytes.
func newReader(data []byte) *SvcReader {
	return &SvcReader{R: msg.NewReader(data)}
}

// ------- ErrEOF on empty reader ----------------------------------

func TestNext_EmptyReaderReturnsErrEOF(t *testing.T) {
	sr := newReader(nil)
	v, err := sr.Next(protocol.VersionNQ)
	if !errors.Is(err, ErrEOF) {
		t.Errorf("err: got %v want ErrEOF", err)
	}
	if v != nil {
		t.Errorf("value: got %v want nil", v)
	}
}

// ------- ErrUnknownSvc on bogus cmd byte -------------------------

func TestNext_UnknownSvcByte(t *testing.T) {
	// 0x00 (svc_bad) is not in the supported list.
	sr := newReader([]byte{protocol.SvcBad})
	v, err := sr.Next(protocol.VersionNQ)
	if !errors.Is(err, ErrUnknownSvc) {
		t.Errorf("err: got %v want ErrUnknownSvc", err)
	}
	if v != nil {
		t.Errorf("value: got %v want nil", v)
	}
}

// Another opcode the engine has no encoder/decoder for in this commit.
func TestNext_UnknownSvc_Skipped(t *testing.T) {
	// svc_time (= 7) is deferred (server-time stamp; folded into the
	// host's per-frame state by the apply layer, not surfaced as a
	// Decoded value).
	sr := newReader([]byte{protocol.SvcTime})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrUnknownSvc) {
		t.Errorf("err: got %v want ErrUnknownSvc", err)
	}
}

// ------- DecodedNop ----------------------------------------------

func TestNext_Nop_Roundtrip(t *testing.T) {
	buf := sizebuf.New(make([]byte, 8))
	if err := server.EncodeNop(buf); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(DecodedNop); !ok {
		t.Errorf("type: got %T want DecodedNop", v)
	}
}

// ------- DecodedDisconnect ---------------------------------------

func TestNext_Disconnect_Roundtrip(t *testing.T) {
	buf := sizebuf.New(make([]byte, 8))
	if err := server.EncodeDisconnect(buf); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(DecodedDisconnect); !ok {
		t.Errorf("type: got %T want DecodedDisconnect", v)
	}
}

// ------- DecodedKilledMonster ------------------------------------

func TestNext_KilledMonster_Roundtrip(t *testing.T) {
	buf := sizebuf.New(make([]byte, 8))
	if err := server.EncodeKilledMonster(buf); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(DecodedKilledMonster); !ok {
		t.Errorf("type: got %T want DecodedKilledMonster", v)
	}
}

// ------- DecodedFoundSecret --------------------------------------

func TestNext_FoundSecret_Roundtrip(t *testing.T) {
	buf := sizebuf.New(make([]byte, 8))
	if err := server.EncodeFoundSecret(buf); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(DecodedFoundSecret); !ok {
		t.Errorf("type: got %T want DecodedFoundSecret", v)
	}
}

// ------- DecodedIntermission -------------------------------------

func TestNext_Intermission_Roundtrip(t *testing.T) {
	buf := sizebuf.New(make([]byte, 8))
	if err := server.EncodeIntermission(buf); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(DecodedIntermission); !ok {
		t.Errorf("type: got %T want DecodedIntermission", v)
	}
}

// ------- DecodedSellScreen ---------------------------------------

func TestNext_SellScreen_Roundtrip(t *testing.T) {
	buf := sizebuf.New(make([]byte, 8))
	if err := server.EncodeSellScreen(buf); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(DecodedSellScreen); !ok {
		t.Errorf("type: got %T want DecodedSellScreen", v)
	}
}

// ------- DecodedSetView ------------------------------------------

func TestNext_SetView_Roundtrip(t *testing.T) {
	const want = 1234
	buf := sizebuf.New(make([]byte, 8))
	if err := server.EncodeSetView(buf, want); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := v.(DecodedSetView)
	if !ok {
		t.Fatalf("type: got %T want DecodedSetView", v)
	}
	if got.EntityNum != want {
		t.Errorf("EntityNum: got %d want %d", got.EntityNum, want)
	}
}

// SetView with entityNum >= 32768: the wire is unsigned short; the
// decoder must round-trip the value, not sign-extend it.
func TestNext_SetView_UnsignedShortRange(t *testing.T) {
	const want = 0xc001
	buf := sizebuf.New(make([]byte, 8))
	if err := server.EncodeSetView(buf, want); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedSetView)
	if got.EntityNum != want {
		t.Errorf("EntityNum: got %d want %d (unsigned round-trip)", got.EntityNum, want)
	}
}

// Short read inside svc_setview body.
func TestNext_SetView_Truncated(t *testing.T) {
	// One byte: cmd consumed cleanly, short read fails.
	sr := newReader([]byte{protocol.SvcSetView})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedSignonNum ----------------------------------------

func TestNext_SignonNum_Roundtrip(t *testing.T) {
	for stage := 1; stage <= 4; stage++ {
		buf := sizebuf.New(make([]byte, 8))
		if err := server.EncodeSignonNum(buf, stage); err != nil {
			t.Fatal(err)
		}
		sr := newReader(buf.Bytes())
		v, err := sr.Next(protocol.VersionNQ)
		if err != nil {
			t.Fatal(err)
		}
		got := v.(DecodedSignonNum)
		if got.Stage != stage {
			t.Errorf("stage: got %d want %d", got.Stage, stage)
		}
	}
}

func TestNext_SignonNum_Truncated(t *testing.T) {
	sr := newReader([]byte{protocol.SvcSignonNum})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedPrint / DecodedStuffText / DecodedFinale / DecodedCutscene

func TestNext_Print_Roundtrip(t *testing.T) {
	const want = "hello, world"
	buf := sizebuf.New(make([]byte, 64))
	if err := msg.WriteByte(buf, protocol.SvcPrint); err != nil {
		t.Fatal(err)
	}
	if err := msg.WriteString(buf, want); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if v.(DecodedPrint).Text != want {
		t.Errorf("text: got %q want %q", v.(DecodedPrint).Text, want)
	}
}

func TestNext_StuffText_Roundtrip(t *testing.T) {
	const want = "name BlubBlub\n"
	buf := sizebuf.New(make([]byte, 64))
	if err := server.EncodeStuffText(buf, want); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if v.(DecodedStuffText).Text != want {
		t.Errorf("text mismatch")
	}
}

func TestNext_Finale_Roundtrip(t *testing.T) {
	const want = "episode complete"
	buf := sizebuf.New(make([]byte, 64))
	if err := server.EncodeFinale(buf, want); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if v.(DecodedFinale).Text != want {
		t.Errorf("text mismatch")
	}
}

func TestNext_Cutscene_Roundtrip(t *testing.T) {
	const want = "you have entered the slipgate"
	buf := sizebuf.New(make([]byte, 64))
	if err := server.EncodeCutscene(buf, want); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if v.(DecodedCutscene).Text != want {
		t.Errorf("text mismatch")
	}
}

func TestNext_CenterPrint_Roundtrip(t *testing.T) {
	const want = "you got the shotgun"
	buf := sizebuf.New(make([]byte, 64))
	if err := server.EncodeCenterPrint(buf, want); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if v.(DecodedCenterPrint).Text != want {
		t.Errorf("text: got %q want %q", v.(DecodedCenterPrint).Text, want)
	}
}

func TestNext_CenterPrint_Truncated(t *testing.T) {
	// Cmd byte only -- ReadString hits EOF immediately.
	sr := newReader([]byte{protocol.SvcCenterPrint})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

func TestNext_CenterPrint_Empty(t *testing.T) {
	// Empty string body (lone NUL after the cmd byte).
	buf := sizebuf.New(make([]byte, 4))
	if err := server.EncodeCenterPrint(buf, ""); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if v.(DecodedCenterPrint).Text != "" {
		t.Errorf("text: got %q want empty", v.(DecodedCenterPrint).Text)
	}
}

// Each of the four "byte + string" opcodes shares the same body
// path; one truncation test is enough to cover the bad branch.
func TestNext_Print_Truncated(t *testing.T) {
	// Cmd byte only -- ReadString hits EOF immediately.
	sr := newReader([]byte{protocol.SvcPrint})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedUpdateName ---------------------------------------

func TestNext_UpdateName_Roundtrip(t *testing.T) {
	const name = "Player1"
	buf := sizebuf.New(make([]byte, 32))
	if err := server.EncodeUpdateName(buf, 3, name); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedUpdateName)
	if got.Slot != 3 || got.Name != name {
		t.Errorf("got %+v want slot=3 name=%q", got, name)
	}
}

func TestNext_UpdateName_Truncated(t *testing.T) {
	// Cmd byte only -- ReadU8 for slot hits EOF.
	sr := newReader([]byte{protocol.SvcUpdateName})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedUpdateColors -------------------------------------

func TestNext_UpdateColors_Roundtrip(t *testing.T) {
	buf := sizebuf.New(make([]byte, 8))
	if err := server.EncodeUpdateColors(buf, 5, 0x42); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedUpdateColors)
	if got.Slot != 5 || got.Colors != 0x42 {
		t.Errorf("got %+v want slot=5 colors=0x42", got)
	}
}

func TestNext_UpdateColors_Truncated(t *testing.T) {
	sr := newReader([]byte{protocol.SvcUpdateColors})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedUpdateFrags --------------------------------------

func TestNext_UpdateFrags_Roundtrip(t *testing.T) {
	buf := sizebuf.New(make([]byte, 8))
	if err := server.EncodeUpdateFrags(buf, 7, -42); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedUpdateFrags)
	if got.Slot != 7 || got.Frags != -42 {
		t.Errorf("got %+v want slot=7 frags=-42", got)
	}
}

func TestNext_UpdateFrags_Truncated(t *testing.T) {
	sr := newReader([]byte{protocol.SvcUpdateFrags})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedUpdateStat ---------------------------------------

func TestNext_UpdateStat_Roundtrip(t *testing.T) {
	buf := sizebuf.New(make([]byte, 16))
	if err := server.EncodeUpdateStat(buf, 9, 0x7fff_1234); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedUpdateStat)
	if got.Stat != 9 || got.Value != 0x7fff_1234 {
		t.Errorf("got %+v want stat=9 value=0x7fff1234", got)
	}
}

func TestNext_UpdateStat_Truncated(t *testing.T) {
	sr := newReader([]byte{protocol.SvcUpdateStat})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedParticle -----------------------------------------

func TestNext_Particle_Roundtrip(t *testing.T) {
	origin := [3]float32{8, 16, 24}
	// dir values that quantise+restore cleanly (multiples of 1/16).
	dir := [3]float32{0.5, -1.0, 2.0}
	buf := sizebuf.New(make([]byte, 32))
	if err := server.EncodeParticle(buf, origin, dir, 73, 5); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedParticle)
	if got.Origin != origin {
		t.Errorf("origin: got %v want %v", got.Origin, origin)
	}
	if got.Dir != dir {
		t.Errorf("dir: got %v want %v", got.Dir, dir)
	}
	if got.Color != 73 || got.Count != 5 {
		t.Errorf("got %+v want color=73 count=5", got)
	}
}

func TestNext_Particle_Truncated(t *testing.T) {
	// Cmd byte only.
	sr := newReader([]byte{protocol.SvcParticle})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedSound --------------------------------------------

// Sound roundtrip on plain NQ: default volume + attenuation get
// omitted from the wire (no SndVolume / SndAttenuation bits); the
// decoder must reconstruct the defaults.
func TestNext_Sound_NQ_DefaultVolAtten(t *testing.T) {
	origin := [3]float32{100, 200, 300}
	buf := sizebuf.New(make([]byte, 32))
	if err := server.EncodeSound(buf, 42, 3, 17, origin,
		protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation,
		protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedSound)
	if got.EntityIdx != 42 || got.Channel != 3 || got.SoundNum != 17 {
		t.Errorf("ids: got %+v want ent=42 ch=3 sn=17", got)
	}
	if got.Volume != protocol.DefaultSoundVolume {
		t.Errorf("volume: got %d want default", got.Volume)
	}
	if got.Atten != float32(protocol.DefaultSoundAttenuation) {
		t.Errorf("atten: got %v want default", got.Atten)
	}
	if got.Origin != origin {
		t.Errorf("origin: got %v want %v", got.Origin, origin)
	}
}

// Sound with non-default volume + attenuation -> both wire bits set.
func TestNext_Sound_NQ_NonDefaultVolAtten(t *testing.T) {
	origin := [3]float32{0, 0, 0}
	buf := sizebuf.New(make([]byte, 32))
	const vol = 128
	const atten float32 = 2.0
	if err := server.EncodeSound(buf, 1, 0, 5, origin, vol, atten,
		protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedSound)
	if got.Volume != vol {
		t.Errorf("volume: got %d want %d", got.Volume, vol)
	}
	if math.Abs(float64(got.Atten-atten)) > 1e-6 {
		t.Errorf("atten: got %v want %v", got.Atten, atten)
	}
}

// FITZ-large-entity branch: entIdx >= 8192 forces the
// SndFitzLargeEntity bit + the alternate (short entIdx, byte
// channel) wire layout.
func TestNext_Sound_FITZ_LargeEntity(t *testing.T) {
	origin := [3]float32{0, 0, 0}
	buf := sizebuf.New(make([]byte, 32))
	if err := server.EncodeSound(buf, 9000, 2, 17, origin,
		protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation,
		protocol.VersionFitz); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionFitz)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedSound)
	if got.EntityIdx != 9000 || got.Channel != 2 || got.SoundNum != 17 {
		t.Errorf("got %+v want ent=9000 ch=2 sn=17", got)
	}
}

// FITZ-large-sound branch: soundNum >= 256 forces the
// SndFitzLargeSound bit + the short soundNum wire encoding.
func TestNext_Sound_FITZ_LargeSound(t *testing.T) {
	origin := [3]float32{0, 0, 0}
	buf := sizebuf.New(make([]byte, 32))
	if err := server.EncodeSound(buf, 1, 0, 500, origin,
		protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation,
		protocol.VersionFitz); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionFitz)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedSound)
	if got.SoundNum != 500 {
		t.Errorf("soundnum: got %d want 500", got.SoundNum)
	}
}

// BJP/BJP2/BJP3 each route sound-num through a different width.
func TestNext_Sound_BJP_Byte(t *testing.T) {
	origin := [3]float32{0, 0, 0}
	buf := sizebuf.New(make([]byte, 32))
	if err := server.EncodeSound(buf, 1, 0, 7, origin,
		protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation,
		protocol.VersionBJP); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionBJP)
	if err != nil {
		t.Fatal(err)
	}
	if v.(DecodedSound).SoundNum != 7 {
		t.Errorf("soundnum")
	}
}

// BJP2 / BJP3 always route soundNum through a short on the wire
// (per server.writeSoundNum) regardless of value -- the encoder
// rejects soundNum >= 256 on those protocols today (see sound.go's
// largeSound check), so the value here stays under 256 but still
// exercises the short-width decode branch in readSoundNum.
func TestNext_Sound_BJP2_Short(t *testing.T) {
	origin := [3]float32{0, 0, 0}
	buf := sizebuf.New(make([]byte, 32))
	if err := server.EncodeSound(buf, 1, 0, 99, origin,
		protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation,
		protocol.VersionBJP2); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionBJP2)
	if err != nil {
		t.Fatal(err)
	}
	if v.(DecodedSound).SoundNum != 99 {
		t.Errorf("soundnum")
	}
}

func TestNext_Sound_BJP3_Short(t *testing.T) {
	origin := [3]float32{0, 0, 0}
	buf := sizebuf.New(make([]byte, 32))
	if err := server.EncodeSound(buf, 1, 0, 77, origin,
		protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation,
		protocol.VersionBJP3); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionBJP3)
	if err != nil {
		t.Fatal(err)
	}
	if v.(DecodedSound).SoundNum != 77 {
		t.Errorf("soundnum")
	}
}

// Unknown-protocol path inside readSoundNum: the decoder surfaces an
// ErrUnknownSvc-wrapped error when proto is not a known version.
func TestNext_Sound_UnknownProto(t *testing.T) {
	// Build a valid NQ sound, then feed the decoder a bogus proto.
	origin := [3]float32{0, 0, 0}
	buf := sizebuf.New(make([]byte, 32))
	if err := server.EncodeSound(buf, 1, 0, 5, origin,
		protocol.DefaultSoundVolume, protocol.DefaultSoundAttenuation,
		protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	if _, err := sr.Next(0); !errors.Is(err, ErrUnknownSvc) {
		t.Errorf("err: got %v want ErrUnknownSvc wrap", err)
	}
}

// Truncation just after the cmd byte.
func TestNext_Sound_TruncatedAtFieldMask(t *testing.T) {
	sr := newReader([]byte{protocol.SvcSound})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// Truncation just after the field mask (body cut off before origin).
func TestNext_Sound_TruncatedInBody(t *testing.T) {
	// Field mask = 0 -> no vol/atten bytes; need short channel +
	// byte soundnum + 3 coords. Give only the mask byte.
	sr := newReader([]byte{protocol.SvcSound, 0})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedServerInfo ---------------------------------------

func TestNext_ServerInfo_Roundtrip(t *testing.T) {
	info := server.ServerInfo{
		Protocol:      protocol.VersionNQ,
		MaxClients:    4,
		GameType:      server.GameTypeDeathmatch,
		LevelName:     "The Slipgate Complex",
		ModelPrecache: []string{"maps/e1m1.bsp", "progs/player.mdl", "progs/grenade.mdl", ""},
		SoundPrecache: []string{"", "weapons/sgun1.wav", "items/itembk2.wav", ""},
	}
	buf := sizebuf.New(make([]byte, 256))
	if err := server.EncodeServerInfo(buf, info); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := v.(DecodedServerInfo)
	if !ok {
		t.Fatalf("type: %T", v)
	}
	if got.Protocol != info.Protocol {
		t.Errorf("proto: got %d want %d", got.Protocol, info.Protocol)
	}
	if got.MaxClients != info.MaxClients {
		t.Errorf("maxclients")
	}
	if got.GameType != int(info.GameType) {
		t.Errorf("gametype")
	}
	if got.LevelName != info.LevelName {
		t.Errorf("level")
	}
	// Encoder skips slot 0 of both precache lists.
	wantModels := []string{"progs/player.mdl", "progs/grenade.mdl"}
	wantSounds := []string{"weapons/sgun1.wav", "items/itembk2.wav"}
	if len(got.ModelPrecache) != len(wantModels) {
		t.Fatalf("models: got %v want %v", got.ModelPrecache, wantModels)
	}
	for i, s := range wantModels {
		if got.ModelPrecache[i] != s {
			t.Errorf("models[%d]: got %q want %q", i, got.ModelPrecache[i], s)
		}
	}
	if len(got.SoundPrecache) != len(wantSounds) {
		t.Fatalf("sounds: got %v want %v", got.SoundPrecache, wantSounds)
	}
	for i, s := range wantSounds {
		if got.SoundPrecache[i] != s {
			t.Errorf("sounds[%d]: got %q want %q", i, got.SoundPrecache[i], s)
		}
	}

	// The encoder writes a trailing svc_signonnum byte + stage 1
	// byte as a SECOND message; verify it.
	v2, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	if v2.(DecodedSignonNum).Stage != 1 {
		t.Errorf("trailing signon stage")
	}
}

// Truncated serverinfo: cmd byte only.
func TestNext_ServerInfo_TruncatedAtHeader(t *testing.T) {
	sr := newReader([]byte{protocol.SvcServerInfo})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// Truncated serverinfo: header survives but model list is cut off.
func TestNext_ServerInfo_TruncatedInModelList(t *testing.T) {
	// Build a valid serverinfo prefix then cut after the level name
	// and BEFORE the model list sentinel. Easiest: build a real one
	// and truncate its bytes.
	info := server.ServerInfo{
		Protocol:      protocol.VersionNQ,
		MaxClients:    4,
		GameType:      server.GameTypeCoop,
		LevelName:     "x",
		ModelPrecache: []string{"world.bsp", "foo.mdl", ""},
		SoundPrecache: []string{"", ""},
	}
	buf := sizebuf.New(make([]byte, 256))
	if err := server.EncodeServerInfo(buf, info); err != nil {
		t.Fatal(err)
	}
	// Cut at byte 9 (cmd + 4 proto + 1 maxc + 1 gt + 2 name "x\0" = 9).
	// Header reads cleanly; the first readStringList (models) hits EOF
	// on its first ReadString call.
	full := buf.Bytes()
	sr := newReader(full[:9])
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// Truncated serverinfo: sound list cut off after model list closes.
func TestNext_ServerInfo_TruncatedInSoundList(t *testing.T) {
	info := server.ServerInfo{
		Protocol:      protocol.VersionNQ,
		MaxClients:    4,
		GameType:      server.GameTypeCoop,
		LevelName:     "x",
		ModelPrecache: []string{"world.bsp", ""}, // model list has only the sentinel
		SoundPrecache: []string{"", "ow.wav", ""},
	}
	buf := sizebuf.New(make([]byte, 256))
	if err := server.EncodeServerInfo(buf, info); err != nil {
		t.Fatal(err)
	}
	// Drop the trailing svc_signonnum + stage 1 (last 2 bytes), then
	// drop a few more to cut the sound list mid-string.
	full := buf.Bytes()
	// Find the model-sentinel byte (zero after the level name + zero
	// model precache walk). Easiest: truncate hard to 10 bytes which
	// is: 1 cmd + 4 proto + 1 mc + 1 gt + 2 name "x\0" + 1 model
	// sentinel = 10. The next ReadString (for sounds) then hits EOF.
	sr := newReader(full[:10])
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedBaseline -----------------------------------------

func TestNext_Baseline_NQ_Roundtrip(t *testing.T) {
	base := server.EntityBaseline{
		ModelIndex: 5,
		Frame:      2,
		ColorMap:   1,
		SkinNum:    0,
		Origin:     [3]float32{32, 64, 16},
		Angles:     [3]float32{0, 90, 0},
	}
	buf := sizebuf.New(make([]byte, 64))
	if err := server.EncodeBaseline(buf, 7, base, protocol.VersionNQ); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedBaseline)
	if got.EntityNum != 7 || got.ModelIdx != 5 || got.Frame != 2 ||
		got.ColorMap != 1 || got.SkinNum != 0 {
		t.Errorf("scalars: got %+v", got)
	}
	if got.Origin != base.Origin {
		t.Errorf("origin")
	}
	// Angle quantisation: WriteAngle rounds, so the round-trip is
	// approximate. Tolerate one quantum (= 360/256 ~= 1.4 degrees).
	for i := 0; i < 3; i++ {
		diff := math.Abs(float64(got.Angles[i] - base.Angles[i]))
		if diff > 360.0/256.0+1e-3 {
			t.Errorf("angles[%d]: got %v want ~%v", i, got.Angles[i], base.Angles[i])
		}
	}
	if got.Alpha != protocol.EntAlphaDefault {
		t.Errorf("alpha: got %d want default", got.Alpha)
	}
}

// BJP routes modelIndex through a short rather than a byte.
func TestNext_Baseline_BJP_ShortModelIdx(t *testing.T) {
	base := server.EntityBaseline{ModelIndex: 1234, Frame: 0}
	buf := sizebuf.New(make([]byte, 64))
	if err := server.EncodeBaseline(buf, 3, base, protocol.VersionBJP); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionBJP)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedBaseline)
	if got.ModelIdx != 1234 {
		t.Errorf("modelIdx: got %d want 1234", got.ModelIdx)
	}
}

// BJP2 also uses a short modelIdx.
func TestNext_Baseline_BJP2_ShortModelIdx(t *testing.T) {
	base := server.EntityBaseline{ModelIndex: 257, Frame: 0}
	buf := sizebuf.New(make([]byte, 64))
	if err := server.EncodeBaseline(buf, 3, base, protocol.VersionBJP2); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionBJP2)
	if err != nil {
		t.Fatal(err)
	}
	if v.(DecodedBaseline).ModelIdx != 257 {
		t.Errorf("modelIdx")
	}
}

// BJP3 -- third path through the short branch.
func TestNext_Baseline_BJP3_ShortModelIdx(t *testing.T) {
	base := server.EntityBaseline{ModelIndex: 300, Frame: 0}
	buf := sizebuf.New(make([]byte, 64))
	if err := server.EncodeBaseline(buf, 3, base, protocol.VersionBJP3); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionBJP3)
	if err != nil {
		t.Fatal(err)
	}
	if v.(DecodedBaseline).ModelIdx != 300 {
		t.Errorf("modelIdx")
	}
}

func TestNext_Baseline_Truncated(t *testing.T) {
	sr := newReader([]byte{protocol.SvcSpawnBaseline})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedUpdate -------------------------------------------

// Minimal update: only entityNum (no optional fields, no MOREBITS).
func TestNext_Update_Minimal(t *testing.T) {
	upd := server.EntityUpdate{Bits: 0}
	buf := sizebuf.New(make([]byte, 32))
	if err := server.EncodeUpdate(buf, 17, upd); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedUpdate)
	if got.EntityNum != 17 {
		t.Errorf("entityNum: got %d want 17", got.EntityNum)
	}
	if got.Bits != 0 {
		t.Errorf("bits: got %d want 0", got.Bits)
	}
}

// Full update: every optional bit set so the decoder walks every
// branch.
func TestNext_Update_AllOptionalFields(t *testing.T) {
	upd := server.EntityUpdate{
		Bits: protocol.UMoreBits | protocol.ULongEntity |
			protocol.UModel | protocol.UFrame | protocol.UColorMap |
			protocol.USkin | protocol.UEffects |
			protocol.UOrigin1 | protocol.UAngle1 |
			protocol.UOrigin2 | protocol.UAngle2 |
			protocol.UOrigin3 | protocol.UAngle3,
		Origin:   [3]float32{1, 2, 3},
		Angles:   [3]float32{0, 0, 0},
		Model:    9,
		Frame:    11,
		ColorMap: 1,
		Skin:     2,
		Effects:  4,
	}
	buf := sizebuf.New(make([]byte, 64))
	if err := server.EncodeUpdate(buf, 1000, upd); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedUpdate)
	if got.EntityNum != 1000 {
		t.Errorf("entityNum")
	}
	if got.Model != 9 || got.Frame != 11 || got.ColorMap != 1 ||
		got.Skin != 2 || got.Effects != 4 {
		t.Errorf("scalars: %+v", got)
	}
	if got.Origin != upd.Origin {
		t.Errorf("origin: got %v want %v", got.Origin, upd.Origin)
	}
}

// Truncated update body.
func TestNext_Update_Truncated(t *testing.T) {
	// Cmd byte = USignal + UMoreBits forces a second byte read which
	// will fail.
	sr := newReader([]byte{byte(protocol.USignal | protocol.UMoreBits)})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- DecodedClientData ---------------------------------------

func TestNext_ClientData_Minimal(t *testing.T) {
	// All-zero state -- the encoder still sets SUItems, writes the
	// items long, health short, ammo+activeweapon bytes; everything
	// else is gated off.
	state := server.ClientDataState{
		ViewHeightOffset: protocol.DefaultViewHeight,
		Items:            0,
		Health:           100,
	}
	buf := sizebuf.New(make([]byte, 64))
	if err := server.EncodeClientData(buf, state); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedClientData)
	if got.Health != 100 {
		t.Errorf("health: got %d want 100", got.Health)
	}
	if got.ViewHeightOffset != protocol.DefaultViewHeight {
		t.Errorf("viewheight: got %v want default", got.ViewHeightOffset)
	}
	if got.OnGround || got.InWater {
		t.Errorf("ground/water bits should default to false")
	}
}

// All optional bits set.
func TestNext_ClientData_Maximal(t *testing.T) {
	state := server.ClientDataState{
		ViewHeightOffset: 30,
		IdealPitch:       -5,
		PunchAngle:       [3]float32{1, 2, 3},
		Velocity:         [3]float32{16, 32, 48},
		Items:            0x1234_5678, // any int32
		OnGround:         true,
		InWater:          true,
		WeaponFrame:      3,
		ArmorValue:       100,
		WeaponModel:      5,
		Health:           75,
		CurrentAmmo:      25,
		Ammo:             [4]int{10, 20, 30, 40},
		ActiveWeapon:     7,
	}
	buf := sizebuf.New(make([]byte, 64))
	if err := server.EncodeClientData(buf, state); err != nil {
		t.Fatal(err)
	}
	sr := newReader(buf.Bytes())
	v, err := sr.Next(protocol.VersionNQ)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(DecodedClientData)
	if got.ViewHeightOffset != 30 || got.IdealPitch != -5 {
		t.Errorf("view/pitch: %+v", got)
	}
	if got.PunchAngle != state.PunchAngle {
		t.Errorf("punchangle: got %v want %v", got.PunchAngle, state.PunchAngle)
	}
	if got.Velocity != state.Velocity {
		t.Errorf("velocity: got %v want %v", got.Velocity, state.Velocity)
	}
	if !got.OnGround || !got.InWater {
		t.Errorf("ground/water bits not set")
	}
	if got.WeaponFrame != 3 || got.ArmorValue != 100 || got.WeaponModel != 5 {
		t.Errorf("weapon scalars: %+v", got)
	}
	if got.Health != 75 || got.CurrentAmmo != 25 {
		t.Errorf("health/ammo")
	}
	for i, want := range state.Ammo {
		if got.Ammo[i] != want {
			t.Errorf("ammo[%d]: got %d want %d", i, got.Ammo[i], want)
		}
	}
	if got.ActiveWeapon != 7 {
		t.Errorf("activeweapon")
	}
	if got.Items != state.Items {
		t.Errorf("items")
	}
}

func TestNext_ClientData_Truncated(t *testing.T) {
	sr := newReader([]byte{protocol.SvcClientData})
	if _, err := sr.Next(protocol.VersionNQ); !errors.Is(err, ErrCorruptMessage) {
		t.Errorf("err: got %v want ErrCorruptMessage", err)
	}
}

// ------- Marker interface cover ----------------------------------

// The isDecoded marker methods exist solely to seal the [Decoded]
// interface; nothing calls them at runtime. Touch each one so the
// statement-coverage report acknowledges them.
func TestDecodedMarkers(t *testing.T) {
	var sink Decoded
	for _, d := range []Decoded{
		DecodedNop{}, DecodedDisconnect{}, DecodedKilledMonster{},
		DecodedFoundSecret{}, DecodedSellScreen{},
		DecodedSetView{}, DecodedSignonNum{},
		DecodedPrint{}, DecodedStuffText{},
		DecodedFinale{}, DecodedIntermission{}, DecodedCutscene{},
		DecodedUpdateName{}, DecodedUpdateColors{},
		DecodedUpdateFrags{}, DecodedUpdateStat{},
		DecodedParticle{}, DecodedSound{},
		DecodedServerInfo{}, DecodedBaseline{},
		DecodedUpdate{}, DecodedClientData{},
	} {
		// Calling the unexported marker requires a per-type assertion
		// so the receiver is the concrete struct, not the interface.
		switch v := d.(type) {
		case DecodedNop:
			v.isDecoded()
		case DecodedDisconnect:
			v.isDecoded()
		case DecodedKilledMonster:
			v.isDecoded()
		case DecodedFoundSecret:
			v.isDecoded()
		case DecodedSellScreen:
			v.isDecoded()
		case DecodedSetView:
			v.isDecoded()
		case DecodedSignonNum:
			v.isDecoded()
		case DecodedPrint:
			v.isDecoded()
		case DecodedStuffText:
			v.isDecoded()
		case DecodedFinale:
			v.isDecoded()
		case DecodedIntermission:
			v.isDecoded()
		case DecodedCutscene:
			v.isDecoded()
		case DecodedUpdateName:
			v.isDecoded()
		case DecodedUpdateColors:
			v.isDecoded()
		case DecodedUpdateFrags:
			v.isDecoded()
		case DecodedUpdateStat:
			v.isDecoded()
		case DecodedParticle:
			v.isDecoded()
		case DecodedSound:
			v.isDecoded()
		case DecodedServerInfo:
			v.isDecoded()
		case DecodedBaseline:
			v.isDecoded()
		case DecodedUpdate:
			v.isDecoded()
		case DecodedClientData:
			v.isDecoded()
		}
		sink = d
	}
	_ = sink
}

// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"fmt"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// ErrNilBuf is the sentinel returned by the misc encoders in this
// file when the caller passes a nil sizebuf. The other server-side
// encoders (datagram.go, sound.go, serverinfo.go, baseline.go) still
// return inline errors.New("server: nil sizebuf") values; the
// sentinel is introduced here so the new APIs are errors.Is-friendly
// without churning the existing files.
var ErrNilBuf = errors.New("server: nil sizebuf")

// ErrSignonStageRange is returned by EncodeSignonNum when the
// requested signon stage is outside the protocol-defined range
// [1, 4]. Quake's handshake state machine only has four stages and
// the wire byte is a 1-byte unsigned, so values outside that range
// would either be ignored or misinterpreted by the client.
var ErrSignonStageRange = errors.New("server: signon stage outside [1, 4]")

// ErrEntityNumRange is returned by EncodeSetView when entityNum is
// outside the unsigned 16-bit range the wire format encodes. The
// C upstream silently truncates via MSG_WriteShort's (short)cast;
// the Go port validates instead so a buggy caller surfaces loudly
// rather than render-cameras getting attached to the wrong edict.
var ErrEntityNumRange = errors.New("server: entityNum outside [0, 65535]")

// EncodeNop writes a single svc_nop byte (keepalive ping). Sent
// when nothing else is going out but the connection needs a
// liveness signal. tyrquake: SV_SendNop in NQ/sv_main.c.
func EncodeNop(buf *sizebuf.Buffer) error {
	if buf == nil {
		return ErrNilBuf
	}
	return msg.WriteByte(buf, protocol.SvcNop)
}

// EncodeDisconnect writes a single svc_disconnect byte. The client
// closes the connection on receipt. tyrquake: SV_SendDisconnect's
// trailer (the wire-emit; the printf reason text is the caller's
// job and lands via [EncodeStuffText] or the Message buffer).
func EncodeDisconnect(buf *sizebuf.Buffer) error {
	if buf == nil {
		return ErrNilBuf
	}
	return msg.WriteByte(buf, protocol.SvcDisconnect)
}

// EncodeSetView writes svc_setview + a short entity index. Tells
// the client which entity is its render-camera (1 = self, 0 =
// detached / cutscene). tyrquake: emitted inline by
// SV_SendServerinfo and by SV_SetView (a QC builtin).
//
// entityNum must fit in an unsigned 16-bit slot ([0, 65535]);
// returns [ErrEntityNumRange] otherwise. The C upstream silently
// truncates via (short)cast in MSG_WriteShort; the Go port
// validates to surface caller bugs explicitly.
func EncodeSetView(buf *sizebuf.Buffer, entityNum int) error {
	if buf == nil {
		return ErrNilBuf
	}
	if entityNum < 0 || entityNum > 0xffff {
		return fmt.Errorf("%w: %d", ErrEntityNumRange, entityNum)
	}
	if err := msg.WriteByte(buf, protocol.SvcSetView); err != nil {
		return err
	}
	return msg.WriteShort(buf, entityNum)
}

// EncodeSignonNum writes svc_signonnum + a single byte signonStage.
// The client uses this to advance its handshake state machine
// (1, 2, 3, 4 are the four signon stages). tyrquake: emitted
// inline by SV_SendServerinfo and per-stage in host_cmd.c.
//
// signonStage must be in [1, 4]; returns [ErrSignonStageRange]
// otherwise. No bytes are written when the stage is out of range.
func EncodeSignonNum(buf *sizebuf.Buffer, signonStage int) error {
	if buf == nil {
		return ErrNilBuf
	}
	if signonStage < 1 || signonStage > 4 {
		return fmt.Errorf("%w: %d", ErrSignonStageRange, signonStage)
	}
	if err := msg.WriteByte(buf, protocol.SvcSignonNum); err != nil {
		return err
	}
	return msg.WriteByte(buf, signonStage)
}

// EncodeIntermission writes a single svc_intermission byte. The
// client flips into intermission mode on receipt: the renderer
// hides the in-game HUD and overlays the end-of-level scoreboard
// (time taken + secrets found + monsters killed, sourced from the
// per-client stat bank already pushed via svc_updatestat). tyrquake:
// emitted by SV_SaveSpawnparms inside Host_FindMaxClients during a
// changelevel + by PF_Intermission (a QC builtin) for trigger-driven
// intermissions.
//
// No payload: the camera lock + text layout are driven entirely off
// the client's cached stat bank + the most-recent info_intermission
// entity origin (which the server still has to push separately via
// svc_setangle + svc_setview in a follow-up batch; this commit
// lands the wire opcode + HUD swap, the camera pose stays at the
// player's last-known origin).
func EncodeIntermission(buf *sizebuf.Buffer) error {
	if buf == nil {
		return ErrNilBuf
	}
	return msg.WriteByte(buf, protocol.SvcIntermission)
}

// EncodeFinale writes svc_finale + a centered NUL-terminated text
// string. Shown at end-of-episode / end-of-level intermissions.
// tyrquake: emitted by PF_Finale (a QC builtin).
func EncodeFinale(buf *sizebuf.Buffer, text string) error {
	if buf == nil {
		return ErrNilBuf
	}
	if err := msg.WriteByte(buf, protocol.SvcFinale); err != nil {
		return err
	}
	return msg.WriteString(buf, text)
}

// EncodeCutscene writes svc_cutscene + a NUL-terminated text
// string. Used for engine-side cutscene captions. tyrquake:
// emitted by PF_Cutscene (a QC builtin).
func EncodeCutscene(buf *sizebuf.Buffer, text string) error {
	if buf == nil {
		return ErrNilBuf
	}
	if err := msg.WriteByte(buf, protocol.SvcCutscene); err != nil {
		return err
	}
	return msg.WriteString(buf, text)
}

// EncodeCenterPrint writes svc_centerprint + a NUL-terminated text
// string. The client overlays it horizontally-centered near 40% of
// the screen height (the "you got the shotgun" / intermission banner).
// tyrquake: emitted by PF_centerprint (a QC builtin) + by SV_PrintToClient
// for intermission text.
func EncodeCenterPrint(buf *sizebuf.Buffer, text string) error {
	if buf == nil {
		return ErrNilBuf
	}
	if err := msg.WriteByte(buf, protocol.SvcCenterPrint); err != nil {
		return err
	}
	return msg.WriteString(buf, text)
}

// EncodeStuffText writes svc_stufftext + a NUL-terminated string
// the client interprets as console commands (e.g. "name BlubBlub\n"
// to rename a player). tyrquake: SV_BroadcastCommand / per-builtin
// stuffcmd.
func EncodeStuffText(buf *sizebuf.Buffer, text string) error {
	if buf == nil {
		return ErrNilBuf
	}
	if err := msg.WriteByte(buf, protocol.SvcStuffText); err != nil {
		return err
	}
	return msg.WriteString(buf, text)
}

// ErrCDTrackRange is returned by EncodeCDTrack when track or loopTrack
// is outside the unsigned-byte range the wire format encodes. The C
// upstream silently truncates via the byte cast in MSG_WriteByte; the
// Go port validates so a buggy caller surfaces loudly rather than
// silently switching to the wrong track number.
var ErrCDTrackRange = errors.New("server: cdtrack number outside [0, 255]")

// EncodeCDTrack writes svc_cdtrack + two bytes: the active track and
// the loop-back track. The client reads both, opens the matching
// "music/trackXX.ogg" off the embedded pak (or any other registered
// audio asset source), and streams the decoded PCM through the audio
// mixer alongside the per-tic SFX.
//
// In id Software's original Q1, the two bytes selected a CD audio
// track (the 1996 retail discs shipped 10 ambient/score tracks on
// audio tracks 2..11 of the CD); the Go port replaces the CD-DA
// dependency with .ogg files inside the pak, matching the convention
// used by every modern source port (QuakeSpasm, FTEQW, vkQuake).
// tyrquake: SV_SendServerinfo emits this right after the precache
// lists -- the wire byte was reused 1:1 by the source-port community,
// only the playback backend changed.
//
// track is the active music track (1 = title music, 2..11 = per-map
// score from the retail soundtrack; 0 = silence). loopTrack is the
// track the streamer falls back to once `track` reaches EOF (typically
// equal to `track` for self-looping background score; 0 stops at EOF).
//
// Both byte ranges are validated against [0, 255]; out-of-range
// returns [ErrCDTrackRange] without writing any bytes.
func EncodeCDTrack(buf *sizebuf.Buffer, track, loopTrack int) error {
	if buf == nil {
		return ErrNilBuf
	}
	if track < 0 || track > 0xff {
		return fmt.Errorf("%w: track=%d", ErrCDTrackRange, track)
	}
	if loopTrack < 0 || loopTrack > 0xff {
		return fmt.Errorf("%w: loopTrack=%d", ErrCDTrackRange, loopTrack)
	}
	if err := msg.WriteByte(buf, protocol.SvcCDTrack); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, track); err != nil {
		return err
	}
	return msg.WriteByte(buf, loopTrack)
}

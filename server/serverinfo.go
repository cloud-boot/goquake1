// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// GameType is the svc_serverinfo gametype byte: it tells the client
// whether the upcoming map is cooperative (the default; id1 single-
// player is just coop with one player) or deathmatch. tyrquake:
// GAME_COOP / GAME_DEATHMATCH in NQ/protocol.h.
type GameType byte

const (
	// GameTypeCoop is the cooperative + single-player gametype
	// (tyrquake: GAME_COOP = 0).
	GameTypeCoop GameType = 0
	// GameTypeDeathmatch is the free-for-all gametype (tyrquake:
	// GAME_DEATHMATCH = 1).
	GameTypeDeathmatch GameType = 1
)

// ErrEmptyLevelName fires when callers try to encode a serverinfo
// handshake with an empty LevelName. The C upstream feeds
// sv.edicts->v.message directly to MSG_WriteString and would emit a
// lone NUL byte for the banner string; the Go port surfaces it
// earlier because an empty banner is a worldspawn-misconfig bug, not
// a wire-protocol feature.
var ErrEmptyLevelName = errors.New("server: empty serverinfo level name")

// ServerInfo bundles the per-spawn handshake fields the
// svc_serverinfo message needs. LevelName is the worldspawn
// "message" string (the scrolling banner the client overlays during
// the loading screen); ModelPrecache + SoundPrecache are the
// sentinel-terminated asset lists from [Server] (walk stops at the
// first empty entry).
type ServerInfo struct {
	Protocol      int      // one of protocol.Version*
	MaxClients    int      // svs.maxclients
	GameType      GameType // GameTypeCoop or GameTypeDeathmatch
	LevelName     string   // the worldspawn "message" cvar
	ModelPrecache []string // sentinel-terminated; the slot-0 worldmodel IS encoded
	SoundPrecache []string // sentinel-terminated; the slot-0 reserved entry is skipped
}

// EncodeServerInfo writes the svc_serverinfo handshake into buf:
// protocol version + max_clients + gametype + level banner +
// sentinel-terminated model precache strings + sentinel-terminated
// sound precache strings + the signon-1 marker that tells the client
// "handshake done, expect precache/baseline next".
//
// tyrquake: the message-writing portion of SV_SendServerinfo in
// NQ/sv_main.c. The Go port deliberately SIMPLIFIES the upstream:
//
//   - The leading svc_print version-banner ("VERSION %4.2f SERVER
//     (%i CRC)") is dropped: it needs the build version + program
//     CRC globals the Go port doesn't carry. A future glue layer
//     can prepend that banner separately.
//   - The svc_cdtrack pair + svc_setview placeholder are dropped:
//     they need the worldspawn `sounds` field and the client's edict
//     slot, both of which belong in the per-client lifecycle code
//     (yet to be ported). Callers that need cdtrack/setview should
//     emit those bytes themselves after EncodeServerInfo returns.
//
// The result is a pure wire encoder: every input is an explicit
// parameter, no globals, no side effects beyond buf.
//
// The model + sound walks match the C upstream's "skip slot 0,
// stop at first empty slot" pattern: ModelPrecache[0] is the
// worldmodel ("maps/<name>.bsp") -- the client gets it from the
// previous svc_serverinfo bytes implicitly via the level name and
// is supposed to reconstruct it; the C source iterates from index 1
// for that reason. SoundPrecache[0] is reserved/none. The Go port
// preserves both skips.
//
// Returns:
//
//	nil on success (buf updated with the full handshake)
//	[ErrEmptyLevelName] when info.LevelName is empty
//	propagated msg.Write* errors on sizebuf overflow
func EncodeServerInfo(buf *sizebuf.Buffer, info ServerInfo) error {
	if buf == nil {
		return errors.New("server: nil sizebuf")
	}
	if info.LevelName == "" {
		return ErrEmptyLevelName
	}

	if err := msg.WriteByte(buf, protocol.SvcServerInfo); err != nil {
		return err
	}
	if err := msg.WriteLong(buf, int32(info.Protocol)); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, info.MaxClients); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, int(info.GameType)); err != nil {
		return err
	}
	if err := msg.WriteString(buf, info.LevelName); err != nil {
		return err
	}

	// Model precache walk: skip slot 0 (the worldmodel), stop at the
	// first empty slot, terminate with a NUL byte. Matches the C
	// "for (s = sv.model_precache + 1; *s; s++)" loop.
	for i := 1; i < len(info.ModelPrecache); i++ {
		name := info.ModelPrecache[i]
		if name == "" {
			break
		}
		if err := msg.WriteString(buf, name); err != nil {
			return err
		}
	}
	if err := msg.WriteByte(buf, 0); err != nil {
		return err
	}

	// Sound precache walk: same pattern. Slot 0 is the reserved
	// "no sound" sentinel; the upstream starts at index 1.
	for i := 1; i < len(info.SoundPrecache); i++ {
		name := info.SoundPrecache[i]
		if name == "" {
			break
		}
		if err := msg.WriteString(buf, name); err != nil {
			return err
		}
	}
	if err := msg.WriteByte(buf, 0); err != nil {
		return err
	}

	// Signon marker: tells the client the serverinfo handshake is
	// complete and signon stage 1 (precache acknowledgement) starts
	// next. tyrquake: svc_signonnum + byte 1.
	if err := msg.WriteByte(buf, protocol.SvcSignonNum); err != nil {
		return err
	}
	if err := msg.WriteByte(buf, 1); err != nil {
		return err
	}

	return nil
}

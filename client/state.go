// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"errors"

	"github.com/go-quake1/engine/sizebuf"
)

// ConnectionState tracks the client's high-level lifecycle phase.
// tyrquake: cactive_t in NQ/client.h. The upstream enum has five
// values (ca_dedicated / ca_disconnected / ca_connected /
// ca_firstupdate / ca_active); the Go port collapses
// ca_connected+ca_firstupdate into [StateConnecting] (both phases
// are "signon handshake in progress" from the lifecycle helpers'
// point of view) and drops ca_dedicated (the dedicated-server
// build has no client state at all, so the case is structurally
// unreachable here).
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting                   // signon handshake in progress
	StateConnected                    // fully signed on, in-game
)

// MaxLightStyles + MaxDLights + MaxTempEntities + MaxEfrags +
// MaxBeams + NumPingTimes are the per-client static caps.
// tyrquake: NQ/quakedef.h + NQ/client.h defines.
const (
	MaxLightStyles  = 64  // NQ/quakedef.h MAX_LIGHTSTYLES
	MaxDLights      = 32  // NQ/client.h MAX_DLIGHTS
	MaxTempEntities = 64  // NQ/quakedef.h MAX_TEMP_ENTITIES
	MaxEfrags       = 640 // NQ/quakedef.h MAX_EFRAGS (id1 value)
	MaxBeams        = 24  // NQ/client.h MAX_BEAMS
	NumPingTimes    = 16  // mirrors server.NumPingTimes
)

// MaxClientMessage is the per-client transport buffer size. Same
// numeric value as server.MaxMsgLen (1 << 18); duplicated here so
// the client package stays decoupled from the server package.
// tyrquake: NQ/quakedef.h MAX_MSGLEN.
const MaxClientMessage = 1 << 18

// LightStyle is one of the 64 named light animation strings.
// Each byte is one frame; 'a' = dim, 'z' = bright. tyrquake:
// lightstyle_t.map in NQ/client.h (the .length field is implicit
// in len(Anim)).
type LightStyle struct {
	Anim []byte
}

// DLight is a per-frame dynamic light (muzzle flash, projectile
// glow, ...). tyrquake: dlight_t in NQ/client.h. Color is stored
// inline (the upstream holds a `const float *` into dl_colors[],
// the Go port copies the three components so the struct owns its
// data and is trivially zero-valued).
type DLight struct {
	Origin   [3]float32
	Radius   float32
	Die      float32 // server time at which the light should expire
	Decay    float32 // radius reduction per second
	MinLight float32
	Key      int
	Color    [3]float32
}

// State is the top-level client state. Everything the renderer +
// UI read to draw a frame lives here. tyrquake: client_state_t in
// NQ/client.h.
//
// The struct is wiped on every map change (see [State.Clear]);
// the [State.Message] sizebuf survives because the upstream's
// cls.message is allocated in client_static_t (which is NOT
// wiped). The Go port collapses both into one struct + the Clear
// method preserves the buffer.
type State struct {
	Connection ConnectionState
	Spawned    bool

	// Time is the client's view of simulation time, between MsgTime
	// and OldTime so the renderer can lerp other state. OldTime is
	// the previous tick we received from the server. tyrquake:
	// client_state_t.time / oldtime / mtime[0].
	Time    float32
	OldTime float32
	MsgTime float32

	// Player slot the local client is bound to (1..maxclients).
	// tyrquake: client_state_t.viewentity.
	PlayerNum int

	// Server identity. ModelPrecache + SoundPrecache are
	// sentinel-terminated slices the decoder fills as the
	// svc_serverinfo handler walks the wire string list.
	MapName       string
	LevelName     string
	ModelPrecache []string
	SoundPrecache []string
	LightStyles   [MaxLightStyles]LightStyle

	// Per-frame visual state. DLights[i].Die == 0 means the slot
	// is free; NumVisEdicts is the count of entities the decoder
	// has queued for this frame's render walk.
	DLights      [MaxDLights]DLight
	NumVisEdicts int

	// Local player state -- the mirror of the server's clientdata
	// (svc_clientdata wire message). tyrquake: scattered fields in
	// client_state_t (viewheight / idealpitch / punchangle /
	// velocity / stats[STAT_HEALTH] / stats[STAT_ITEMS] /
	// stats[STAT_SHELLS..STAT_CELLS] / stats[STAT_ACTIVEWEAPON]).
	ViewHeightOffset float32
	IdealPitch       float32
	PunchAngle       [3]float32
	Velocity         [3]float32
	Health           int
	Items            int32
	Ammo             [4]int // shells / nails / rockets / cells
	CurrentAmmo      int
	Stats            [32]int32 // MAX_CL_STATS generic stat bank
	OnGround         bool
	InWater          bool

	// Cross-tick latency tracking. PingTimes is a ring buffer
	// indexed by NumPings modulo NumPingTimes. tyrquake: pings
	// recorded by CL_ParseUpdate.
	PingTimes [NumPingTimes]float32
	NumPings  int
	Frags     [16]int

	// Message is the per-client transport buffer (the client's
	// outgoing reliable channel to the server). Allocated by
	// [NewState] and preserved across [State.Clear] /
	// [State.Disconnect]; only its contents are wiped.
	Message *sizebuf.Buffer
}

// ErrAlreadyConnected is returned by [State.SetConnecting] when
// the state is not in [StateDisconnected].
var ErrAlreadyConnected = errors.New("client: not in StateDisconnected")

// ErrNotConnecting is returned by [State.MarkSpawned] when the
// state is not in [StateConnecting].
var ErrNotConnecting = errors.New("client: not in StateConnecting")

// NewState returns a fresh client state with the transport buffer
// pre-allocated to [MaxClientMessage]. Connection is
// [StateDisconnected]; Spawned is false; all numeric fields are
// zero.
func NewState() *State {
	return &State{
		Message: sizebuf.New(make([]byte, MaxClientMessage)),
	}
}

// Clear resets the per-map state without freeing the transport
// buffer. Equivalent to tyrquake's CL_ClearState (called at map
// change / disconnect): wipes the cl struct but keeps cls.message
// alive.
//
// Connection state is NOT changed -- the upstream's CL_ClearState
// is independent of the cls.state transition (CL_Disconnect calls
// Clear AFTER flipping cls.state to ca_disconnected, but spawning
// a fresh map calls Clear without touching cls.state).
func (s *State) Clear() {
	buf := s.Message
	buf.Clear()
	*s = State{
		Connection: s.Connection,
		Spawned:    s.Spawned,
		Message:    buf,
	}
}

// Disconnect transitions to [StateDisconnected] and clears
// per-map state via [State.Clear]. Idempotent (calling on an
// already-disconnected state simply re-clears, mirroring
// tyrquake's CL_Disconnect which always runs the cl-wipe path
// regardless of prior cls.state).
//
// tyrquake: CL_Disconnect in NQ/cl_main.c.
func (s *State) Disconnect() {
	s.Connection = StateDisconnected
	s.Spawned = false
	s.Clear()
}

// SetConnecting transitions from [StateDisconnected] to
// [StateConnecting]. Returns [ErrAlreadyConnected] if the state
// is not currently disconnected.
//
// tyrquake: cls.state = ca_connected at the end of
// CL_EstablishConnection.
func (s *State) SetConnecting() error {
	if s.Connection != StateDisconnected {
		return ErrAlreadyConnected
	}
	s.Connection = StateConnecting
	return nil
}

// MarkSpawned transitions to [StateConnected] and flips Spawned
// true. Returns [ErrNotConnecting] if the state is not in
// [StateConnecting] (i.e. caller skipped the handshake or already
// completed it).
//
// tyrquake: cls.state = ca_active at the end of CL_SignonReply
// stage 4.
func (s *State) MarkSpawned() error {
	if s.Connection != StateConnecting {
		return ErrNotConnecting
	}
	s.Connection = StateConnected
	s.Spawned = true
	return nil
}

// RecordPing appends a latency sample to the rolling window. The
// ring-buffer index is NumPings mod [NumPingTimes]; NumPings is
// incremented unconditionally so callers can use it as a
// monotonic counter (the actual sample slot is computed via the
// modulo).
//
// tyrquake: cl.ping_times[host_client->num_pings & (NumPingTimes
// - 1)] in CL_ParseUpdate.
func (s *State) RecordPing(latency float32) {
	s.PingTimes[s.NumPings%NumPingTimes] = latency
	s.NumPings++
}

// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/sizebuf"
)

// ServerState tracks the lifecycle phase of the active map.
// StateLoading guards SV_SpawnServer-only operations (precaching
// models / sounds, writing the signon buffer); StateActive means
// the server is live + accepting per-tick frames + datagrams.
// tyrquake: server_state_t in NQ/server.h.
type ServerState int32

const (
	StateLoading ServerState = iota
	StateActive
)

// UserCmd is one client input frame: where the player is looking
// (viewangles) and which direction they intend to move (forward /
// side / up; in player-local "wishdir" units, NOT world vectors),
// plus the trigger-state bitfield (Buttons: BUTTON_ATTACK /
// BUTTON_JUMP / ...) and the impulse byte ("+impulse N" -- weapon
// switch + cheats + miscellaneous one-shot commands).
// SV_RunClients reads UserCmds off the wire + feeds them to
// sv_user's movement integrator. tyrquake: usercmd_t in NQ/client.h.
type UserCmd struct {
	ViewAngles  [3]float32
	ForwardMove float32
	SideMove    float32
	UpMove      float32
	Buttons     uint8 // BUTTON_ATTACK / BUTTON_JUMP / ... bitmask
	Impulse     uint8 // "+impulse N" weapon / cheat number
}

// Server is the per-map runtime state. Lives from SV_SpawnServer
// (map load) through SV_SendClientMessages (per-tick frames) until
// the next SV_SpawnServer wipes it. tyrquake: server_t in NQ/server.h.
//
// Precaches are sentinel-terminated slices sized to [MaxModels] /
// [MaxSounds] / [MaxLightStyles]: writes find the first empty slot,
// reads stop at the first empty slot (see [ModelIndex] /
// [SoundIndex]). This matches the C upstream's fixed-size arrays
// + NULL sentinel.
//
// Edicts is the per-map entity pool. NumEdicts is the index of the
// first FREE edict (i.e. the entity count so far). The world model
// always lives in Edicts[0]; client players in Edicts[1..MaxClients].
type Server struct {
	Active   bool // true once SV_SpawnServer finishes
	Paused   bool
	LoadGame bool // handle the spawn specially (savegame load)

	Time          float64 // simulation time, seconds since map load
	LastCheck     int     // PF_checkclient round-robin cursor
	LastCheckTime float64

	Name      string // map slug, e.g. "e1m1"
	ModelName string // "maps/<Name>.bsp", goes into ModelPrecache[0]

	WorldModel *model.BrushModel

	ModelPrecache []string       // [MaxModels], sentinel-terminated
	Models        []*model.Model // parallel to ModelPrecache
	// BrushModels is the parallel-to-ModelPrecache slice of per-
	// submodel [model.BrushModel] handles SpawnServer carves out of
	// the worldmodel's `*N` submodels so SOLID_BSP entities (doors,
	// lifts, train movers, breakables) can drive a per-entity
	// clipping hull through [world.HullForBounds]. Slot 0 is the
	// empty-string sentinel (nil); slot 1 is the worldmodel; slots
	// 2..N hold the per-submodel BrushModels. Slots for non-brush
	// precaches (.mdl alias / .spr sprite) stay nil -- the trace
	// dispatcher checks for nil before reaching for Hulls.
	BrushModels   []*model.BrushModel // parallel to ModelPrecache
	SoundPrecache []string            // [MaxSounds], sentinel-terminated
	LightStyles   []string            // [MaxLightStyles]

	Edicts    []*progs.Edict // [MaxEdicts]
	NumEdicts int            // first free slot
	MaxEdicts int            // cap on entity allocation

	// Arena is the per-map edict pool that backs every Edicts[i]'s
	// Fields slice. SpawnServer allocates a fresh one sized to
	// MaxEdicts; the VM needs a handle (via [progs.VM.SetArena]) so
	// the entity-pointer opcodes (OP_ADDRESS / OP_LOAD_ENT /
	// OP_STORE_P_*) can resolve QC pointers back to *Edict + field
	// byte-offset. Embedders that wire the arena onto their VM read
	// it from here AFTER SpawnServer returns -- or, to make it
	// available during the entity-spawn pass (which runs SpawnFn
	// per entity + needs the arena live for self.field = X writes),
	// pass an OnArenaReady hook via [SpawnDeps].
	Arena *progs.EdictArena

	State ServerState

	// Datagram (unreliable, per-tick): temp-entities, particle bursts,
	// sound events. Cleared every SV_SendClientMessages.
	Datagram *sizebuf.Buffer

	// ReliableDatagram (reliable, end-of-frame): mirrored into every
	// active client's per-frame message buffer.
	ReliableDatagram *sizebuf.Buffer

	// Signon (reliable, retained): the spawn-time handshake the
	// server replays when a new client connects mid-game.
	Signon *sizebuf.Buffer

	Protocol int // wire protocol version (one of protocol.Version*)
}

// Client is one player slot. Lives across maps (Static.Clients holds
// the pool) but per-map fields (Edict, Spawned, OldFrags) reset on
// SV_SpawnServer. tyrquake: client_t in NQ/server.h.
type Client struct {
	Active     bool // false = slot is free
	Spawned    bool // false = don't send datagrams to this slot yet
	DropAsap   bool // queued for disconnect on next SendClientMessages
	SendSignon bool // only meaningful before Spawned

	LastMessage float64 // wall-clock of the last reliable send

	// NetConnection is the per-client transport handle the netcode
	// layer holds (in upstream: struct qsocket_s*). Typed any here
	// so the server package stays decoupled from the netcode (which
	// hasn't been ported yet); the future netcode layer will assert
	// the concrete type.
	NetConnection any

	Cmd     UserCmd    // last movement input
	WishDir [3]float32 // world-space velocity intent, computed from Cmd

	// Message is the per-client reliable buffer. Filled across a
	// tick (events, console prints, baseline updates), copied + cleared
	// on each SV_SendClientMessages.
	Message *sizebuf.Buffer

	Edict  *progs.Edict // points at Server.Edicts[clientIdx+1]
	Name   string       // display name; max 32 bytes
	Colors int          // shirt/pants palette bits

	PingTimes [NumPingTimes]float32 // rolling-window latency samples
	NumPings  int                   // ring-buffer cursor

	SpawnParms [NumSpawnParms]float32 // carried across maps

	OldFrags int // for delta-encoding the scoreboard
}

// Static is the cross-map server state. Survives SV_SpawnServer
// (a new map keeps the same clients + serverflags). Owns the
// Clients pool. tyrquake: server_static_t in NQ/server.h.
type Static struct {
	MaxClients        int // current player-slot count (1..MaxClientsLimit)
	MaxClientsLimit   int // engine-side cap (= [MaxClients] = 16 for NQ)
	Clients           []*Client
	ServerFlags       int // episode-completion bits, carried across maps
	ChangeLevelIssued bool
}

// NewServer returns a Server with its precache slices + datagram /
// reliable_datagram / signon buffers pre-allocated to the static
// sizes the upstream uses. Caller still needs to call SV_SpawnServer-
// equivalent setup to fill in WorldModel + Edicts.
func NewServer() *Server {
	return &Server{
		ModelPrecache:    make([]string, MaxModels),
		Models:           make([]*model.Model, MaxModels),
		BrushModels:      make([]*model.BrushModel, MaxModels),
		SoundPrecache:    make([]string, MaxSounds),
		LightStyles:      make([]string, MaxLightStyles),
		Datagram:         sizebuf.New(make([]byte, MaxDatagram)),
		ReliableDatagram: sizebuf.New(make([]byte, MaxMsgLen)),
		Signon:           sizebuf.New(make([]byte, MaxMsgLen)),
	}
}

// NewClient returns a Client with its reliable Message buffer
// pre-allocated. All other fields default to zero (the slot is
// inactive until the netcode + SV_ConnectClient populate it).
func NewClient() *Client {
	return &Client{
		Message: sizebuf.New(make([]byte, MaxMsgLen)),
	}
}

// NewStatic returns a Static with maxclients slots pre-allocated
// (each [Client] initialised via [NewClient]). MaxClients +
// MaxClientsLimit both start at maxclients; the engine sets the
// limit higher if the netcode wants headroom.
func NewStatic(maxclients int) *Static {
	clients := make([]*Client, maxclients)
	for i := range clients {
		clients[i] = NewClient()
	}
	return &Static{
		MaxClients:      maxclients,
		MaxClientsLimit: maxclients,
		Clients:         clients,
	}
}

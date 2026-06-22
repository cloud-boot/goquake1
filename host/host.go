// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package host

import (
	"errors"
	"time"

	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/world"
)

// readClientMoves is the package-level seam Frame() walks to drain
// each active client's inbox before RunPhysics dispatches PhysicsWalk.
// The default is [server.ReadClientMoves]; tests swap it via the
// runClientCmds path (see Frame's hookable form below) so the cmd-
// drain step can be exercised without a real NetConn.
var readClientMoves = server.ReadClientMoves

// Host is the main game-loop coordinator. Owns the Server + Static
// + VM + World + per-frame timing + the ThinkCaller bridge.
// tyrquake: the global host_* variables in NQ/host.c (host_client,
// host_server, host_static, host_frametime, host_time, etc.).
//
// A zero Host is not usable; build one via [NewHost] which validates
// the required dependencies and pre-allocates the Server / Static
// / World pools at the configured client count.
type Host struct {
	Server   *server.Server
	Static   *server.Static
	VM       *progs.VM
	World    *world.World
	Cache    *model.Cache
	Resolver server.FileResolver

	// FrameTime is sv.frametime per tic (seconds; 0.05 = 20Hz).
	// Carried so [Host.Frame] callers can pass it as the dt value
	// when they have no per-frame wall-clock measurement of their
	// own. tyrquake: host_frametime.
	FrameTime float32

	// NowFn is the wall-clock the per-tic loop reads to advance
	// sv.time. nil falls back to a time.Now-based default.
	// tyrquake: Sys_FloatTime.
	NowFn func() float64

	// progsRef is the Progs pointer the bridge consults. Set via
	// [Host.SetProgs] -- the VM does not expose its bound Progs so
	// the host has to be told. nil means the bridge skips the
	// named-global hand-off (the per-think dispatch still runs).
	progsRef *progs.Progs

	// interner is the StringInterner SpawnServer hands to the
	// entity-spawn pass. Defaults to a noop returning offset 0
	// (the empty-string sentinel); production embedders override
	// via [Host.SetInterner] once a real progs.StringInterner exists.
	interner progs.StringInterner

	// spawnFn is the per-entity QC spawn-dispatch hook SpawnServer
	// hands to the entity-spawn pass. nil = entities still get their
	// fields populated from the BSP entity lump but no per-classname
	// QC initialiser runs (no monster health, no model, no solid).
	// Wired by the embedder via [Host.SetSpawnFn] once the VM has
	// a builtin table large enough for the spawn-time builtins
	// (precache_*, setmodel, setorigin, ...) the QC code calls.
	spawnFn func(ent *progs.Edict, classname string)

	// onArenaReady is the per-SpawnServer arena-publication hook.
	// SpawnServer calls it once, AFTER the EdictArena is allocated
	// + BEFORE the entity-spawn pass runs. Wired by the embedder
	// via [Host.SetOnArenaReady]; the production callback is
	// vm.SetArena so the per-entity SpawnFn dispatches see live
	// entity-pointer opcodes. nil = arena is still published on
	// Server.Arena but not handed to the VM mid-SpawnServer.
	onArenaReady func(arena *progs.EdictArena)
}

// ErrNilDep fires on a missing required NewHost dependency.
var ErrNilDep = errors.New("host: NewHost requires non-nil VM, Cache, Resolver")

// DefaultFrameTime is the per-tic interval the Go port defaults to
// when the caller doesn't override Host.FrameTime. 0.05 = 20Hz,
// matching tyrquake's sys_ticrate default.
const DefaultFrameTime float32 = 0.05

// defaultNowFn returns wall-clock seconds since the Unix epoch as a
// float64, the same shape the upstream's Sys_FloatTime exposes.
func defaultNowFn() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// NewHost wires up a fresh Host with the given dependencies.
// maxClients controls the [server.Static] pool size; values <= 0
// fall back to 1 (a single local-client loop is the minimum
// useful configuration).
//
// Returns ErrNilDep on missing required deps.
func NewHost(vm *progs.VM, cache *model.Cache, resolver server.FileResolver, maxClients int) (*Host, error) {
	if vm == nil || cache == nil || resolver == nil {
		return nil, ErrNilDep
	}
	if maxClients <= 0 {
		maxClients = 1
	}
	return &Host{
		Server:    server.NewServer(),
		Static:    server.NewStatic(maxClients),
		VM:        vm,
		World:     world.New(),
		Cache:     cache,
		Resolver:  resolver,
		FrameTime: DefaultFrameTime,
		NowFn:     defaultNowFn,
		interner:  func(string) int32 { return 0 },
	}, nil
}

// SetInterner overrides the [progs.StringInterner] the host hands
// to [server.SpawnServer]'s entity-spawn pass. The default is a
// noop that interns every string to offset 0; callers wire a real
// interner once their progs's string table is appendable.
func (h *Host) SetInterner(intern progs.StringInterner) {
	h.interner = intern
}

// SetSpawnFn installs the per-entity QC spawn-dispatch hook that
// [server.SpawnServer] hands to the entity-spawn pass. The hook is
// invoked once per parsed entity (after AssignFields has populated
// the edict's entvars) with the edict + its "classname" field value;
// the production implementation typically resolves classname via
// [progs.Progs.FindFunction], sets the QC "self" global to point at
// ent, and calls [progs.VM.Run] on the function index.
//
// nil disables the dispatch -- entities still parse + assign but no
// QC initialiser runs. The default for a fresh Host is nil since the
// dispatch needs a builtin table the embedder owns (host.go can't
// pre-wire it without growing a dependency on every progs builtin
// the spawn-time QC code calls).
func (h *Host) SetSpawnFn(fn func(ent *progs.Edict, classname string)) {
	h.spawnFn = fn
}

// SetOnArenaReady installs the arena-publication hook
// [server.SpawnServer] fires once per map load, right after the
// per-map [progs.EdictArena] is allocated + reset and BEFORE the
// entity-spawn pass dispatches the first SpawnFn. Production
// embedders pass [progs.VM.SetArena] (or a closure wrapping it)
// so the VM has the arena handle the entity-pointer opcodes
// (OP_ADDRESS / OP_LOAD_ENT / OP_STORE_P_*) need to resolve
// self.field = X writes inside spawn-time QC.
//
// nil disables the hook; the arena is still stashed on
// Server.Arena so callers can pick it up post-SpawnServer.
func (h *Host) SetOnArenaReady(fn func(arena *progs.EdictArena)) {
	h.onArenaReady = fn
}

// edictAt is the [world.PhysicsEdictResolver] the per-tic RunPhysics
// dispatcher walks. Returns nil for any out-of-range index (the
// dispatcher treats nil as "free slot, skip").
func (h *Host) edictAt(i int) *progs.Edict {
	if i < 0 || i >= len(h.Server.Edicts) {
		return nil
	}
	return h.Server.Edicts[i]
}

// cmdAt is the [world.PhysicsCmdResolver] the per-tic dispatcher
// passes through to the (future) PhysicsWalk handler. Client slots
// are 1..MaxClients (slot 0 is the world); any other index returns
// a zero UserCmd, matching the C upstream's per-non-client
// MOVETYPE_WALK guard (which simply doesn't dispatch Walk for
// non-client entities).
func (h *Host) cmdAt(i int) server.UserCmd {
	if i < 1 || i > h.Static.MaxClients {
		return server.UserCmd{}
	}
	c := h.Static.Clients[i-1]
	if c == nil {
		return server.UserCmd{}
	}
	return c.Cmd
}

// hostKeyAt is the [world.PhysicsKeyResolver] the dispatcher uses:
// edict slot index == area-tree Key (the canonical identity
// mapping; future maps that re-key clients vs monsters can swap
// this for a non-identity function).
func hostKeyAt(i int) world.Key { return world.Key(i) }

// runClientCmds drains every active client's inbox via ReadClientMoves
// then copies the resulting Cmd.ViewAngles into the bound edict's
// `v_angle` entvars field. tyrquake: SV_RunClients in NQ/host.c -- the
// per-tic SV_ReadClientMessage + SV_ClientThink call pair that runs
// just before SV_Physics so PhysicsWalk sees the freshest UserCmd +
// view angles.
//
// The v_angle copy is the minimal SV_RunCmd equivalent: PhysicsWalk's
// CalcWishVel reads v_angle (NOT the cmd's ViewAngles) for the
// forward/right basis, so without this propagation the player's
// wishvel would always lock to v_angle's last value (zero at spawn).
//
// Slots without an Edict yet (ConnectClient ran but the edict pool
// wasn't ready) and slots without a v_angle field (test stubs with
// stripped progs) are skipped silently. Other errors -- a transport
// failure, a bad clc opcode -- are surfaced verbatim so callers can
// log + drop.
func (h *Host) runClientCmds() error {
	p := h.findProgs()
	for _, c := range h.Static.Clients {
		if c == nil || !c.Active || c.NetConnection == nil {
			continue
		}
		if _, err := readClientMoves(c); err != nil {
			return err
		}
		if p == nil || c.Edict == nil {
			continue
		}
		// NewEntVars's nil-arg guard is unreachable here -- the p and
		// c.Edict checks above ensure both inputs are non-nil -- so its
		// error is dropped (bsptrace-style).
		ev, _ := progs.NewEntVars(p, c.Edict)
		// v_angle absent (stripped test progs) -> silent skip. A real
		// Q1 progs.dat always declares it.
		_ = ev.WriteVec3("v_angle", c.Cmd.ViewAngles)
	}
	return nil
}

// Frame runs one game tic: per-tic SV_Physics + SendClientFrames +
// CleanupEnts + ClearDatagram + ClearReliableDatagram. tyrquake:
// Host_Frame's server-side portion (the SV_Physics + SV_SendClient-
// Messages + SV_CleanupEnts + SV_ClearDatagram block in NQ/host.c).
//
// Caller passes the current frame-time delta in seconds (typically
// [Host.FrameTime]). The host advances sv.time by dt before the
// physics pass so per-tic think deadlines align with the new tic.
//
// On a server that has not yet been SpawnServer'd (s.Active == false),
// Frame is a no-op -- there is no per-tic work without a map loaded.
//
// Returns the first error from any step; nil on a clean frame.
func (h *Host) Frame(dt float32) error {
	if !h.Server.Active {
		return nil
	}

	// Advance sv.time. The C upstream does this at the bottom of
	// Host_Frame after SV_Physics; the Go port does it up-front so
	// per-tic think deadlines (RunThink's now+dt window) align with
	// the tic the dispatched code sees. The semantic difference is
	// nil for a single-tic test: every think scheduled this frame
	// fires this frame either way.
	h.Server.Time += float64(dt)

	// SV_RunClients-equivalent: drain each active client's inbox into
	// its Client.Cmd, then mirror Cmd.ViewAngles into the bound edict's
	// v_angle field so PhysicsWalk's wishvel basis is fresh this tic.
	// Runs BEFORE RunPhysics so PhysicsWalk picks up the post-drain
	// cmd via cmdAt.
	if err := h.runClientCmds(); err != nil {
		return err
	}

	ctx := world.PhysicsContext{
		Worldmodel:  h.Server.WorldModel,
		Candidates:  nil,
		Now:         float32(h.Server.Time),
		Dt:          dt,
		ThinkCaller: h.thinkCaller,
	}
	if err := world.RunPhysics(
		h.Server.NumEdicts,
		server.DefaultPhysParams(),
		ctx,
		h.edictAt,
		h.cmdAt,
		hostKeyAt,
		h.findProgs(),
	); err != nil {
		return err
	}

	// SendClientFrames is best-effort per-client; its PerClientErrs
	// surface here as the first non-nil entry (so the caller can
	// log + decide whether to DropClient). The slice itself is
	// dropped after the scan -- the host loop has no per-client
	// retry policy yet.
	res := h.Server.SendClientFrames(h.Static)
	for _, perr := range res.PerClientErrs {
		if perr != nil {
			return perr
		}
	}

	// End-of-frame: clear muzzleflash effect bits, then drop the
	// per-tic unreliable + reliable buffers so the next tic starts
	// with a fresh canvas.
	h.Server.CleanupEnts(nil)
	h.Server.ClearDatagram()
	h.Server.ClearReliableDatagram()
	return nil
}

// SpawnServer wraps server.Server.SpawnServer with the host's
// configured deps (Cache, Resolver, the VM's bound Progs, the
// World, and the Static client pool). tyrquake: the SV_SpawnServer
// call inside Host_Cmd's "map" command handler.
//
// The bound Progs comes from h.VM via [findProgs]; the host owns
// the VM, which owns the Progs, so the caller doesn't need to
// re-supply it. Returns SpawnServer's error verbatim.
func (h *Host) SpawnServer(mapName string, protocol int) error {
	deps := server.SpawnDeps{
		Cache:    h.Cache,
		Resolver: h.Resolver,
		Progs:    h.findProgs(),
		Static:   h.Static,
		World:    h.World,
		// SpawnFn is whatever [Host.SetSpawnFn] last installed; nil
		// = entities still get their fields populated from the BSP
		// entity lump but no per-classname QC initialiser runs. The
		// embedder wires the real dispatch once the VM has a builtin
		// table large enough for the spawn-time builtins the QC code
		// calls (precache_*, setmodel, setorigin, ...).
		SpawnFn: h.spawnFn,
		// Interner is a noop-zero by default: every string field
		// interns to offset 0 (the empty-string sentinel). The map
		// still parses; only the human-readable string payload is
		// dropped. Production embedders override via [Host.SetInterner]
		// once a real progs.StringInterner exists.
		Interner: h.interner,
		// OnArenaReady is the per-SpawnServer arena-publication
		// hook; nil = arena is still stashed on Server.Arena post-
		// SpawnServer, just not handed to the VM mid-spawn.
		OnArenaReady: h.onArenaReady,
	}
	return h.Server.SpawnServer(mapName, protocol, deps)
}

// ConnectLoopback creates a loopback NetConn pair, binds the
// server-side half to a free client slot, and returns the client-
// side half for the local client to read/write.
//
// tyrquake: the implicit "local client" connection that
// Host_InitLocal sets up before the first PR_ExecuteProgram dispatch.
//
// Returns:
//
//	(clientSide, slotIdx, nil)            -- happy path
//	(nil, -1, ErrNoFreeClientSlot)        -- pool is full
//
// The server-side half is held by Static.Clients[slotIdx]
// .NetConnection (as an `any`); callers that need to talk to it
// type-assert to *server.LoopbackConn.
func (h *Host) ConnectLoopback() (server.NetConn, int, error) {
	clientSide, serverSide := server.NewLoopbackConn()
	now := h.NowFn()
	// Pre-compute the slot index the upcoming ConnectClient call
	// will pick (the first non-active slot). makeEdict needs the
	// index *before* ConnectClient flips the Active flag, since
	// ConnectClient calls makeEdict only after setting Active = true
	// -- so a post-hoc scan inside makeEdict would find the *next*
	// free slot, not the one being bound.
	freeSlot := -1
	for i, c := range h.Static.Clients {
		if !c.Active {
			freeSlot = i
			break
		}
	}
	makeEdict := func() *progs.Edict {
		// SpawnServer reserves Server.Edicts[1..MaxClients] for
		// clients (slot 0 is the world); if SpawnServer hasn't run
		// yet, the pool is empty + this returns nil. The upstream
		// lifecycle crashes on the first per-client progs dispatch
		// in that case; the Go port surfaces it as a nil-edict
		// client the per-tic physics guards against.
		if freeSlot < 0 || freeSlot+1 >= len(h.Server.Edicts) {
			return nil
		}
		return h.Server.Edicts[freeSlot+1]
	}
	idx, err := server.ConnectClient(h.Static, serverSide, now, makeEdict)
	if err != nil {
		return nil, -1, err
	}
	return clientSide, idx, nil
}

// thinkCaller is the [server.ThinkCaller] hook the host installs on
// every per-tic Physics call. Translates a QC function index into a
// [progs.VM.Run] invocation, after setting the QC self global to
// point at the thinking edict and the time global to the current
// sv.time.
//
// Wiring approach: the Go port's VM has SetGlobalInt /
// SetGlobalFloat keyed by global-pool slot offsets, but no named
// "self" / "other" / "time" accessor -- the upstream uses a C
// pr_global_struct overlaid on the start of pr_globals at fixed
// offsets, which is a layout convention this Go port leaves to the
// embedder. The bridge looks up the named globals via Progs.FindGlobal;
// progs.dat files that don't declare them (test stubs, custom
// minimalist QC) silently skip the write -- the per-think dispatch
// still happens, just without the global hand-off. Real Q1 progs.dat
// always declares all three.
//
// Returns the propagated VM.Run error; nil on success.
func (h *Host) thinkCaller(ent *progs.Edict, funcID int32) error {
	p := h.findProgs()
	if p != nil {
		if def := p.FindGlobal("self"); def != nil {
			// Self is an ev_entity, stored as the byte-offset
			// pointer the arena's MakePointer produces. The VM
			// owns the same arena via SetArena; if the arena is
			// not wired, the per-edict pointer arithmetic is
			// undefined -- but FindGlobal returning non-nil means
			// the embedder declared the field, so we trust the
			// embedder also wired the arena.
			_ = h.VM.SetGlobalInt(int(def.Ofs), int32(h.entityPointer(ent)))
		}
		if def := p.FindGlobal("other"); def != nil {
			// Default other to the world edict (index 0), matching
			// the C upstream's PR_ExecuteProgram entry convention.
			_ = h.VM.SetGlobalInt(int(def.Ofs), int32(h.entityPointer(h.worldEdict())))
		}
		if def := p.FindGlobal("time"); def != nil {
			_ = h.VM.SetGlobalFloat(int(def.Ofs), float32(h.Server.Time))
		}
	}
	return h.VM.Run(funcID)
}

// entityPointer returns the QC entity-pointer (a byte offset into
// the edict arena) for ent. Falls back to 0 (the world pointer)
// when ent is nil, matching the upstream's "every nil ent is the
// world" convention inside PR_ExecuteProgram.
//
// SCOPE NOTE: the VM has no exported arena accessor on this port,
// so the host returns 0 for every entity here. The QC bridge still
// sets self / other globals; their pointer value just always
// references the world edict (index 0). A follow-up that exposes
// the VM's arena will let entityPointer translate ent into the
// arena's per-edict byte offset. The bridge's primary contract --
// dispatch vm.Run(funcID) -- is satisfied either way.
func (h *Host) entityPointer(ent *progs.Edict) int {
	if ent == nil {
		return 0
	}
	// Without an exported arena handle, the byte-offset is opaque
	// from outside the VM. Return 0 as a sentinel; the test
	// harness asserts the bridge's vm.Run contract directly.
	return 0
}

// worldEdict returns Server.Edicts[0] -- the world entity the C
// upstream pins as pr_global_struct->other's default before each
// think dispatch. nil when SpawnServer has not run.
func (h *Host) worldEdict() *progs.Edict {
	if len(h.Server.Edicts) == 0 {
		return nil
	}
	return h.Server.Edicts[0]
}

// findProgs returns the Progs the VM is bound to. The VM exposes no
// direct accessor for its bound progs (it owns a writable copy of
// the globals, but the Functions / FieldDefs / GlobalDefs come from
// the original Progs the embedder loaded). The Host stashes the
// reference via [Host.SetProgs]; production callers call it once
// after NewVM, passing the same *Progs they fed into NewVM.
func (h *Host) findProgs() *progs.Progs {
	return h.progsRef
}

// SetProgs records the Progs the bridge should reach for when
// resolving the self / other / time globals + the per-tic physics
// dispatch's entvars binds. Callers pass the same *Progs they used
// to build [Host.VM]. Safe to call before or after [Host.SpawnServer].
// nil is accepted -- the bridge falls back to the no-named-global
// path + RunPhysics surfaces the nil-progs guard verbatim.
func (h *Host) SetProgs(p *progs.Progs) {
	h.progsRef = p
}

// Progs returns the [progs.Progs] last installed via [Host.SetProgs]
// (nil if none). Exposed so embedders that need to issue ad-hoc
// EntVars reads / writes against the host's edict pool can construct
// the [progs.EntVars] view without re-loading the bytecode. The host
// itself uses the unexported alias [Host.findProgs] internally so the
// accessor surface stays narrow + opt-in.
func (h *Host) Progs() *progs.Progs {
	return h.progsRef
}

// ErrNoEdict fires when [Host.EdictOrigin] is asked for a slot that
// has no live edict -- either SpawnServer has not run (the pool is
// empty) or the requested slot is past the end of the allocated pool.
var ErrNoEdict = errors.New("host: edict slot out of range")

// ErrNoProgs fires when [Host.EdictOrigin] is called without a Progs
// bound -- the field-name -> offset lookup needs the FieldDefs table,
// which only the embedder-supplied Progs carries.
var ErrNoProgs = errors.New("host: EdictOrigin needs a bound Progs")

// EdictOrigin returns the value of the per-edict QC "origin" vector
// at slot in the active server's edict pool. tyrquake: ent->v.origin
// for ent = sv.edicts[slot] (the world entity is slot 0; clients
// occupy slots 1..MaxClients).
//
// Bring-up use case: per-frame camera-follow reads slot 1 (the local
// player) and uses the returned origin as the [render.RefDef]
// ViewOrigin. Until the QC SpawnFn hook is wired the player edict's
// origin field stays at the bytecode default (typically {0,0,0});
// callers detect that case and fall back to a fixed anchor.
//
// Returns ErrNoEdict when slot is out of range, ErrNoProgs when no
// Progs is bound, and ErrFieldNotFound / ErrFieldTypeMismatch from
// the underlying [progs.EntVars] read.
//
// NewEntVars's nil-arg guard is unreachable here -- the slot + progs
// guards above ensure both inputs are non-nil -- so its error is
// dropped (bsptrace-style) rather than threaded through.
func (h *Host) EdictOrigin(slot int) ([3]float32, error) {
	if slot < 0 || slot >= len(h.Server.Edicts) {
		return [3]float32{}, ErrNoEdict
	}
	ent := h.Server.Edicts[slot]
	if ent == nil {
		return [3]float32{}, ErrNoEdict
	}
	p := h.findProgs()
	if p == nil {
		return [3]float32{}, ErrNoProgs
	}
	v, _ := progs.NewEntVars(p, ent)
	return v.ReadVec3("origin")
}

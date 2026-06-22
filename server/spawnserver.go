// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"fmt"

	"github.com/go-quake1/engine/entparse"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/progs"
)

// AreaClearer is the area-tree builder hook SpawnServer calls after
// the worldmodel is loaded. tyrquake: SV_ClearWorld. The real
// implementation lives in package world (*world.World.Clear), which
// imports this package -- the interface here avoids the import cycle
// without forcing the server package to know about the world type.
type AreaClearer interface {
	Clear(mins, maxs [3]float32)
}

// SpawnDeps bundles the external resources SpawnServer needs.
// Caller (the future host layer) builds it once + reuses across
// map loads.
//
// Required:
//
//	Cache    -- model cache; the worldmodel and every submodel get
//	            cached + indexed by their precache name
//	Resolver -- bytes-by-name resolver (PAK walker, embedded asset
//	            reader, in-memory map for tests)
//	Progs    -- loaded QC progs; AssignFields reads the field-def
//	            table from here to map entity keys to per-edict
//	            byte offsets
//	Static   -- cross-map state; MaxClients drives the post-load
//	            NumEdicts seed
//	World    -- area-tree builder; satisfied by *world.World
//
// Optional:
//
//	SpawnFn       -- per-entity QC spawn dispatch. nil = entities get
//	                 their fields populated but no spawn hook fires.
//	Interner      -- progs.StringInterner the AssignFields layer uses
//	                 for string-typed fields (classname etc.). nil
//	                 means string fields error on assign -- callers
//	                 that want to ingest a map must supply one.
//	OnArenaReady  -- fires once, right after the per-map EdictArena
//	                 is allocated + reset, BEFORE the entity-spawn
//	                 pass runs. Lets the embedder hand the arena to
//	                 their VM (via [progs.VM.SetArena]) so the
//	                 entity-pointer opcodes resolve during SpawnFn.
//	                 nil = skip; the arena is still stored on
//	                 Server.Arena for post-spawn pickup.
type SpawnDeps struct {
	Cache        *model.Cache
	Resolver     FileResolver
	Progs        *progs.Progs
	Static       *Static
	World        AreaClearer
	SpawnFn      func(ent *progs.Edict, classname string)
	Interner     progs.StringInterner
	OnArenaReady func(arena *progs.EdictArena)
}

// ErrSpawnServerNilDeps fires on missing required deps.
var ErrSpawnServerNilDeps = errors.New("server: SpawnServer requires non-nil Cache, Resolver, Progs, Static, World")

// ErrSpawnServerNotBrush fires when the worldmodel resolver returns
// a non-brush model (alias .mdl / sprite .spr blob masquerading as a
// .bsp). tyrquake: SV_SpawnServer Sys_Errors on this via
// Mod_ForName's brush-only contract -- the Go port surfaces it as a
// typed error.
var ErrSpawnServerNotBrush = errors.New("server: worldmodel is not a brush model")

// SpawnServer loads a new map into the Server: resets state, fetches
// the worldmodel via deps.Cache, builds + walks the area tree,
// parses the entities lump, populates the edict pool, runs the
// per-entity QC spawn dispatch.
//
// tyrquake: SV_SpawnServer in NQ/sv_main.c.
//
// Steps (matching the C order):
//
//  1. Validate deps (returns ErrSpawnServerNilDeps on missing).
//  2. s.Reset(mapName, protocol) -- clears precaches + buffers +
//     flips State to StateLoading + allocates the Edicts pool.
//     Propagates ErrEmptyMapName on empty mapName.
//  3. Load the worldmodel: bspPath = MapBSPPath(mapName); m, err =
//     LoadModelByName(deps.Cache, bspPath, deps.Resolver). Set
//     s.WorldModel = LoadBrush(m.Brush, 0); errors on non-brush.
//  4. Build the per-hull bsptrace.Hulls via model.LoadBrush(file, 0)
//     -- folded into step 3 since LoadBrush returns the BrushModel
//     with Hulls already populated.
//  5. deps.World.Clear(worldmodel.Mins, worldmodel.Maxs) using the
//     worldmodel's own bounds from the bspfile.Models lump (NOT the
//     hull's clip bounds, which are player/monster body sizes).
//  6. Reserve client slots: s.NumEdicts = deps.Static.MaxClients + 1
//     (world at idx 0, clients at idx 1..MaxClients). The per-slot
//     *Edict references are pulled from a fresh EdictArena sized to
//     s.MaxEdicts; the arena is the slot allocator used by the
//     entity-spawn pass.
//  7. Populate the model precache: s.ModelPrecache[0] is the empty
//     sentinel (tyrquake's pr_strings reference), s.ModelPrecache[1]
//     = bspPath + s.Models[1] = the worldmodel; then walk the
//     worldmodel's submodels: s.ModelPrecache[1+i] = LocalModelName(i)
//     for i >= 1 + s.Models[1+i] = LoadModelByName(deps.Cache,
//     localName, resolver). Submodels' loader entries are the world
//     bspfile re-cached under the "*N" alias (Mod_ForName-equivalent
//     semantics).
//  8. Parse + assign entities: entFields = ParseEntities(file.
//     Entities()); SpawnEntities(entFields, deps.Progs, edictAt,
//     deps.Interner, deps.SpawnFn) where edictAt resolves the per-
//     entity slot off the arena built in step 6.
//  9. Set s.Active = true; s.State = StateActive.
//
// SIMPLIFICATIONS (deliberate scope cuts vs the C upstream):
//
//   - SKIP "run 2 frames to settle" (the upstream's
//     `host_frametime = 0.1; SV_Physics(); SV_Physics()`) -- the
//     caller can do this externally via SV_Physics once it's wired.
//   - SKIP SV_CreateBaseline (a separate per-edict snapshot pass) --
//     the caller can run it externally via this package's
//     baseline.go.
//   - SKIP SV_SendServerinfo (per-client handshake) -- the caller
//     iterates clients + calls EncodeServerInfo (serverinfo.go).
//   - SKIP coop / deathmatch cvar coupling -- those are gameplay-
//     mode-specific and live in cvars.go.
//   - SKIP the pr_global_struct.serverflags / mapname assignment --
//     needs progs globals access this layer doesn't have yet.
//   - SKIP the loadgame branch -- savegame loading is a separate
//     flow that bypasses SpawnServer's entity-parse pass.
//
// Returns the first error encountered. On failure, the Server is
// left in a partial state -- the caller is expected to abandon it
// + retry with a fresh NewServer().
func (s *Server) SpawnServer(mapName string, protocol int, deps SpawnDeps) error {
	// Step 1: deps validation.
	if deps.Cache == nil || deps.Resolver == nil || deps.Progs == nil || deps.Static == nil || deps.World == nil {
		return ErrSpawnServerNilDeps
	}

	// Step 2: per-map state reset. Propagates ErrEmptyMapName for an
	// empty mapName -- this is the only error path Reset takes on
	// SpawnServer's input domain.
	if err := s.Reset(mapName, protocol); err != nil {
		return err
	}

	// Step 3 + 4: worldmodel. MapBSPPath only fails on empty name,
	// which Reset already rejected, so the error here is unreachable
	// in this code path (dropped bsptrace-style).
	bspPath, _ := MapBSPPath(mapName)
	world, err := LoadModelByName(deps.Cache, bspPath, deps.Resolver)
	if err != nil {
		return fmt.Errorf("server: load worldmodel %q: %w", bspPath, err)
	}
	if world.Kind != model.KindBrush || world.Brush == nil {
		return fmt.Errorf("%w: %q kind=%d", ErrSpawnServerNotBrush, bspPath, world.Kind)
	}
	worldBrush, err := model.LoadBrush(world.Brush, 0)
	if err != nil {
		return fmt.Errorf("server: build hulls for %q: %w", bspPath, err)
	}
	s.WorldModel = worldBrush

	// Step 5: area tree. Use the worldmodel's bspfile Models[0]
	// bounds (the full map AABB), NOT the per-hull clip mins/maxs
	// (which are entity body sizes). Models() already succeeded
	// inside LoadBrush so its error path here is the same drop-the-
	// unreachable-error pattern.
	models, _ := world.Brush.Models()
	deps.World.Clear(models[0].Mins, models[0].Maxs)

	// Step 6: edict pool + client-slot reservation. Allocate a
	// fresh arena sized to s.MaxEdicts (set by Reset). The arena
	// owns the underlying Edict storage; s.Edicts holds pointers
	// into it so existing accessors (s.Edicts[i].Fields etc.) keep
	// working.
	arena := progs.NewEdictArena(deps.Progs, s.MaxEdicts)
	arena.Reset()
	for i := 0; i < s.MaxEdicts; i++ {
		// arena.Get only fails on out-of-range index; the loop bound
		// matches Cap exactly so the error path is unreachable.
		e, _ := arena.Get(i)
		s.Edicts[i] = e
	}
	// Publish the arena onto the Server so callers can pick it up
	// post-SpawnServer, and fire the optional OnArenaReady hook so
	// embedders that need it live for the upcoming entity-spawn
	// pass (e.g. progs.VM.SetArena callers, whose entity-pointer
	// opcodes resolve via this arena) wire it in BEFORE SpawnFn
	// dispatches the first entity.
	s.Arena = arena
	if deps.OnArenaReady != nil {
		deps.OnArenaReady(arena)
	}
	// World at idx 0, clients at idx 1..MaxClients. NumEdicts marks
	// the first free slot, so the entity-spawn pass starts past the
	// reserved range.
	s.NumEdicts = deps.Static.MaxClients + 1

	// Step 7: model precache. Slot 0 is the empty-string sentinel
	// (tyrquake parks the pr_strings empty-string offset there as a
	// "no model" marker); slot 1 is the worldmodel under its full
	// "maps/<name>.bsp" name; slots 2..N are the brushmodel submodels
	// under their "*1", "*2", ... aliases.
	s.ModelPrecache[0] = ""
	s.ModelPrecache[1] = bspPath
	s.Models[1] = world
	for i := 1; i < len(models); i++ {
		// LocalModelName errors only for i < 0 or i >= MaxModels;
		// the loop bound clamps to len(models) which the cache-
		// indirectly bounds via the bsp Models lump (capped at
		// MaxModels - 1 by the precache slot count). Drop the
		// unreachable error path.
		name, _ := LocalModelName(i)
		s.ModelPrecache[1+i] = name
		// Submodel slots share the worldmodel's bspfile.File -- the
		// upstream's Mod_ForName treats "*N" as an in-bsp reference,
		// not a separate file, so the *Model wrapper points at the
		// same parsed bspfile. The per-submodel hull carving
		// (model.LoadBrush(file, N)) is the host layer's job and is
		// done lazily on first collision query, not here.
		s.Models[1+i] = world
	}

	// Step 8: entity parse + spawn. ParseEntities is tolerant of an
	// empty entities lump (returns nil, nil), in which case
	// SpawnEntities is a no-op.
	entFields, err := entparse.ParseEntities(world.Brush.Entities())
	if err != nil {
		return fmt.Errorf("server: parse entities: %w", err)
	}
	// edictAt is the slot allocator the entity-spawn pass calls per
	// parsed entity. tyrquake: ED_LoadFromFile reserves entity[0] for
	// worldspawn (sv.edicts[0]) and ED_Allocs every subsequent entity
	// past the reserved client range (sv.num_edicts++). The Go port
	// mirrors that exactly: entity index 0 -> slot 0; entity index >0
	// -> s.NumEdicts++ (which already starts at MaxClients+1 from
	// step 6, so the first non-world entity lands at the first
	// post-client slot). Out-of-range returns nil; SpawnEntities then
	// records the per-slot allocation failure + moves on.
	edictAt := func(i int) *progs.Edict {
		if i == 0 {
			return s.Edicts[0]
		}
		slot := s.NumEdicts
		if slot >= s.MaxEdicts {
			return nil
		}
		s.NumEdicts++
		// Match the upstream's ED_Alloc semantics: the allocator
		// claims a free slot + flips its Free flag false so subsequent
		// walks (CleanupEnts, SV_CreateBaseline, server-side PVS) see
		// it as live. The arena's Reset starts every non-world slot
		// at Free=true so unclaimed slots stay invisible to those
		// walks; without this flip the entity-spawn pass populates
		// fields but leaves the slot administratively "free", which
		// silently hides every parsed entity from every per-edict
		// per-frame loop (SV_CreateBaseline skipped 539/540 slots in
		// the bring-up before this fix).
		e := s.Edicts[slot]
		e.Free = false
		e.FreeTime = 0
		return e
	}
	if err := entparse.SpawnEntities(entFields, deps.Progs, edictAt, deps.Interner, deps.SpawnFn); err != nil {
		return fmt.Errorf("server: spawn entities: %w", err)
	}

	// Step 9: arm the server.
	s.Active = true
	s.State = StateActive
	return nil
}

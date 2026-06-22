// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package host

import (
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/world"
)

// TraceResult is the shape every TraceLine call returns. Mirrors
// tyrquake's pr_global_struct->trace_* fields (the QC-visible globals
// PF_traceline writes after running the swept-line clip):
//
//   - AllSolid    -- entire move was inside a solid volume
//   - StartSolid  -- the start point was inside a solid volume
//   - Fraction    -- 0..1, the fraction of the move that was clear
//   - EndPos      -- where the trace ended (start + (end-start)*Fraction)
//   - PlaneNormal -- the impact plane normal (zero on a clean trace)
//   - PlaneDist   -- the impact plane distance (zero on a clean trace)
//   - EntIdx      -- index of the entity that clipped the trace
//     (-1 = no clip; 0 = world; >0 = a sv.edicts[] slot)
//   - InOpen      -- trace endpoint is in an empty leaf (CONTENTS_EMPTY)
//   - InWater     -- trace endpoint is in a water leaf (CONTENTS_WATER /
//     LAVA / SLIME / SKY -- the upstream lumps them
//     together via PointContents == EMPTY ? open : water)
//
// The shape is what the QC builtin closure writes back into the named
// globals trace_allsolid / trace_startsolid / trace_fraction /
// trace_endpos / trace_plane_normal / trace_plane_dist / trace_ent /
// trace_inopen / trace_inwater.
type TraceResult struct {
	AllSolid    bool
	StartSolid  bool
	Fraction    float32
	EndPos      [3]float32
	PlaneNormal [3]float32
	PlaneDist   float32
	EntIdx      int
	InOpen      bool
	InWater     bool
}

// MoveMode mirrors tyrquake's MOVE_* enum: MoveNormal clips the line
// against monsters + world; MoveNoMonsters skips non-BSP solid edicts
// (so traceline can probe the static map geometry without monsters
// blocking sightlines).
type MoveMode int

const (
	MoveNormal MoveMode = iota
	MoveNoMonsters
)

// TraceLine runs a point-line swept trace from start to end against
// the world brushmodel + every linked candidate edict in h.Server,
// excluding the passEdict (the entity calling traceline -- typically
// the monster itself so it doesn't clip against its own bounds).
//
// tyrquake: PF_traceline (pr_cmds.c).
//
// On a host without a Server / WorldModel / Edicts pool the trace
// is a no-op clean ray (Fraction=1, EndPos=end, InOpen=true) so
// builtins remain safe to call before SpawnServer.
//
// Returns an error only when the underlying world.TraceMove surfaces
// one (a malformed BrushModel hull tree); a clean miss or a clean
// impact both return (TraceResult, nil).
func (h *Host) TraceLine(start, end [3]float32, mode MoveMode, passEdict *progs.Edict) (TraceResult, error) {
	res := TraceResult{
		Fraction: 1,
		EndPos:   end,
		EntIdx:   -1,
		InOpen:   true,
	}

	if h == nil || h.Server == nil || h.Server.WorldModel == nil {
		return res, nil
	}

	// Build the candidate list: every non-free, non-pass solid edict
	// whose absbounds overlap the swept line. The area-tree walk
	// already returns only linked solid edicts; we further drop the
	// pass-edict + (for MoveNoMonsters) FL_MONSTER-flagged edicts.
	mins := [3]float32{0, 0, 0}
	maxs := [3]float32{0, 0, 0}
	smin, smax := world.SweptBounds(start, end, mins, maxs)

	candidates := []world.Target{}
	candIdx := []int{} // edict slot for each candidate (parallel to candidates)

	if h.World != nil {
		keys := h.World.AreaQuery(smin, smax, world.QuerySolidsOnly)
		passIdx := -1
		if passEdict != nil {
			for i, ed := range h.Server.Edicts {
				if ed == passEdict {
					passIdx = i
					break
				}
			}
		}
		p := h.findProgs()
		for _, k := range keys {
			idx := int(k)
			if idx <= 0 || idx >= len(h.Server.Edicts) {
				continue
			}
			if idx == passIdx {
				continue
			}
			ed := h.Server.Edicts[idx]
			if ed == nil || ed.Free {
				continue
			}
			// Without a progs binding we cannot extract per-edict
			// origin / mins / maxs / solid -- skip the candidate
			// rather than guess. The world trace still runs.
			if p == nil {
				continue
			}
			ev, _ := progs.NewEntVars(p, ed)
			solidF, err := ev.ReadFloat("solid")
			if err != nil {
				continue
			}
			sol := server.Solid(int32(solidF))
			if sol == server.SolidNot {
				continue
			}
			// MoveNoMonsters: drop any non-BSP solid (= bbox/slidebox/
			// trigger). Monsters use SolidSlideBox; doors / movers
			// use SolidBSP and stay in the trace.
			if mode == MoveNoMonsters && sol != server.SolidBSP {
				continue
			}
			origin, _ := ev.ReadVec3("origin")
			emins, _ := ev.ReadVec3("mins")
			emaxs, _ := ev.ReadVec3("maxs")
			tgt := world.Target{
				Origin: origin,
				Mins:   emins,
				Maxs:   emaxs,
				Solid:  sol,
			}
			if sol == server.SolidBSP {
				// SOLID_BSP (sub-model doors / movers / breakables)
				// clips need the per-entity model.BrushModel. The
				// modelindex field on the edict is the per-precache
				// slot the worldmodel's submodel "*N" was carved into
				// at SpawnServer time (Server.BrushModels[idx]).
				// tyrquake: SV_HullForEntity's
				// `model = sv.models[(int)ent->v.modelindex]` lookup.
				miF, err := ev.ReadFloat("modelindex")
				if err != nil {
					// No modelindex field on this edict -> can't
					// resolve the submodel hulls. Drop the candidate
					// rather than fall through to the boxhull path
					// (which would over-occlude with the entity's
					// bbox, not its real BSP geometry).
					continue
				}
				mi := int(miF)
				if mi <= 0 || mi >= len(h.Server.BrushModels) {
					continue
				}
				bm := h.Server.BrushModels[mi]
				if bm == nil {
					// Submodel slot carved nil at SpawnServer (corrupt
					// bsp lump, or an alias/sprite precache slot). Skip
					// rather than over-occlude with the bbox.
					continue
				}
				tgt.BrushModel = bm
			}
			candidates = append(candidates, tgt)
			candIdx = append(candIdx, idx)
		}
	}

	tr, err := world.TraceMove(h.Server.WorldModel, candidates, start, mins, maxs, end)
	if err != nil {
		return res, err
	}

	res.AllSolid = tr.Trace.AllSolid
	res.StartSolid = tr.Trace.StartSolid
	res.Fraction = tr.Trace.Fraction
	res.EndPos = tr.Trace.EndPos
	res.PlaneNormal = tr.Trace.Plane.Normal
	res.PlaneDist = tr.Trace.Plane.Dist
	switch {
	case tr.EntityIdx >= 0 && tr.EntityIdx < len(candIdx):
		res.EntIdx = candIdx[tr.EntityIdx]
	case tr.WorldClipped:
		res.EntIdx = 0
	default:
		res.EntIdx = -1
	}

	// Open vs water dispatch via PointContents at the endpoint. The
	// upstream uses CONTENTS_EMPTY for "open" and EVERYTHING ELSE
	// (water/lava/slime/sky) for "in water". Errors collapse to
	// "open" (= default trace dispatch).
	contents, err := world.PointContents(h.Server.WorldModel, res.EndPos)
	if err == nil {
		if contents == contentsEmpty {
			res.InOpen = true
			res.InWater = false
		} else {
			res.InOpen = false
			res.InWater = true
		}
	}

	return res, nil
}

// contentsEmpty mirrors bspfile.ContentsEmpty (-1). Repeated here as
// a const so the package doesn't grow a direct bspfile dep for one
// constant.
const contentsEmpty = -1

// FindRadius returns every non-free, non-world edict whose bbox
// centre falls within rad units of org, in arena-order (smallest
// edict index first). tyrquake: PF_findradius (pr_cmds.c). The QC
// builtin walks the returned slice in reverse to chain edicts via
// the .chain field (head -> last -> ... -> first -> world).
//
// The returned slice indexes h.Server.Edicts directly. Slot 0 is
// always excluded (the world entity is never a target). On a host
// without an active server the returned slice is empty.
func (h *Host) FindRadius(org [3]float32, rad float32) []int {
	out := []int{}
	if h == nil || h.Server == nil {
		return out
	}
	p := h.findProgs()
	if p == nil {
		return out
	}
	r2 := rad * rad
	n := h.Server.NumEdicts
	if n > len(h.Server.Edicts) {
		n = len(h.Server.Edicts)
	}
	for i := 1; i < n; i++ {
		ed := h.Server.Edicts[i]
		if ed == nil || ed.Free {
			continue
		}
		ev, _ := progs.NewEntVars(p, ed)
		// Upstream PF_findradius skips SOLID_NOT (= solid 0) entities.
		// The check is "if (ent->v.solid == SOLID_NOT) continue" --
		// a missing `solid` field falls through to "treat as 0 =
		// SOLID_NOT" so we skip when the field can't be read too.
		solidF, err := ev.ReadFloat("solid")
		if err != nil {
			continue
		}
		if server.Solid(int32(solidF)) == server.SolidNot {
			continue
		}
		origin, err := ev.ReadVec3("origin")
		if err != nil {
			continue
		}
		emins, _ := ev.ReadVec3("mins")
		emaxs, _ := ev.ReadVec3("maxs")
		// Center-of-bbox per upstream: eorg[k] = org[k] -
		// (ent.origin[k] + 0.5*(ent.mins[k] + ent.maxs[k])).
		var dx, dy, dz float32
		dx = org[0] - (origin[0] + 0.5*(emins[0]+emaxs[0]))
		dy = org[1] - (origin[1] + 0.5*(emins[1]+emaxs[1]))
		dz = org[2] - (origin[2] + 0.5*(emins[2]+emaxs[2]))
		if dx*dx+dy*dy+dz*dz > r2 {
			continue
		}
		out = append(out, i)
	}
	return out
}

// WriteTraceGlobals stores res into the QC-visible trace_* globals
// the QC code reads after a traceline call. Looks up each global by
// name via Progs.FindGlobal so progs.dat layouts that omit a global
// (test stubs) silently skip writing it. Returns the first VM write
// error (typically ErrGlobalOffset); a nil progs or a clean lookup
// surface as a clean nil.
//
// tyrquake: PF_traceline's pr_global_struct->trace_* assignments.
//
// entPointer is the QC entity-pointer for res.EntIdx (= the byte
// offset the arena's MakePointer produces). The caller resolves it
// outside this function so WriteTraceGlobals stays free of arena
// plumbing (and testable without a live arena).
func WriteTraceGlobals(vm *progs.VM, p *progs.Progs, res TraceResult, entPointer int32) error {
	if vm == nil || p == nil {
		return nil
	}
	type binding struct {
		name string
		fn   func() error
	}
	boolf := func(b bool) float32 {
		if b {
			return 1
		}
		return 0
	}
	bindings := []binding{
		{"trace_allsolid", func() error {
			def := p.FindGlobal("trace_allsolid")
			if def == nil {
				return nil
			}
			return vm.SetGlobalFloat(int(def.Ofs), boolf(res.AllSolid))
		}},
		{"trace_startsolid", func() error {
			def := p.FindGlobal("trace_startsolid")
			if def == nil {
				return nil
			}
			return vm.SetGlobalFloat(int(def.Ofs), boolf(res.StartSolid))
		}},
		{"trace_fraction", func() error {
			def := p.FindGlobal("trace_fraction")
			if def == nil {
				return nil
			}
			return vm.SetGlobalFloat(int(def.Ofs), res.Fraction)
		}},
		{"trace_endpos", func() error {
			def := p.FindGlobal("trace_endpos")
			if def == nil {
				return nil
			}
			return vm.SetGlobalVector(int(def.Ofs), res.EndPos)
		}},
		{"trace_plane_normal", func() error {
			def := p.FindGlobal("trace_plane_normal")
			if def == nil {
				return nil
			}
			return vm.SetGlobalVector(int(def.Ofs), res.PlaneNormal)
		}},
		{"trace_plane_dist", func() error {
			def := p.FindGlobal("trace_plane_dist")
			if def == nil {
				return nil
			}
			return vm.SetGlobalFloat(int(def.Ofs), res.PlaneDist)
		}},
		{"trace_ent", func() error {
			def := p.FindGlobal("trace_ent")
			if def == nil {
				return nil
			}
			return vm.SetGlobalInt(int(def.Ofs), entPointer)
		}},
		{"trace_inopen", func() error {
			def := p.FindGlobal("trace_inopen")
			if def == nil {
				return nil
			}
			return vm.SetGlobalFloat(int(def.Ofs), boolf(res.InOpen))
		}},
		{"trace_inwater", func() error {
			def := p.FindGlobal("trace_inwater")
			if def == nil {
				return nil
			}
			return vm.SetGlobalFloat(int(def.Ofs), boolf(res.InWater))
		}},
	}
	for _, b := range bindings {
		if err := b.fn(); err != nil {
			return err
		}
	}
	return nil
}

// ChainEdicts links the edicts in slots (in reverse order) via their
// `chain` field, returning the slot of the head of the chain (= the
// first edict in slots, or 0 = world when slots is empty). The chain
// terminates at world (slot 0). tyrquake: PF_findradius's chain build.
//
// Returns the head slot on success; -1 when the chain field cannot
// be resolved (no progs, no chain field). The chain field is an
// ev_entity field that holds a QC entity-pointer; the caller passes
// `pointerFor` to convert a slot index into the arena-relative byte
// offset (= arena.MakePointer(slot, 0)).
//
// A nil pointerFor (test stubs without a live arena) falls back to
// writing the raw slot index as the chain value -- the chain is then
// not a valid QC entity-pointer but the linkage shape is observable
// (every non-head ent's chain field == previous slot, head .chain ==
// 0 = world).
func ChainEdicts(p *progs.Progs, edicts []*progs.Edict, slots []int, pointerFor func(slot int) int32) (int, error) {
	if p == nil {
		return -1, nil
	}
	// Resolve the chain field once; absent => no-op + return world.
	chainDef := p.FindField("chain")
	if chainDef == nil {
		return 0, nil
	}
	if len(slots) == 0 {
		return 0, nil
	}
	ptrOf := pointerFor
	if ptrOf == nil {
		ptrOf = func(s int) int32 { return int32(s) }
	}
	// Walk slots in reverse so the head of the chain ends up
	// pointing at the previous-match (first match in slots[]).
	// tyrquake's loop is also bottom-up.
	prevPtr := int32(0) // world = end-of-chain sentinel
	for _, slot := range slots {
		if slot < 0 || slot >= len(edicts) {
			continue
		}
		ed := edicts[slot]
		if ed == nil {
			continue
		}
		if err := ed.FieldSetInt(int(chainDef.Ofs), prevPtr); err != nil {
			return -1, err
		}
		prevPtr = ptrOf(slot)
	}
	// prevPtr is now the pointer of the LAST written slot = the
	// head of the chain. Return its slot.
	return slots[len(slots)-1], nil
}

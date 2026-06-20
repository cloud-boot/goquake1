// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"

	"github.com/go-quake1/engine/progs"
)

// ThinkCaller is the dispatch callback SV_RunThink invokes when an
// edict's nextthink fires. The funcID is the QC function-index (the
// ev_function value stored on ent.v.think); the future progs-runtime
// glue resolves funcID -> a Programs.Call() invocation. The Go port
// decouples this so RunThink stays free of QC interpreter knowledge.
//
// The caller is responsible for setting the QC self/other globals
// (the C upstream's pr_global_struct->self = EDICT_TO_PROG(ent) /
// pr_global_struct->other = EDICT_TO_PROG(sv.edicts)) before
// dispatching funcID, since RunThink has no handle on the QC global
// pool.
type ThinkCaller func(ent *progs.Edict, funcID int32) error

// Sentinel errors.
var (
	ErrNilEntity     = errors.New("server: RunThink given nil edict")
	ErrNilEntVars    = errors.New("server: RunThink given nil EntVars")
	ErrNoThinkCaller = errors.New("server: RunThink given nil thinkCaller")
)

// RunThink fires an edict's think callback if its nextthink has
// arrived. tyrquake: SV_RunThink in common/sv_phys.c.
//
// Algorithm (from the C upstream):
//
//  1. thinktime = ent.v.nextthink
//  2. if thinktime <= 0 OR thinktime > now + dt: skip (return true, nil)
//  3. clamp thinktime to >= now (the C upstream pins
//     pr_global_struct->time = thinktime so a trigger with a stale
//     local time can't appear "in the past"). The Go port does not
//     own the QC global pool -- the thinkCaller does -- so this
//     clamp would have no observable side effect here; it is
//     dropped, bsptrace-style.
//  4. ent.v.nextthink = 0 (clear so it doesn't re-fire).
//  5. thinkCaller sets the QC self / other globals + invokes the
//     QC function indexed by ent.v.think.
//
// Returns:
//
//	true,  nil  -- think fired, or was skipped because no nextthink
//	               scheduled / scheduled past the current tick.
//	false, err  -- thinkCaller returned an error (the C upstream
//	               cascades the PR_ExecuteProgram error up).
//
// The C version checks ent->free after the think to bail (its
// qboolean return). The Go port surfaces no ent.free in the edict
// arena yet, so RunThink always returns true on the happy path.
//
// Parameters:
//
//	ent          edict whose think might fire.
//	ev           EntVars bound to ent + the active Progs.
//	now          current server time (the C upstream's sv.time).
//	dt           tick interval (the C upstream's host_frametime).
//	thinkCaller  dispatch hook (see ThinkCaller).
func RunThink(ent *progs.Edict, ev *progs.EntVars, now, dt float32, thinkCaller ThinkCaller) (bool, error) {
	if ent == nil {
		return false, ErrNilEntity
	}
	if ev == nil {
		return false, ErrNilEntVars
	}
	if thinkCaller == nil {
		return false, ErrNoThinkCaller
	}

	thinktime, err := ev.ReadFloat("nextthink")
	if err != nil {
		return false, err
	}
	if thinktime <= 0 || thinktime > now+dt {
		return true, nil
	}

	funcID, err := ev.ReadInt32("think")
	if err != nil {
		return false, err
	}

	// Clear nextthink BEFORE dispatching: matches the C upstream,
	// where ent->v.nextthink = 0 is written before PR_ExecuteProgram
	// so a think that re-schedules itself (ent.v.nextthink = now +
	// delay) survives the clear.
	//
	// WriteFloat only fails when the field is absent or the wrong
	// type. The preceding ReadFloat("nextthink") proved the field
	// exists as EvFloat, so the error branch is unreachable here and
	// is dropped, bsptrace-style.
	_ = ev.WriteFloat("nextthink", 0)

	if err := thinkCaller(ent, funcID); err != nil {
		return false, err
	}
	return true, nil
}

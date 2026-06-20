// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"github.com/go-quake1/engine/cvar"
)

// ServerCvar is one named cvar binding the server layer registers
// at boot. tyrquake's NQ/sv_main.c declares each of these via
// cvar_t + Cvar_RegisterVariable; the Go port hoists the (name,
// default, flags) triple into a static table the caller binds
// against any cvar.Registry.
type ServerCvar struct {
	Name    string
	Default string // wire format (matches the C upstream's char* default)
	// Archive flags + per-cvar callbacks deferred -- this table
	// only carries the (name, default) pair, which is enough for
	// the cvar registry to instantiate the cvar.
}

// ServerCvars returns the static table of (name, default) bindings
// the server layer wires up. The order matches NQ/sv_main.c's
// registration order verbatim so a side-by-side audit against the
// C source stays trivial.
//
// Categories (commented inline in the returned slice):
//   - gameplay rules: teamplay, skill, deathmatch, coop, fraglimit,
//     timelimit, pausable
//   - physics: sv_maxvelocity, sv_gravity, sv_friction, edgefriction,
//     sv_stopspeed, sv_maxspeed, sv_accelerate
//   - aim assist: sv_idealpitchscale, sv_aim, sv_nostep
//
// NOTE on "edgefriction" -- the C upstream variable is spelled
// `sv_edgefriction` in code but registered with the console name
// "edgefriction" (NQ/sv_user.c line 38:
// `cvar_t sv_edgefriction = { "edgefriction", "2" };`). The Go
// table mirrors the wire name, NOT the C identifier, because
// console/config parity is the contract callers care about.
//
// tyrquake: NQ/sv_main.c SV_RegisterVariables (lines 142-175).
func ServerCvars() []ServerCvar {
	return []ServerCvar{
		// gameplay rules (NQ/host.c)
		{Name: "teamplay", Default: "0"},
		{Name: "skill", Default: "1"},
		{Name: "deathmatch", Default: "0"},
		{Name: "coop", Default: "0"},
		{Name: "fraglimit", Default: "0"},
		{Name: "timelimit", Default: "0"},
		{Name: "pausable", Default: "1"},

		// physics (common/sv_phys.c + NQ/sv_user.c for edgefriction)
		{Name: "sv_maxvelocity", Default: "2000"},
		{Name: "sv_gravity", Default: "800"},
		{Name: "sv_friction", Default: "4"},
		{Name: "edgefriction", Default: "2"},
		{Name: "sv_stopspeed", Default: "100"},
		{Name: "sv_maxspeed", Default: "320"},
		{Name: "sv_accelerate", Default: "10"},

		// aim assist (NQ/sv_user.c + common/pr_cmds.c + common/sv_phys.c)
		{Name: "sv_idealpitchscale", Default: "0.8"},
		{Name: "sv_aim", Default: "0.93"},
		{Name: "sv_nostep", Default: "0"},
	}
}

// RegisterServerCvars binds each entry from [ServerCvars] into the
// supplied cvar.Registry. The order matches the static table; the
// caller can re-register or override entries afterward.
//
// Returns the first error from registry registration; subsequent
// entries are still attempted (best-effort registration). The C
// upstream Sys_Error's on a duplicate name; the Go port surfaces
// the error so the test harness can verify multiple registrations
// against the same registry detect collisions
// (cvar.ErrAlreadyRegistered).
//
// The cvar.Registry API used here is the one exposed by
// engine/cvar: Register takes a *cvar.Var literal and returns
// error. We allocate a fresh Var per entry so re-registering
// against another registry doesn't share state (Register flips
// `registered`/resets the string under us).
func RegisterServerCvars(registry *cvar.Registry) error {
	var first error
	for _, entry := range ServerCvars() {
		v := &cvar.Var{Name: entry.Name, String: entry.Default}
		if err := registry.Register(v); err != nil && first == nil {
			first = err
		}
	}
	return first
}

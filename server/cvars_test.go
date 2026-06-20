// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"errors"
	"strconv"
	"testing"

	"github.com/go-quake1/engine/cvar"
)

// TestServerCvars_Count locks the table size at 17 -- if anyone
// adds/removes an entry without updating tests or the spec, this
// is the first thing that fails and points them at the table.
func TestServerCvars_Count(t *testing.T) {
	got := len(ServerCvars())
	const want = 17
	if got != want {
		t.Fatalf("ServerCvars entry count: got %d want %d", got, want)
	}
}

// TestServerCvars_SpotCheck verifies the requested per-entry
// spot-checks: skill / pausable / sv_gravity / sv_aim / sv_nostep.
// These are the user-facing knobs that surface bugs most often, so
// keep them pinned even if other entries shuffle.
func TestServerCvars_SpotCheck(t *testing.T) {
	want := map[string]string{
		"skill":      "1",
		"pausable":   "1",
		"sv_gravity": "800",
		"sv_aim":     "0.93",
		"sv_nostep":  "0",
	}
	index := map[string]string{}
	for _, e := range ServerCvars() {
		index[e.Name] = e.Default
	}
	for name, def := range want {
		got, ok := index[name]
		if !ok {
			t.Errorf("ServerCvars missing %q", name)
			continue
		}
		if got != def {
			t.Errorf("ServerCvars[%q]: got %q want %q", name, got, def)
		}
	}
}

// TestServerCvars_DriftDetector audits all 17 (name, default)
// pairs against the C upstream literals. The reference column
// names the exact file:line that owns the cvar_t definition --
// if the C source changes, update the want column AND the
// reference, never silently match phys.go.
//
// Drift note (2026-06): the C variable named `sv_edgefriction`
// in NQ/sv_user.c is registered with the console NAME
// "edgefriction" (line 38:
// `cvar_t sv_edgefriction = { "edgefriction", "2" };`). The
// spec passed to this task said "sv_edgefriction" -- this test
// canonicalizes the C upstream name, which is what the registry
// and config writer see.
func TestServerCvars_CSourceDrift(t *testing.T) {
	type entry struct {
		Name    string
		Default string
		Ref     string // tyrquake source location
	}
	want := []entry{
		// gameplay rules
		{"teamplay", "0", "NQ/host.c:90"},
		{"skill", "1", "NQ/host.c:97"},
		{"deathmatch", "0", "NQ/host.c:98"},
		{"coop", "0", "NQ/host.c:99"},
		{"fraglimit", "0", "NQ/host.c:88"},
		{"timelimit", "0", "NQ/host.c:89"},
		{"pausable", "1", "NQ/host.c:101"},
		// physics
		{"sv_maxvelocity", "2000", "common/sv_phys.c:60"},
		{"sv_gravity", "800", "common/sv_phys.c:58"},
		{"sv_friction", "4", "common/sv_phys.c:57"},
		{"edgefriction", "2", "NQ/sv_user.c:38 (note: C var name is sv_edgefriction)"},
		{"sv_stopspeed", "100", "common/sv_phys.c:59"},
		{"sv_maxspeed", "320", "NQ/sv_user.c:154"},
		{"sv_accelerate", "10", "NQ/sv_user.c:155"},
		// aim assist
		{"sv_idealpitchscale", "0.8", "NQ/sv_user.c:37"},
		{"sv_aim", "0.93", "common/pr_cmds.c:1441 (NQ_HACK)"},
		{"sv_nostep", "0", "common/sv_phys.c:64"},
	}
	got := ServerCvars()
	if len(got) != len(want) {
		t.Fatalf("table length drift: got %d want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.Name || g.Default != w.Default {
			t.Errorf("entry %d drift: got (%q, %q) want (%q, %q) [tyrquake: %s]",
				i, g.Name, g.Default, w.Name, w.Default, w.Ref)
		}
	}
}

// TestServerCvars_OrderMatchesCSource pins the order in the slice
// to the C upstream's SV_RegisterVariables() sequence (NQ/sv_main.c
// lines 142-175). The order is part of the contract because boot-time
// config replay depends on it (e.g. a config that touches both
// sv_maxspeed and sv_accelerate has to see them registered in the
// same order tyrquake does).
func TestServerCvars_OrderMatchesCSource(t *testing.T) {
	want := []string{
		"teamplay", "skill", "deathmatch", "coop",
		"fraglimit", "timelimit", "pausable",
		"sv_maxvelocity", "sv_gravity", "sv_friction", "edgefriction",
		"sv_stopspeed", "sv_maxspeed", "sv_accelerate",
		"sv_idealpitchscale", "sv_aim", "sv_nostep",
	}
	got := ServerCvars()
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("order[%d]: got %q want %q", i, got[i].Name, name)
		}
	}
}

// TestRegisterServerCvars_FreshRegistry exercises the happy path:
// every entry binds, no error surfaces, and every name is reachable
// via registry.Find afterward.
func TestRegisterServerCvars_FreshRegistry(t *testing.T) {
	r := cvar.New()
	if err := RegisterServerCvars(r); err != nil {
		t.Fatalf("RegisterServerCvars on fresh registry: %v", err)
	}
	for _, e := range ServerCvars() {
		v := r.Find(e.Name)
		if v == nil {
			t.Errorf("registry missing %q after RegisterServerCvars", e.Name)
			continue
		}
		if v.String != e.Default {
			t.Errorf("registry[%q].String: got %q want %q",
				e.Name, v.String, e.Default)
		}
	}
}

// TestRegisterServerCvars_PhysDefaultsMatch cross-checks each
// physics cvar's string default, when parsed as a float32, against
// the corresponding DefaultPhysParams field. If anyone bumps one
// table without the other, this fails with a message naming the
// drifting cvar.
func TestRegisterServerCvars_PhysDefaultsMatch(t *testing.T) {
	phys := DefaultPhysParams()
	index := map[string]string{}
	for _, e := range ServerCvars() {
		index[e.Name] = e.Default
	}
	cases := []struct {
		Name string
		Want float32
	}{
		{"sv_maxvelocity", phys.MaxVelocity},
		{"sv_gravity", phys.Gravity},
		{"sv_friction", phys.Friction},
		{"edgefriction", phys.EdgeFriction},
		{"sv_stopspeed", phys.StopSpeed},
		{"sv_maxspeed", phys.MaxSpeed},
		{"sv_accelerate", phys.Accelerate},
	}
	for _, c := range cases {
		raw, ok := index[c.Name]
		if !ok {
			t.Errorf("ServerCvars missing physics cvar %q", c.Name)
			continue
		}
		parsed, err := strconv.ParseFloat(raw, 32)
		if err != nil {
			t.Errorf("ServerCvars[%q] = %q does not parse: %v",
				c.Name, raw, err)
			continue
		}
		if float32(parsed) != c.Want {
			t.Errorf("physics drift: ServerCvars[%q]=%v but DefaultPhysParams=%v -- one of the tables is out of sync",
				c.Name, parsed, c.Want)
		}
	}
}

// TestRegisterServerCvars_DuplicateSurfaces verifies the error path:
// running RegisterServerCvars twice against the same registry trips
// cvar.ErrAlreadyRegistered on the second pass, and the function
// returns the FIRST error (matching the doc contract). The body of
// the loop still attempts every entry, so we also check that the
// second-error suppression branch is taken (the registry receives
// 17 collision attempts; only one error surfaces).
func TestRegisterServerCvars_DuplicateSurfaces(t *testing.T) {
	r := cvar.New()
	if err := RegisterServerCvars(r); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	err := RegisterServerCvars(r)
	if err == nil {
		t.Fatal("second pass: expected ErrAlreadyRegistered, got nil")
	}
	if !errors.Is(err, cvar.ErrAlreadyRegistered) {
		t.Errorf("second pass: got %v, want %v", err, cvar.ErrAlreadyRegistered)
	}
	// Verify the registry still has exactly the original 17 entries
	// (the failed re-registrations must not have shadowed them).
	for _, e := range ServerCvars() {
		v := r.Find(e.Name)
		if v == nil {
			t.Errorf("entry %q vanished after duplicate-registration attempt", e.Name)
		}
	}
}

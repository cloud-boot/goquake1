// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qargs

import "testing"

func TestRegistry_Empty(t *testing.T) {
	r := New()
	if r.Argc() != 0 {
		t.Errorf("Argc on empty: %d", r.Argc())
	}
	if r.Argv(0) != "" {
		t.Errorf("Argv(0) on empty: %q", r.Argv(0))
	}
	if r.CheckParm("-x") != 0 {
		t.Errorf("CheckParm on empty: %d", r.CheckParm("-x"))
	}
}

func TestRegistry_InitAndAccess(t *testing.T) {
	r := New()
	r.Init([]string{"goquake1", "-game", "id1", "-basedir", "/data"})
	if r.Argc() != 5 {
		t.Errorf("Argc: got %d want 5", r.Argc())
	}
	if r.Argv(0) != "goquake1" {
		t.Errorf("Argv(0): %q", r.Argv(0))
	}
	if r.Argv(2) != "id1" {
		t.Errorf("Argv(2): %q", r.Argv(2))
	}
	if r.Argv(-1) != "" || r.Argv(100) != "" {
		t.Error("Argv out of range should return empty")
	}
}

func TestRegistry_CheckParm(t *testing.T) {
	r := New()
	r.Init([]string{"exe", "-game", "id1", "-nosound"})
	if r.CheckParm("-game") != 1 {
		t.Errorf("CheckParm(-game): %d", r.CheckParm("-game"))
	}
	if r.CheckParm("-nosound") != 3 {
		t.Errorf("CheckParm(-nosound): %d", r.CheckParm("-nosound"))
	}
	if r.CheckParm("-missing") != 0 {
		t.Errorf("CheckParm(-missing): %d", r.CheckParm("-missing"))
	}
	// argv[0] is never matched -- matches the upstream "for i = 1" loop.
	if r.CheckParm("exe") != 0 {
		t.Errorf("CheckParm should not match argv[0]: %d", r.CheckParm("exe"))
	}
}

func TestRegistry_SafeMode(t *testing.T) {
	r := New()
	r.Init([]string{"exe", "-safe"})
	// Every safe-mode replacement should now be present.
	for _, s := range safeModeReplacements {
		if r.CheckParm(s) == 0 {
			t.Errorf("safe-mode replacement %q missing", s)
		}
	}
	// Original argv preserved.
	if r.Argv(0) != "exe" || r.Argv(1) != "-safe" {
		t.Errorf("safe-mode mutated original argv: %q %q", r.Argv(0), r.Argv(1))
	}
}

func TestRegistry_AddParm(t *testing.T) {
	r := New()
	r.Init([]string{"exe"})
	r.AddParm("-extra")
	if r.CheckParm("-extra") == 0 {
		t.Error("AddParm did not register the new flag")
	}
}

func TestRegistry_MaxNumArgsCap(t *testing.T) {
	// Init with more than MaxNumArgs entries: silently capped.
	argv := make([]string, MaxNumArgs+10)
	for i := range argv {
		argv[i] = "x"
	}
	r := New()
	r.Init(argv)
	if r.Argc() != MaxNumArgs {
		t.Errorf("Argc after over-cap Init: got %d want %d", r.Argc(), MaxNumArgs)
	}
	// AddParm at cap is also a no-op.
	r.AddParm("-overflow")
	if r.Argc() != MaxNumArgs {
		t.Errorf("AddParm at cap should be no-op; Argc=%d", r.Argc())
	}
}

// When the initial argv is right at MaxNumArgs, safe-mode replacements
// must still respect the cap (no panic, no over-allocation).
func TestRegistry_SafeModeAtCap(t *testing.T) {
	argv := make([]string, MaxNumArgs)
	argv[0] = "exe"
	argv[1] = "-safe"
	for i := 2; i < MaxNumArgs; i++ {
		argv[i] = "filler"
	}
	r := New()
	r.Init(argv)
	if r.Argc() != MaxNumArgs {
		t.Errorf("Argc: got %d want %d", r.Argc(), MaxNumArgs)
	}
}

// Init is idempotent on repeated calls (re-uses the underlying slice).
func TestRegistry_InitReusable(t *testing.T) {
	r := New()
	r.Init([]string{"exe", "-game", "id1"})
	r.Init([]string{"exe", "-game", "rogue"})
	if r.Argv(2) != "rogue" {
		t.Errorf("Init re-seed failed: %q", r.Argv(2))
	}
}

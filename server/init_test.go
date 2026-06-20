// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-quake1/engine/cmd"
	"github.com/go-quake1/engine/cvar"
	"github.com/go-quake1/engine/protocol"
)

// resetDefaultProtocol restores the package-level DefaultProtocol
// after a test mutates it. Every test that touches DefaultProtocol
// runs this via t.Cleanup so the global state can't leak across
// tests (the file-level var is the one mutable bit of shared state
// in this package's init layer).
func resetDefaultProtocol(t *testing.T) {
	t.Helper()
	saved := DefaultProtocol
	t.Cleanup(func() { DefaultProtocol = saved })
}

// captureSink builds a printf-shaped closure that appends each
// formatted line to the returned slice. Tests inspect the slice
// to verify SvProtocolCmd's user-visible output.
func captureSink() (sink func(string, ...any), out *[]string) {
	lines := []string{}
	return func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	}, &lines
}

// TestDefaultProtocol_DriftDetector pins the boot-time default to
// protocol.VersionNQ. The C upstream uses FITZ here; the port
// chose NQ to match the rest of the still-NQ-shaped server layer.
// If anyone bumps DefaultProtocol without updating the comment in
// init.go AND this test, the drift is caught here.
func TestDefaultProtocol_DriftDetector(t *testing.T) {
	if DefaultProtocol != protocol.VersionNQ {
		t.Errorf("DefaultProtocol: got %d want %d (protocol.VersionNQ)",
			DefaultProtocol, protocol.VersionNQ)
	}
}

// TestAddCommands_FreshRegistry verifies the happy path: against a
// brand-new cmd.Registry every command in AddCommands registers
// and no error surfaces.
func TestAddCommands_FreshRegistry(t *testing.T) {
	r := cmd.New()
	if err := AddCommands(r); err != nil {
		t.Fatalf("AddCommands on fresh registry: %v", err)
	}
	if !r.Exists("svprotocol") {
		t.Errorf("svprotocol command not registered")
	}
}

// TestAddCommands_DuplicateSurfaces verifies the error path: a
// second AddCommands against the same registry trips the
// duplicate-name guard. The cmd.Registry.Add silently no-ops on
// duplicates (see engine/cmd.Registry.Add), so AddCommands gates
// each registration on Exists and synthesises the error.
func TestAddCommands_DuplicateSurfaces(t *testing.T) {
	r := cmd.New()
	if err := AddCommands(r); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	err := AddCommands(r)
	if err == nil {
		t.Fatal("second pass: expected duplicate-command error, got nil")
	}
	if !strings.Contains(err.Error(), "svprotocol") {
		t.Errorf("error should mention svprotocol, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error should explain duplicate, got %q", err.Error())
	}
}

// TestAddCommands_BoundHandlerInvokes wires the handler via the
// registry's tokenise+dispatch path and confirms it runs without
// panicking AND that one-arg form mutates DefaultProtocol. The
// bound closure discards printf output, so we can only assert on
// the side effect; the printf-capture path is covered by the
// SvProtocolCmd tests below.
func TestAddCommands_BoundHandlerInvokes(t *testing.T) {
	resetDefaultProtocol(t)
	r := cmd.New()
	if err := AddCommands(r); err != nil {
		t.Fatalf("AddCommands: %v", err)
	}
	// no args: prints current value (output discarded by the
	// bound closure).
	if err := r.Execute("svprotocol"); err != nil {
		t.Errorf("execute no-arg: %v", err)
	}
	// one int arg: should update DefaultProtocol.
	if err := r.Execute("svprotocol 666"); err != nil {
		t.Errorf("execute one-arg: %v", err)
	}
	if DefaultProtocol != 666 {
		t.Errorf("DefaultProtocol after set: got %d want 666", DefaultProtocol)
	}
}

// TestInit_HappyPath wires both layers via Init and confirms both
// the cvar registry and the cmd registry get populated.
func TestInit_HappyPath(t *testing.T) {
	cv := cvar.New()
	cm := cmd.New()
	if err := Init(cv, cm); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// every server cvar should be reachable.
	for _, e := range ServerCvars() {
		if cv.Find(e.Name) == nil {
			t.Errorf("cvar %q missing after Init", e.Name)
		}
	}
	// svprotocol command should be registered.
	if !cm.Exists("svprotocol") {
		t.Errorf("svprotocol missing after Init")
	}
}

// TestInit_CvarErrorPropagates pre-populates the cvar registry
// with a name that collides with the first ServerCvars entry, so
// Init's first delegate call (RegisterServerCvars) surfaces an
// ErrAlreadyRegistered. Init must return that error WITHOUT
// touching the cmd registry.
func TestInit_CvarErrorPropagates(t *testing.T) {
	cv := cvar.New()
	// "teamplay" is the first entry; pre-register it to force the
	// collision on the first attempt.
	if err := cv.Register(&cvar.Var{Name: "teamplay", String: "0"}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	cm := cmd.New()
	err := Init(cv, cm)
	if err == nil {
		t.Fatal("Init: expected duplicate-cvar error, got nil")
	}
	// cmd registry should NOT have been touched because Init
	// short-circuits on the cvar error.
	if cm.Exists("svprotocol") {
		t.Errorf("svprotocol should not be registered when Init aborts on cvar error")
	}
}

// TestInit_CmdErrorPropagates pre-registers svprotocol in the cmd
// registry, so AddCommands surfaces its duplicate error. The cvar
// registration succeeds first; Init returns the cmd error.
func TestInit_CmdErrorPropagates(t *testing.T) {
	cv := cvar.New()
	cm := cmd.New()
	cm.Add("svprotocol", func([]string) {})
	err := Init(cv, cm)
	if err == nil {
		t.Fatal("Init: expected duplicate-command error, got nil")
	}
	if !strings.Contains(err.Error(), "svprotocol") {
		t.Errorf("error should mention svprotocol, got %q", err.Error())
	}
}

// TestSvProtocolCmd_NoArgs invokes the handler with just the
// command name; it should print the current DefaultProtocol value.
func TestSvProtocolCmd_NoArgs(t *testing.T) {
	resetDefaultProtocol(t)
	sink, out := captureSink()
	SvProtocolCmd(sink, []string{"svprotocol"})
	if len(*out) != 1 {
		t.Fatalf("expected 1 line, got %d (%v)", len(*out), *out)
	}
	want := fmt.Sprintf("sv_protocol is %d\n", protocol.VersionNQ)
	if (*out)[0] != want {
		t.Errorf("got %q want %q", (*out)[0], want)
	}
}

// TestSvProtocolCmd_ValidIntArg supplies one parseable int; the
// handler must update DefaultProtocol AND report the new value
// via printf.
func TestSvProtocolCmd_ValidIntArg(t *testing.T) {
	resetDefaultProtocol(t)
	sink, out := captureSink()
	SvProtocolCmd(sink, []string{"svprotocol", "666"})
	if DefaultProtocol != 666 {
		t.Errorf("DefaultProtocol after set: got %d want 666", DefaultProtocol)
	}
	if len(*out) != 1 {
		t.Fatalf("expected 1 line, got %d (%v)", len(*out), *out)
	}
	want := "sv_protocol set to 666\n"
	if (*out)[0] != want {
		t.Errorf("got %q want %q", (*out)[0], want)
	}
}

// TestSvProtocolCmd_InvalidArg supplies a non-int second arg; the
// handler must print the usage hint and LEAVE DefaultProtocol
// unchanged.
func TestSvProtocolCmd_InvalidArg(t *testing.T) {
	resetDefaultProtocol(t)
	before := DefaultProtocol
	sink, out := captureSink()
	SvProtocolCmd(sink, []string{"svprotocol", "notanumber"})
	if DefaultProtocol != before {
		t.Errorf("DefaultProtocol changed after bad arg: got %d want %d",
			DefaultProtocol, before)
	}
	if len(*out) != 1 {
		t.Fatalf("expected 1 line, got %d (%v)", len(*out), *out)
	}
	if !strings.Contains((*out)[0], "Usage:") {
		t.Errorf("expected usage hint, got %q", (*out)[0])
	}
}

// TestSvProtocolCmd_TooManyArgs supplies more than one extra arg;
// the handler must print the usage hint and leave state alone.
func TestSvProtocolCmd_TooManyArgs(t *testing.T) {
	resetDefaultProtocol(t)
	before := DefaultProtocol
	sink, out := captureSink()
	SvProtocolCmd(sink, []string{"svprotocol", "1", "2", "3"})
	if DefaultProtocol != before {
		t.Errorf("DefaultProtocol changed after too-many: got %d want %d",
			DefaultProtocol, before)
	}
	if len(*out) != 1 {
		t.Fatalf("expected 1 line, got %d (%v)", len(*out), *out)
	}
	if !strings.Contains((*out)[0], "Usage:") {
		t.Errorf("expected usage hint, got %q", (*out)[0])
	}
}

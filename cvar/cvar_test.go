// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cvar

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// --- Find / VariableValue / VariableString -----------------------------------

func TestFind_Empty(t *testing.T) {
	r := New()
	if got := r.Find("missing"); got != nil {
		t.Errorf("Find on empty registry: got %v want nil", got)
	}
}

func TestVariableValue_Missing(t *testing.T) {
	r := New()
	if got := r.VariableValue("missing"); got != 0 {
		t.Errorf("VariableValue(missing): got %v want 0", got)
	}
}

func TestVariableValue_Present(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "foo", String: "3.5"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := r.VariableValue("foo"); got != 3.5 {
		t.Errorf("VariableValue(foo): got %v want 3.5", got)
	}
}

func TestVariableString_Missing(t *testing.T) {
	r := New()
	if got := r.VariableString("missing"); got != "" {
		t.Errorf("VariableString(missing): got %q want empty", got)
	}
}

func TestVariableString_Present(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "foo", String: "bar"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := r.VariableString("foo"); got != "bar" {
		t.Errorf("VariableString(foo): got %q want %q", got, "bar")
	}
}

// --- Register ----------------------------------------------------------------

func TestRegister_Nil(t *testing.T) {
	r := New()
	if err := r.Register(nil); err == nil {
		t.Errorf("Register(nil): want error, got nil")
	}
}

func TestRegister_AppliesInitialString(t *testing.T) {
	r := New()
	v := &Var{Name: "foo", String: "42"}
	if err := r.Register(v); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if v.String != "42" || v.Value != 42 {
		t.Errorf("after Register: got (%q, %v) want (\"42\", 42)", v.String, v.Value)
	}
	if !v.registered {
		t.Errorf("Var.registered should be true after Register")
	}
}

func TestRegister_DuplicateRejected(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "foo", String: "1"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(&Var{Name: "foo", String: "2"})
	if !errors.Is(err, ErrAlreadyRegistered) {
		t.Errorf("duplicate Register: got %v want ErrAlreadyRegistered", err)
	}
}

func TestRegister_CommandExistsHook(t *testing.T) {
	r := New()
	r.CommandExists = func(name string) bool { return name == "echo" }
	err := r.Register(&Var{Name: "echo", String: "1"})
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Errorf("Register over existing command: got %v want 'is a command' error", err)
	}
	// Non-colliding name still succeeds.
	if err := r.Register(&Var{Name: "ok", String: "1"}); err != nil {
		t.Errorf("Register non-colliding: %v", err)
	}
}

// Register must bypass the Developer-only guard for the initial Set,
// even when r.Developer is false. tyrquake forces developer.value = 1
// for the duration of the initial Cvar_Set.
func TestRegister_BypassesDeveloperGuard(t *testing.T) {
	r := New()
	r.Developer = false
	v := &Var{Name: "secret", String: "1", Flags: Developer}
	if err := r.Register(v); err != nil {
		t.Errorf("Register developer cvar: %v", err)
	}
	if v.Value != 1 {
		t.Errorf("developer cvar initial value: got %v want 1", v.Value)
	}
	// After register, Developer-flag protection is back on.
	if r.Developer {
		t.Errorf("r.Developer leaked true after Register")
	}
	if err := r.Set("secret", "2"); !errors.Is(err, ErrDeveloperOnly) {
		t.Errorf("Set developer cvar after register: got %v want ErrDeveloperOnly", err)
	}
}

// Register-after-SetPending: a value stashed before Register supersedes
// the struct's default String.
func TestRegister_AppliesPending(t *testing.T) {
	r := New()
	r.SetPending("foo", "99")
	v := &Var{Name: "foo", String: "1"}
	if err := r.Register(v); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if v.String != "99" || v.Value != 99 {
		t.Errorf("after pending Register: got (%q,%v) want (\"99\",99)", v.String, v.Value)
	}
	// pending bucket should be drained.
	if _, ok := r.pending["foo"]; ok {
		t.Errorf("pending entry not consumed")
	}
}

// SetPending on an already-registered name routes through Set immediately.
func TestSetPending_AlreadyRegistered(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "foo", String: "1"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.SetPending("foo", "7")
	if got := r.VariableString("foo"); got != "7" {
		t.Errorf("SetPending on registered: got %q want \"7\"", got)
	}
	if _, ok := r.pending["foo"]; ok {
		t.Errorf("pending bucket should stay empty for registered names")
	}
}

// --- Set ---------------------------------------------------------------------

func TestSet_NotFound(t *testing.T) {
	r := New()
	err := r.Set("missing", "1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Set(missing): got %v want ErrNotFound", err)
	}
}

func TestSet_Obsolete(t *testing.T) {
	r := New()
	v := &Var{Name: "old", String: "1"}
	if err := r.Register(v); err != nil {
		t.Fatalf("Register: %v", err)
	}
	v.Flags = Obsolete
	err := r.Set("old", "2")
	if !errors.Is(err, ErrObsolete) {
		t.Errorf("Set obsolete: got %v want ErrObsolete", err)
	}
}

func TestSet_DeveloperGuard(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "debug", String: "0", Flags: Developer}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.Developer = false
	if err := r.Set("debug", "1"); !errors.Is(err, ErrDeveloperOnly) {
		t.Errorf("Set developer cvar: got %v want ErrDeveloperOnly", err)
	}
	r.Developer = true
	if err := r.Set("debug", "1"); err != nil {
		t.Errorf("Set developer cvar in dev mode: %v", err)
	}
	if v := r.Find("debug"); v.String != "1" || v.Value != 1 {
		t.Errorf("after dev-mode Set: got (%q,%v) want (\"1\",1)", v.String, v.Value)
	}
}

func TestSet_CallbackFiresOnlyOnChange(t *testing.T) {
	r := New()
	calls := 0
	v := &Var{Name: "foo", String: "1", Callback: func(_ *Var) { calls++ }}
	if err := r.Register(v); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Register itself triggers one Set against the empty initial -> 1 call.
	if calls != 1 {
		t.Errorf("after Register: callback called %d times, want 1", calls)
	}
	// Same value, no callback.
	if err := r.Set("foo", "1"); err != nil {
		t.Fatalf("Set same: %v", err)
	}
	if calls != 1 {
		t.Errorf("after redundant Set: callback called %d times, want still 1", calls)
	}
	// Different value, one more call.
	if err := r.Set("foo", "2"); err != nil {
		t.Fatalf("Set diff: %v", err)
	}
	if calls != 2 {
		t.Errorf("after changing Set: callback called %d times, want 2", calls)
	}
}

// Developer guard must NOT fire on a redundant Set (same value), so a
// later refresh of an unchanged developer-flagged cvar doesn't reject.
func TestSet_DeveloperGuardSkipsUnchanged(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "debug", String: "0", Flags: Developer}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.Developer = false
	if err := r.Set("debug", "0"); err != nil {
		t.Errorf("unchanged Set on developer cvar: %v (want nil)", err)
	}
}

// Callback nil is fine (no panic).
func TestSet_NilCallback(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "foo", String: "1"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Set("foo", "2"); err != nil {
		t.Errorf("Set with nil callback: %v", err)
	}
}

// --- SetValue ----------------------------------------------------------------

func TestSetValue(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "foo", String: "0"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.SetValue("foo", 1.5); err != nil {
		t.Errorf("SetValue: %v", err)
	}
	v := r.Find("foo")
	if v.Value != 1.5 {
		t.Errorf("after SetValue: value %v want 1.5", v.Value)
	}
	// String form should be the %f-style "1.500000".
	if v.String != "1.500000" {
		t.Errorf("SetValue String format: got %q want \"1.500000\"", v.String)
	}
}

func TestSetValue_PropagatesError(t *testing.T) {
	r := New()
	if err := r.SetValue("missing", 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetValue(missing): got %v want ErrNotFound", err)
	}
}

// --- Command -----------------------------------------------------------------

func TestCommand_EmptyArgs(t *testing.T) {
	r := New()
	if r.Command(nil) {
		t.Errorf("Command(nil): got true want false")
	}
}

func TestCommand_UnknownName(t *testing.T) {
	r := New()
	if r.Command([]string{"missing"}) {
		t.Errorf("Command(missing): got true want false")
	}
}

func TestCommand_InspectsWithOneArg(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "foo", String: "bar"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	var out strings.Builder
	r.Printf = func(format string, args ...any) {
		out.WriteString(format)
		_ = args
	}
	if !r.Command([]string{"foo"}) {
		t.Errorf("Command(foo): got false want true")
	}
	if !strings.Contains(out.String(), "is") {
		t.Errorf("Command print: got %q, want non-empty containing 'is'", out.String())
	}
}

func TestCommand_InspectObsolete(t *testing.T) {
	r := New()
	v := &Var{Name: "old", String: "1"}
	if err := r.Register(v); err != nil {
		t.Fatalf("Register: %v", err)
	}
	v.Flags = Obsolete
	var out strings.Builder
	r.Printf = func(format string, args ...any) {
		out.WriteString(format)
		_ = args
	}
	if !r.Command([]string{"old"}) {
		t.Errorf("Command(old): got false want true")
	}
	if !strings.Contains(out.String(), "obsolete") {
		t.Errorf("obsolete inspect: got %q", out.String())
	}
}

// One-arg inspect with no Printf hook installed is a no-op, not a panic.
func TestCommand_InspectNoPrintf(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "foo", String: "1"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !r.Command([]string{"foo"}) {
		t.Errorf("Command(foo) without Printf hook: got false want true")
	}
}

func TestCommand_SetsWithTwoArgs(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "foo", String: "0"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !r.Command([]string{"foo", "9"}) {
		t.Errorf("Command(foo 9): got false want true")
	}
	if got := r.VariableString("foo"); got != "9" {
		t.Errorf("after Command set: got %q want \"9\"", got)
	}
}

// Command swallows the Set error (returns true regardless) because the
// console handler doesn't surface errors. The Var should remain
// unchanged when the Set was rejected (obsolete here).
func TestCommand_SetErrorSwallowed(t *testing.T) {
	r := New()
	v := &Var{Name: "old", String: "1"}
	if err := r.Register(v); err != nil {
		t.Fatalf("Register: %v", err)
	}
	v.Flags = Obsolete
	if !r.Command([]string{"old", "2"}) {
		t.Errorf("Command(old 2): got false want true (Command always claims the match)")
	}
	if got := r.VariableString("old"); got != "1" {
		t.Errorf("obsolete cvar after Command set: got %q want \"1\"", got)
	}
}

// --- WriteVariables ----------------------------------------------------------

func TestWriteVariables_FlagFilter(t *testing.T) {
	r := New()
	for _, v := range []*Var{
		{Name: "saved", String: "1", Flags: Config},
		{Name: "transient", String: "2"},
		{Name: "video", String: "3", Flags: Video},
		{Name: "both", String: "4", Flags: Config | Video},
	} {
		if err := r.Register(v); err != nil {
			t.Fatalf("Register %s: %v", v.Name, err)
		}
	}
	var buf bytes.Buffer
	if err := r.WriteVariables(&buf, Config); err != nil {
		t.Fatalf("WriteVariables: %v", err)
	}
	want := "both \"4\"\nsaved \"1\"\n"
	if buf.String() != want {
		t.Errorf("WriteVariables Config: got %q want %q", buf.String(), want)
	}

	buf.Reset()
	if err := r.WriteVariables(&buf, Video); err != nil {
		t.Fatalf("WriteVariables Video: %v", err)
	}
	wantV := "both \"4\"\nvideo \"3\"\n"
	if buf.String() != wantV {
		t.Errorf("WriteVariables Video: got %q want %q", buf.String(), wantV)
	}
}

// errWriter fails on the first Write so we can hit WriteVariables's
// error branch.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, io.ErrShortWrite }

func TestWriteVariables_WriterError(t *testing.T) {
	r := New()
	if err := r.Register(&Var{Name: "saved", String: "1", Flags: Config}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.WriteVariables(errWriter{}, Config); err == nil {
		t.Errorf("WriteVariables with failing writer: want error, got nil")
	}
}

// Empty registry / no matching flags produces empty output (and no error).
func TestWriteVariables_Empty(t *testing.T) {
	r := New()
	var buf bytes.Buffer
	if err := r.WriteVariables(&buf, Config); err != nil {
		t.Fatalf("WriteVariables empty: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("empty registry write: got %q want empty", buf.String())
	}
}

// --- parseFloat (Q_atof parity) ----------------------------------------------

func TestParseFloat(t *testing.T) {
	cases := []struct {
		in   string
		want float32
	}{
		{"", 0},
		{"   ", 0},
		{"0", 0},
		{"1", 1},
		{"-1", -1},
		{"3.5", 3.5},
		{".5", 0.5},
		{"1e2", 100},
		{"2E-1", 0.2},
		{"+5", 5},
		// C atof stops at the first non-numeric character; we mimic that.
		{"1.5xyz", 1.5},
		{"7\x00", 7},
		// Pure garbage -> 0 (matches tyrquake's `Q_atof` behaviour).
		{"xyz", 0},
		{"+", 0},
		// A second sign mid-string terminates the parse.
		{"1+2", 1},
	}
	for _, c := range cases {
		if got := parseFloat(c.in); got != c.want {
			t.Errorf("parseFloat(%q): got %v want %v", c.in, got, c.want)
		}
	}
}

// Explicit coverage for the strconv.ParseFloat error branch: a token
// that passes our character filter but is not a valid float (e.g. a
// lone "e") should yield 0 rather than panic.
func TestParseFloat_StrconvError(t *testing.T) {
	if got := parseFloat("e"); got != 0 {
		t.Errorf("parseFloat(e): got %v want 0", got)
	}
	if got := parseFloat(".e"); got != 0 {
		t.Errorf("parseFloat(.e): got %v want 0", got)
	}
}

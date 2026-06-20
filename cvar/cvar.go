// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cvar

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Var mirrors tyrquake's cvar_t. Fields are exported so engine code
// can declare cvars as struct literals (the C idiom
// `cvar_t foo = { "foo", "1", CVAR_CONFIG };`).
//
// tyrquake: cvar_t.
type Var struct {
	Name     string
	String   string
	Flags    uint32
	Server   bool // NQ_HACK: notify players when changed
	Info     bool // QW_HACK: added to serverinfo or userinfo when changed
	Value    float32
	Callback Callback

	// registered is set by Registry.Register and consulted by Set so a
	// Var struct can be reused (e.g. unit tests) without state bleed.
	// Not part of the upstream struct.
	registered bool
}

// Callback is the change-notification hook tyrquake calls "cvar_callback".
// Registry invokes it after Set has installed a new value and only when
// the value actually changed -- matching the C `if (changed && var->callback)`
// guard.
//
// tyrquake: cvar_callback typedef.
type Callback func(*Var)

// Flag bits. tyrquake: CVAR_CONFIG, CVAR_VIDEO, CVAR_DEVELOPER, CVAR_OBSOLETE.
const (
	Config    uint32 = 1 << 0
	Video     uint32 = 1 << 1
	Developer uint32 = 1 << 8
	Obsolete  uint32 = 1 << 9
)

// Errors surfaced by the registry. The C version logs via Con_Printf and
// returns silently; Go callers usually want a structured signal, so the
// hot paths return typed errors and the package also exposes a no-op
// logger field on Registry for parity with the C console output.
var (
	// ErrNotFound is returned by Set / SetValue when the named cvar has
	// not been registered. tyrquake: the "Cvar_Set: variable %s not found"
	// branch.
	ErrNotFound = errors.New("cvar: variable not found")

	// ErrObsolete is returned by Set when the cvar carries the Obsolete
	// flag. tyrquake: the "%s is obsolete." branch.
	ErrObsolete = errors.New("cvar: variable is obsolete")

	// ErrDeveloperOnly is returned by Set when the cvar carries the
	// Developer flag and developer mode is off. tyrquake: the
	// "%s is settable only in developer mode." branch.
	ErrDeveloperOnly = errors.New("cvar: variable is developer-only")

	// ErrAlreadyRegistered is returned by Register when a cvar with the
	// same name is already in the registry. tyrquake: the
	// "Can't register variable %s, already defined" branch.
	ErrAlreadyRegistered = errors.New("cvar: variable already registered")
)

// Registry replaces tyrquake's global cvar_tree. Each instance is
// independent, so tests can spin up a clean registry per case.
type Registry struct {
	vars    map[string]*Var
	pending map[string]string

	// Developer mirrors `developer.value != 0` in tyrquake. When true,
	// Set is allowed to mutate cvars that carry the Developer flag.
	// RegisterVariable temporarily forces it on for the initial Set.
	Developer bool

	// CommandExists, when non-nil, is consulted by Register to reject
	// names that already exist as console commands. tyrquake:
	// Cmd_Exists. Left as a hook so this package doesn't depend on
	// engine/cmd.
	CommandExists func(name string) bool

	// Printf, when non-nil, receives the console-output lines that
	// tyrquake feeds to Con_Printf from the cvar subsystem (currently
	// just the inspection line produced by Command's one-arg form;
	// errors propagate via returned `error` values instead of the
	// C-style printf-and-discard pattern). Field rather than method so
	// callers can swap it at runtime, as the C engine swaps Con_Printf
	// during redirects. tyrquake: Con_Printf indirection.
	Printf func(format string, args ...any)
}

// New returns an empty Registry ready for Register calls.
func New() *Registry {
	return &Registry{
		vars:    make(map[string]*Var),
		pending: make(map[string]string),
	}
}

// Find returns the registered cvar with the given name, or nil.
// tyrquake: Cvar_FindVar.
func (r *Registry) Find(name string) *Var {
	return r.vars[name]
}

// VariableValue returns the float value of the named cvar, or 0 if it
// is not registered. tyrquake: Cvar_VariableValue.
func (r *Registry) VariableValue(name string) float32 {
	v := r.Find(name)
	if v == nil {
		return 0
	}
	return parseFloat(v.String)
}

// VariableString returns the string value of the named cvar, or "" if
// it is not registered. tyrquake: Cvar_VariableString.
func (r *Registry) VariableString(name string) string {
	v := r.Find(name)
	if v == nil {
		return ""
	}
	return v.String
}

// SetPending records a value for a not-yet-registered cvar. When the
// matching Register call lands, the pending value supersedes the
// struct's default String. This implements the engine's
// "config file parsed before subsystem init" boot order without the
// SetPending hook RegisterVariable would silently use the default.
//
// Calling SetPending on an already-registered name routes straight to
// Set so callers don't have to special-case the timing.
func (r *Registry) SetPending(name, value string) {
	if v := r.Find(name); v != nil {
		_ = r.Set(name, value)
		return
	}
	r.pending[name] = value
}

// Register inserts a Var into the registry and applies either the
// struct's initial String or, if SetPending stashed one earlier, the
// pending value. tyrquake: Cvar_RegisterVariable.
func (r *Registry) Register(v *Var) error {
	if v == nil {
		return errors.New("cvar: Register(nil)")
	}
	if _, exists := r.vars[v.Name]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyRegistered, v.Name)
	}
	if r.CommandExists != nil && r.CommandExists(v.Name) {
		return fmt.Errorf("cvar: %s is a command", v.Name)
	}

	initial := v.String
	if pending, ok := r.pending[v.Name]; ok {
		initial = pending
		delete(r.pending, v.Name)
	}

	// Reset String to "" so the developer-only guard in Set (which checks
	// `var->string != value`) treats the initial assignment as a change
	// against "no prior value" rather than against the literal default,
	// matching the C path that zeroes `variable->string` before calling
	// Cvar_Set.
	v.String = ""
	v.Value = 0
	v.registered = true
	r.vars[v.Name] = v

	prevDev := r.Developer
	r.Developer = true
	defer func() { r.Developer = prevDev }()
	return r.Set(v.Name, initial)
}

// Set installs value into the named cvar, parses the float form, and
// fires the change callback if the string actually differs from the
// previous value. tyrquake: Cvar_Set.
func (r *Registry) Set(name, value string) error {
	v := r.Find(name)
	if v == nil {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if v.Flags&Obsolete != 0 {
		return fmt.Errorf("%w: %s", ErrObsolete, name)
	}

	changed := v.String != value
	if changed && v.Flags&Developer != 0 && !r.Developer {
		return fmt.Errorf("%w: %s", ErrDeveloperOnly, name)
	}

	v.String = value
	v.Value = parseFloat(value)

	if changed && v.Callback != nil {
		v.Callback(v)
	}
	return nil
}

// SetValue formats value with the same `%f` printf format tyrquake uses
// and forwards to Set. tyrquake: Cvar_SetValue.
func (r *Registry) SetValue(name string, value float32) error {
	// "%f" in C defaults to 6 decimals; reproduce that explicitly so the
	// round-trip String matches tyrquake's archival format byte-for-byte.
	return r.Set(name, strconv.FormatFloat(float64(value), 'f', 6, 32))
}

// Command handles the console form `<cvarname> [value]`. Returns true
// if the first argument matched a registered cvar (in which case the
// caller should stop searching for command handlers), regardless of
// whether the Set succeeded. tyrquake: Cvar_Command.
//
// With one argument: prints the current value via the Registry's
// optional Printf hook (no-op if nil). With two: sets it.
func (r *Registry) Command(args []string) bool {
	if len(args) == 0 {
		return false
	}
	v := r.Find(args[0])
	if v == nil {
		return false
	}
	if len(args) == 1 {
		r.print(v)
		return true
	}
	_ = r.Set(v.Name, args[1])
	return true
}

// Printf is the engine's console-output hook. When non-nil, Command's
// inspection path (the one-arg form) routes through it. Left exported
// so tests can capture output without indirecting through stdout.
// tyrquake: Con_Printf.
func (r *Registry) print(v *Var) {
	if r.Printf == nil {
		return
	}
	if v.Flags&Obsolete != 0 {
		r.Printf("%s is obsolete.\n", v.Name)
		return
	}
	r.Printf("\"%s\" is \"%s\"\n", v.Name, v.String)
}

// WriteVariables writes each registered cvar whose flags overlap the
// `flags` mask to w in `name "value"\n` form, sorted by name for
// deterministic output (the C version walks the stree in insertion
// order, but tests want stability and downstream diff tools assume
// alphabetical archive files). tyrquake: Cvar_WriteVariables.
func (r *Registry) WriteVariables(w io.Writer, flags uint32) error {
	names := make([]string, 0, len(r.vars))
	for n, v := range r.vars {
		if v.Flags&flags != 0 {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		v := r.vars[n]
		if _, err := io.WriteString(w, v.Name+" \""+v.String+"\"\n"); err != nil {
			return err
		}
	}
	return nil
}

// parseFloat mirrors tyrquake's Q_atof: an unparseable string yields 0,
// matching C's atof() / strtof() error contract that the engine relies
// on (a malformed cvar value is silently coerced to numeric zero,
// never reported as a parse error).
func parseFloat(s string) float32 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// strconv.ParseFloat rejects a leading "+", a trailing junk char,
	// and other forms C's atof accepts by stopping at the first
	// non-numeric character. Replicate that "longest valid prefix"
	// behaviour because several cvars are set from netcode buffers
	// that have trailing whitespace or NULs.
	end := 0
	sawExp := false
	for end < len(s) {
		c := s[end]
		if c == '+' || c == '-' {
			// A sign is only valid at the start of the token or
			// immediately after an exponent marker ("1e-3").
			if end != 0 && !(sawExp && (s[end-1] == 'e' || s[end-1] == 'E')) {
				break
			}
		} else if c == 'e' || c == 'E' {
			sawExp = true
		} else if c == '.' || (c >= '0' && c <= '9') {
			// keep going
		} else {
			break
		}
		end++
	}
	if end == 0 {
		return 0
	}
	f, err := strconv.ParseFloat(s[:end], 32)
	if err != nil {
		return 0
	}
	return float32(f)
}

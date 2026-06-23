// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package runloop

import (
	"errors"
	"io"

	"github.com/go-quake1/engine/backend"
	"github.com/go-quake1/engine/demo"
	"github.com/go-quake1/engine/menu"
)

// Demo bundles the per-Runner demo-playback state: the currently-
// playing [demo.Reader], an optional Restart closure for the
// attract-loop EOF rewind, and the [demo.PlayerOpts] [demo.PlayTick]
// is called with. When [Runner.Demo] is non-nil AND the menu is at
// the title screen (StateMain or StateNone), [Runner.RunFrame]
// substitutes the per-tic demo playback for the live server tick:
//
//   - the host.Frame call is SKIPPED (no server simulation while
//     a recorded stream is being replayed -- the demo body IS the
//     server's per-tic broadcast snapshot)
//   - one [demo.DemoTick] is decoded into the client state via
//     [demo.PlayTick]; the tick's recorded view-angles become
//     r.ViewAngles so the camera follows the recording
//   - on [io.EOF] from [demo.Reader.NextFrame] the Restart closure
//     (if non-nil) is invoked to re-open the same demo + the
//     attract loop continues; if Restart is nil OR Restart fails
//     the demo is stopped (Runner.Demo cleared)
//
// Any per-frame user input that bears a KeyDown event halts the
// demo (Runner.Demo cleared) so the next tic returns to the normal
// host.Frame + client.Tick path -- this is the vanilla "any key
// press interrupts the attract loop" behaviour.
type Demo struct {
	// Reader is the active streaming [demo.Reader]. Re-assigned by
	// the EOF-rewind path when Restart returns a fresh Reader.
	Reader *demo.Reader

	// Restart is the optional factory the runloop calls when Reader
	// returns [io.EOF]. A nil Restart makes the demo stop at clean
	// end of stream (the attract loop only loops when the embedder
	// has wired up a way to re-open the pak entry).
	Restart func() (*demo.Reader, error)

	// PlayerOpts is the configuration handed to [demo.PlayTick] each
	// tic. The zero value defaults to [demo.DefaultPlayerOpts] inside
	// playDemoTick so callers can leave it unset.
	PlayerOpts demo.PlayerOpts

	// FrameCount is the running count of successfully-played demo
	// ticks across all loops. Exposed for the QEMU/serial-log trace
	// so headless validation runs can prove the demo is actually
	// advancing per-tic.
	FrameCount int

	// OnFrame is the optional per-tic callback fired after each
	// successful [demo.PlayTick]. Receives the post-increment
	// FrameCount + the just-applied recorded view-angles so the
	// embedder can drive a serial-log trace ("QUAKE: demo playback
	// frame N viewAngles=...") without having to wrap the runloop.
	// nil = no callback (default; production embeds wire the log
	// hook themselves).
	OnFrame func(frame int, viewAngles [3]float32)
}

// demoActive reports whether the runner should swap in the demo
// playback for this tic. True iff Runner.Demo is non-nil AND its
// Reader is set AND the menu is at the attract-loop screens
// (StateMain or StateNone). When a player picks "New Game" the
// menu transitions to StateNone via StateSkill -> the demo halts on
// the NEXT tic; while the player navigates the title menu the
// recorded stream plays behind the overlay.
//
// Returns false when r is nil so the function is safe on a
// half-built test runner.
func (r *Runner) demoActive() bool {
	if r == nil || r.Demo == nil || r.Demo.Reader == nil {
		return false
	}
	if r.Menu == nil {
		// No menu wired -> attract loop runs unconditionally; the
		// embedder is on the bring-up path where the menu hasn't
		// been added yet but the demo should still play.
		return true
	}
	switch r.Menu.State {
	case menu.StateMain, menu.StateNone:
		return true
	}
	return false
}

// interruptDemoOnInput clears Runner.Demo when snap carries any
// KeyDown event. Mirrors the vanilla CL_NextDemo behaviour where
// any keypress drops the player out of the attract loop and back
// into the live menu / world. KeysUp events are NOT counted
// (matches the press-half-only menu / console toggles already in
// the runloop).
//
// A nil Runner.Demo OR an empty snap.KeysDown leaves the field
// untouched. Returns true when the demo was interrupted so the
// caller can adjust per-tic accounting.
func (r *Runner) interruptDemoOnInput(snap backend.InputSnapshot) bool {
	if r.Demo == nil {
		return false
	}
	if len(snap.KeysDown) == 0 {
		return false
	}
	r.Demo = nil
	return true
}

// playDemoTick decodes one tick from Runner.Demo.Reader and applies
// it to the client state via [demo.PlayTick]. The recorded
// view-angles are written into r.ViewAngles so the per-frame
// Pre2DDraw closure renders from the same camera the demo recorded.
//
// EOF handling:
//
//   - NextFrame io.EOF + Restart != nil: re-open the demo, leave
//     the runner in the demo-active path so the loop continues
//     (the freshly-restarted Reader has just had its header parsed;
//     the next tic decodes the first body verbatim)
//   - NextFrame io.EOF + Restart == nil: clear Runner.Demo so the
//     runloop returns to the normal host.Frame + client.Tick path
//     on the next tic
//   - Restart returns an error: clear Runner.Demo + propagate
//     the error verbatim (a failed restart is a genuine fault the
//     caller wants to see)
//
// Any other NextFrame error (mid-tic short read, corrupt length
// prefix, etc.) propagates verbatim AFTER clearing Runner.Demo so
// the next tic doesn't re-trip the same broken stream.
//
// PlayTick errors propagate verbatim WITHOUT clearing the demo:
// the underlying byte stream is fine, so the next tic can decode
// the next message body cleanly + the embedder can decide whether
// to log + continue or escalate. (The simpler-but-wrong alternative
// would silently swallow Apply errors and let the client drift out
// of sync with the recording.)
func (r *Runner) playDemoTick() error {
	tick, err := r.Demo.Reader.NextFrame()
	if errors.Is(err, io.EOF) {
		return r.restartDemoOnEOF()
	}
	if err != nil {
		r.Demo = nil
		return err
	}
	opts := r.Demo.PlayerOpts
	if opts.Protocol == 0 && opts.TickDelta == 0 && !opts.SkipUnknownSvc {
		opts = demo.DefaultPlayerOpts()
	}
	if perr := demo.PlayTick(r.Client, &tick, &r.ViewAngles, opts); perr != nil {
		return perr
	}
	r.Demo.FrameCount++
	if r.Demo.OnFrame != nil {
		r.Demo.OnFrame(r.Demo.FrameCount, r.ViewAngles)
	}
	return nil
}

// restartDemoOnEOF is the io.EOF arm of playDemoTick. Returns nil
// on a successful restart (the next tic resumes playback against
// the freshly-opened Reader); clears Runner.Demo + returns the
// factory's error on failure; clears Runner.Demo + returns nil
// when no Restart was wired (the demo simply stops at end of
// stream).
func (r *Runner) restartDemoOnEOF() error {
	if r.Demo.Restart == nil {
		r.Demo = nil
		return nil
	}
	fresh, err := r.Demo.Restart()
	if err != nil {
		r.Demo = nil
		return err
	}
	r.Demo.Reader = fresh
	return nil
}

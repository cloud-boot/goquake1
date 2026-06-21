// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package runloop

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/go-quake1/engine/backend"
	"github.com/go-quake1/engine/client"
	"github.com/go-quake1/engine/render"
	"github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/sound"
	"github.com/go-quake1/engine/vfs"
)

func makeStandardVFS(t *testing.T) *vfs.SearchPath {
	t.Helper()
	v := vfs.New()
	mfs := fstest.MapFS{
		"gfx/palette.lmp":  &fstest.MapFile{Data: make([]byte, render.PaletteLumpSize)},
		"gfx/colormap.lmp": &fstest.MapFile{Data: make([]byte, render.ColorMapRows*render.ColorMapCols)},
		"gfx/conchars.lmp": &fstest.MapFile{Data: make([]byte, 128*128)},
	}
	v.Add(mfs)
	return v
}

func newSetupOpts(t *testing.T) SetupOpts {
	t.Helper()
	rec := backend.NewRecorder(320, 200)
	cli, _ := server.NewLoopbackConn()
	return SetupOpts{
		VFS:     makeStandardVFS(t),
		Host:    &loopFakeHost{},
		Client:  client.NewState(),
		Conn:    cli,
		Backend: rec,
	}
}

// ----- happy + nil-arg paths ---------------------------------------

func TestNewRunnerFromVFS_Happy(t *testing.T) {
	r, err := NewRunnerFromVFS(newSetupOpts(t))
	if err != nil {
		t.Fatalf("NewRunnerFromVFS: %v", err)
	}
	if r.FrameBuffer == nil || r.Console == nil || r.Screen == nil {
		t.Fatalf("Runner missing render fields: %+v", r)
	}
	if r.Palette == nil || r.Chars == nil {
		t.Fatalf("Runner missing assets: %+v", r)
	}
	if len(r.RGBA) != 320*200*4 {
		t.Fatalf("RGBA size = %d want %d", len(r.RGBA), 320*200*4)
	}
	if r.NotifyLifetime != 3 || r.MaxNotifyRows != 4 {
		t.Fatalf("notify defaults wrong: lifetime=%v rows=%d", r.NotifyLifetime, r.MaxNotifyRows)
	}
	if r.SoundPool != nil {
		t.Fatalf("SoundPool non-nil with SoundChannels=0")
	}
}

func TestNewRunnerFromVFS_WithSound(t *testing.T) {
	opts := newSetupOpts(t)
	opts.SoundChannels = 8
	r, err := NewRunnerFromVFS(opts)
	if err != nil {
		t.Fatalf("NewRunnerFromVFS: %v", err)
	}
	if r.SoundPool == nil {
		t.Fatalf("SoundPool nil after enabling sound")
	}
	if len(r.MixBuffer) != sound.MixBufferStereoFrames {
		t.Fatalf("MixBuffer size = %d want %d", len(r.MixBuffer), sound.MixBufferStereoFrames)
	}
}

func TestNewRunnerFromVFS_BadSoundChannels(t *testing.T) {
	opts := newSetupOpts(t)
	opts.SoundChannels = 9999 // > MaxChannels
	_, err := NewRunnerFromVFS(opts)
	if err == nil {
		t.Fatalf("expected error for bad SoundChannels")
	}
}

func TestNewRunnerFromVFS_NilVFS(t *testing.T) {
	opts := newSetupOpts(t)
	opts.VFS = nil
	_, err := NewRunnerFromVFS(opts)
	if !errors.Is(err, ErrSetupNilVFS) {
		t.Fatalf("err = %v want ErrSetupNilVFS", err)
	}
}

func TestNewRunnerFromVFS_NilHost(t *testing.T) {
	opts := newSetupOpts(t)
	opts.Host = nil
	_, err := NewRunnerFromVFS(opts)
	if !errors.Is(err, ErrSetupNilHost) {
		t.Fatalf("err = %v want ErrSetupNilHost", err)
	}
}

func TestNewRunnerFromVFS_NilClient(t *testing.T) {
	opts := newSetupOpts(t)
	opts.Client = nil
	_, err := NewRunnerFromVFS(opts)
	if !errors.Is(err, ErrSetupNilClient) {
		t.Fatalf("err = %v want ErrSetupNilClient", err)
	}
}

func TestNewRunnerFromVFS_NilConn(t *testing.T) {
	opts := newSetupOpts(t)
	opts.Conn = nil
	_, err := NewRunnerFromVFS(opts)
	if !errors.Is(err, ErrSetupNilConn) {
		t.Fatalf("err = %v want ErrSetupNilConn", err)
	}
}

func TestNewRunnerFromVFS_NilBackend(t *testing.T) {
	opts := newSetupOpts(t)
	opts.Backend = nil
	_, err := NewRunnerFromVFS(opts)
	if !errors.Is(err, ErrSetupNilBackend) {
		t.Fatalf("err = %v want ErrSetupNilBackend", err)
	}
}

func TestNewRunnerFromVFS_AssetsLoadFails(t *testing.T) {
	// VFS without the required lumps -> LoadStandard fails.
	opts := newSetupOpts(t)
	opts.VFS = vfs.New() // empty
	_, err := NewRunnerFromVFS(opts)
	if err == nil {
		t.Fatalf("expected error when assets missing")
	}
}

func TestNewRunnerFromVFS_BadBackendDim(t *testing.T) {
	// Backend reports 0x0 dims -> NewFrameBuffer fails.
	opts := newSetupOpts(t)
	opts.Backend = backend.NewRecorder(0, 0)
	_, err := NewRunnerFromVFS(opts)
	if err == nil {
		t.Fatalf("expected error for 0x0 backend size")
	}
}

// Coverage: NewScreen has its own validation; if the backend's
// dims pass NewFrameBuffer but fail NewScreen, that path runs.
// NewScreen has the same Width<=0 / Height<=0 check as
// NewFrameBuffer, so this isn't testable independently --
// documented.

func TestNewRunnerFromVFS_OverridesHonored(t *testing.T) {
	opts := newSetupOpts(t)
	opts.BackgroundIdx = 0x42
	opts.NotifyLifetime = 7
	opts.MaxNotifyRows = 10
	r, err := NewRunnerFromVFS(opts)
	if err != nil {
		t.Fatalf("NewRunnerFromVFS: %v", err)
	}
	if r.BackgroundIdx != 0x42 || r.NotifyLifetime != 7 || r.MaxNotifyRows != 10 {
		t.Fatalf("overrides not applied: %+v", r)
	}
}

func TestNewRunnerFromVFS_NarrowBackendKeepsMinConsole(t *testing.T) {
	// Backend smaller than MinConsoleWidth*CharWidth: console width
	// stays at MinConsoleWidth (per the if maxCols > conW guard).
	opts := newSetupOpts(t)
	opts.Backend = backend.NewRecorder(64, 50)
	r, err := NewRunnerFromVFS(opts)
	if err != nil {
		t.Fatalf("NewRunnerFromVFS: %v", err)
	}
	if r.Console.Width != render.MinConsoleWidth {
		t.Fatalf("narrow-fb Console.Width = %d want %d", r.Console.Width, render.MinConsoleWidth)
	}
}

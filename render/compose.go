// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import "errors"

// Compose2D is the 2D-only frame composer: it clears the framebuffer
// to a background fill, draws the drop-down console at its current
// animated position, and lays the notify-line overlay on top. The
// 3D world walk + alias model rendering + status-bar HUD layer over
// the result in follow-up batches.
//
// This is the minimum-viable per-frame pipeline: with just the
// renderer foundation + draw primitives + console state, a host
// loop can already produce visible frames (console + notify text)
// even before the 3D path lands.
//
// tyrquake: the 2D portion of SCR_UpdateScreen in screen.c -- the
// background fill + Con_DrawConsole + Con_DrawNotify calls, minus
// the 3D world view + Sbar_Draw + menu/HUD layers.
type FrameContext struct {
	Screen  *Screen
	Console *Console
	Chars   *Pic
	Palette *Palette

	Now            float32 // wall-clock-like time for the notify overlay
	NotifyLifetime float32 // seconds a notify row stays visible
	MaxNotifyRows  int     // upper bound on the overlay row count

	BackgroundIdx byte // palette index for the framebuffer fill

	// SkipBackgroundFill suppresses the per-frame DrawFill that
	// clears the framebuffer to BackgroundIdx. Set when the caller
	// has already rasterized a 3D scene into fb (via a Runner
	// Pre2DDraw hook or equivalent) and Compose2D's job is to
	// overlay the 2D layers (console + notify) on top WITHOUT
	// wiping the 3D pixels first.
	SkipBackgroundFill bool
}

var (
	ErrComposeNilFB      = errors.New("render: nil framebuffer in compose")
	ErrComposeNilScreen  = errors.New("render: nil screen in compose")
	ErrComposeNilConsole = errors.New("render: nil console in compose")
	ErrComposeNilChars   = errors.New("render: nil chars Pic in compose")
)

// Compose2D paints one full 2D frame into fb:
//
//  1. DrawFill the whole framebuffer to ctx.BackgroundIdx (skipped
//     when ctx.SkipBackgroundFill is true).
//  2. If the console is open (Screen.ConCurrent > 0): DrawConsole.
//  3. DrawNotify the recent-rows overlay.
//
// Returns:
//
//	ErrComposeNilFB        fb == nil
//	ErrComposeNilScreen    ctx.Screen == nil
//	ErrComposeNilConsole   ctx.Console == nil
//	ErrComposeNilChars     ctx.Chars == nil
//	(propagated draw errors)
//
// The palette field is NOT consumed by Compose2D itself (the
// composer writes palette-indexed bytes, not RGBA); it is bundled in
// the context for the convenience ExpandFrame helper that pipes
// the rendered framebuffer through Palette.Expand for display.
func Compose2D(fb *FrameBuffer, ctx FrameContext) error {
	if fb == nil {
		return ErrComposeNilFB
	}
	if ctx.Screen == nil {
		return ErrComposeNilScreen
	}
	if ctx.Console == nil {
		return ErrComposeNilConsole
	}
	if ctx.Chars == nil {
		return ErrComposeNilChars
	}

	// DrawFill's only error path is nil-fb, already caught above;
	// the call is infallible here. Skipped when the caller has
	// pre-drawn a 3D scene into fb.
	if !ctx.SkipBackgroundFill {
		_ = DrawFill(fb, 0, 0, fb.Width, fb.Height, ctx.BackgroundIdx)
	}

	if ctx.Screen.ConCurrent > 0 {
		if err := ctx.Screen.DrawConsole(fb, ctx.Console, ctx.Chars); err != nil {
			return err
		}
	}

	if err := ctx.Screen.DrawNotify(
		fb, ctx.Console, ctx.Chars,
		ctx.Now, ctx.NotifyLifetime, ctx.MaxNotifyRows,
	); err != nil {
		return err
	}

	return nil
}

// ExpandFrame runs Compose2D + then expands the resulting palette-
// indexed framebuffer to RGBA8 in dst (length must be >= fb.Width *
// fb.Height * 4). Convenience for backends that want a one-call
// "give me an RGBA frame" entry point.
//
// Returns ErrComposeNilFB / ErrComposeNilScreen / ... per Compose2D
// PLUS any error from FrameBuffer.Expand (notably ErrFBDstTooSmall).
func ExpandFrame(fb *FrameBuffer, dst []byte, ctx FrameContext) error {
	if err := Compose2D(fb, ctx); err != nil {
		return err
	}
	if ctx.Palette == nil {
		return ErrFBPaletteShape
	}
	return fb.Expand(dst, ctx.Palette)
}

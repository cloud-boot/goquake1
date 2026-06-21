// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import "errors"

var (
	ErrTestSceneNilFB    = errors.New("render: nil framebuffer in RenderTestScene")
	ErrTestSceneNilChars = errors.New("render: nil chars Pic in RenderTestScene")
)

// TestSceneOpts configures the composed test scene.
type TestSceneOpts struct {
	// Color used for the crosshair at the framebuffer center.
	CrosshairColor byte
	// First palette index for the top color bars.
	BarsStartIdx byte
	// Number of color bars at the top of the framebuffer.
	NumBars int
	// Banner text drawn at the top-centre (above the bars).
	BannerText string
}

// DefaultTestSceneOpts returns sensible defaults: a 16-bar color
// strip starting at palette index 16, a banner reading "PURE-GO
// QUAKE 1 ENGINE", and a yellow crosshair.
func DefaultTestSceneOpts() TestSceneOpts {
	return TestSceneOpts{
		CrosshairColor: 0xFF,
		BarsStartIdx:   16,
		NumBars:        16,
		BannerText:     "PURE-GO QUAKE 1 ENGINE",
	}
}

// RenderTestScene paints a self-contained smoke-test scene into fb:
//
//   1. Top band: TestPatternBars colorbars (opts.NumBars stripes from
//      BarsStartIdx)
//   2. Banner text just below the bars (centered horizontally,
//      using DrawCenteredString with the white glyph set)
//   3. Crosshair (Plus style) at the centre of the framebuffer
//
// Useful as a backend smoke-test: any backend that produces a
// recognizable colorbar+banner+crosshair frame is wired correctly.
//
// Returns ErrTestSceneNilFB / ErrTestSceneNilChars on nil args;
// propagates DrawCenteredString / DrawCharacter errors verbatim.
func RenderTestScene(fb *FrameBuffer, chars *Pic, opts TestSceneOpts) error {
	if fb == nil {
		return ErrTestSceneNilFB
	}
	if chars == nil {
		return ErrTestSceneNilChars
	}
	// Color bars at the top + crosshair are infallible past the
	// nil-fb checks above.
	_ = TestPatternBars(fb, opts.NumBars, opts.BarsStartIdx)
	// Banner text below the colorbars. DrawCenteredString can fail
	// if chars is a wrong-shape Pic; propagate.
	bannerY := fb.Height / 4
	if err := DrawCenteredString(fb, chars, fb.Width/2, bannerY, opts.BannerText); err != nil {
		return err
	}
	_ = DrawCrosshair(fb, fb.Width/2, fb.Height/2, opts.CrosshairColor, CrosshairPlus)
	return nil
}

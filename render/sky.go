// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import "errors"

// SkyScrollSpeed is the horizontal scroll rate (texels per second)
// for the cloud-drift effect. tyrquake hard-codes a similar value
// inside R_DrawSkyChain's per-second scroll.
const SkyScrollSpeed float32 = 8.0

// SkyYawScale is the texels-per-yaw-degree factor. A full 360° pan
// walks the texture's width; tex.Width / 360 = texels/deg.
// (Caller-friendly default; the renderer can override.)
const SkyYawScale float32 = 1.0

var (
	ErrSkyNilFB  = errors.New("render: nil framebuffer in DrawSkyHorizon")
	ErrSkyNilTex = errors.New("render: nil sky texture in DrawSkyHorizon")
)

// DrawSkyHorizon paints a horizontal band of sky into the top of
// the framebuffer. The sky texture is sampled with wrap-around
// horizontal U (based on viewYawDeg + timeSec * SkyScrollSpeed) and
// linear V mapped from band_top..band_bottom across the texture
// height.
//
// tyrquake: R_DrawSkyChain in r_sky.c (simplified -- the upstream
// version walks the BSP sky surface list and warps + composites
// the cloud-overlay layer; this MVP path paints a uniform band).
//
// `bandHeight` is the pixel height of the sky band. The band lives
// at y = 0..bandHeight-1 of the framebuffer.
//
// Returns:
//
//	ErrSkyNilFB   fb == nil
//	ErrSkyNilTex  skyTex == nil
//	nil otherwise (bandHeight <= 0 is a silent no-op)
func DrawSkyHorizon(fb *FrameBuffer, skyTex *Pic, viewYawDeg float32, timeSec float32, bandHeight int) error {
	if fb == nil {
		return ErrSkyNilFB
	}
	if skyTex == nil {
		return ErrSkyNilTex
	}
	if bandHeight <= 0 {
		return nil
	}
	if bandHeight > fb.Height {
		bandHeight = fb.Height
	}

	// Compute the horizontal U offset from view yaw + time scroll.
	uOffset := viewYawDeg*SkyYawScale + timeSec*SkyScrollSpeed
	// Wrap into [0, skyTex.Width).
	skyW := float32(skyTex.Width)
	uOffset -= float32(int(uOffset/skyW)) * skyW
	if uOffset < 0 {
		uOffset += skyW
	}

	for y := 0; y < bandHeight; y++ {
		// V mapping: scale band [0, bandHeight) into texture [0, Height).
		v := y * skyTex.Height / bandHeight
		texRow := v * skyTex.Width
		fbRow := y * fb.Pitch
		for x := 0; x < fb.Width; x++ {
			// uOffset is pre-clamped to [0, skyTex.Width) above so
			// (uOffset + x) is non-negative; positive mod stays >= 0.
			u := int(uOffset+float32(x)) % skyTex.Width
			fb.Pixels[fbRow+x] = skyTex.Pixels[texRow+u]
		}
	}
	return nil
}

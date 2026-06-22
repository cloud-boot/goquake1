// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package benchmarks holds performance-parity micro-benchmarks that
// compare go-quake1's software-rendering hot loops against the C engine
// it was hand-ported from (tyrquake@6531579). It lives in its own
// module-internal directory so it is excluded from the per-package
// 100%-coverage gate (the gate measures the engine packages, not this
// benchmark-only tree).
//
// The headline loop is the perspective-correct textured span filler
// (render.FillPerspectiveTexturedPolygon), which is the Go port of
// tyrquake's D_DrawSpans8 (common/d_scan.c) -- the dominant cost of a
// Quake software-rendered frame. The C counterpart is benchmarked by
// benchmarks/c/cbench_spans.c (a verbatim copy of D_DrawSpans8 driven
// over the same full-screen textured surface). See BENCHMARKS.md.
package benchmarks

import (
	"testing"

	"github.com/go-quake1/engine/render"
)

// Frame geometry: the canonical Quake software-render resolution.
const (
	benchW = 320
	benchH = 200
	texW   = 64
	texH   = 64
)

func makeTex() *render.Pic {
	px := make([]byte, texW*texH)
	for i := range px {
		px[i] = byte((i*37 + 11) & 0xff)
	}
	return &render.Pic{Width: texW, Height: texH, Pixels: px}
}

// fullScreenPerspQuad returns a perspective-textured quad covering the
// whole 320x200 view with a depth gradient (near Z at the top, far Z at
// the bottom) so the per-8px 1/z divide in FillPerspectiveTexturedPolygon
// actually runs -- matching the slanted plane the C harness drives
// through D_DrawSpans8.
func fullScreenPerspQuad() []render.PerspTexturedVertex {
	// Z gradient: top of screen is close, bottom is far away, plus a
	// slight left/right tilt so U/V perspective is non-trivial.
	const zTopL, zTopR = 1.0, 1.4
	const zBotL, zBotR = 4.0, 5.0
	// UV spans several texture repeats across the surface so sampling
	// touches the whole texture (clamped inside the filler).
	return []render.PerspTexturedVertex{
		{X: 0, Y: 0, Z: zTopL, U: 0, V: 0},
		{X: benchW, Y: 0, Z: zTopR, U: texW * 4, V: 0},
		{X: benchW, Y: benchH, Z: zBotR, U: texW * 4, V: texH * 4},
		{X: 0, Y: benchH, Z: zBotL, U: 0, V: texH * 4},
	}
}

// BenchmarkPerspSpanFill320x200 measures one full 320x200 perspective-
// textured frame through FillPerspectiveTexturedPolygon (the D_DrawSpans8
// port). Report ms/frame = (ns/op)/1e6; ns/pixel = (ns/op)/(320*200).
func BenchmarkPerspSpanFill320x200(b *testing.B) {
	fb, err := render.NewFrameBuffer(benchW, benchH)
	if err != nil {
		b.Fatal(err)
	}
	tex := makeTex()
	verts := fullScreenPerspQuad()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := render.FillPerspectiveTexturedPolygon(fb, tex, nil, 0, verts); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	pixels := float64(benchW * benchH)
	nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
	b.ReportMetric(nsPerOp/1e6, "ms/frame")
	b.ReportMetric(nsPerOp/pixels, "ns/pixel")
}

// BenchmarkPerspSpanFillLit320x200 is the lit variant (per-pixel
// colormap lookup), the cost actually paid in-engine for world surfaces.
func BenchmarkPerspSpanFillLit320x200(b *testing.B) {
	fb, err := render.NewFrameBuffer(benchW, benchH)
	if err != nil {
		b.Fatal(err)
	}
	tex := makeTex()
	var cm render.ColorMap
	for r := range cm {
		for c := range cm[r] {
			cm[r][c] = byte((r + c) & 0xff)
		}
	}
	verts := fullScreenPerspQuad()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := render.FillPerspectiveTexturedPolygon(fb, tex, &cm, 0, verts); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	pixels := float64(benchW * benchH)
	nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
	b.ReportMetric(nsPerOp/1e6, "ms/frame")
	b.ReportMetric(nsPerOp/pixels, "ns/pixel")
}

// BenchmarkAffineSpanFill320x200 measures the affine textured filler
// (FillTexturedPolygon, tyrquake's D_DrawSpans affine fast path used for
// alias-model triangles), for completeness.
func BenchmarkAffineSpanFill320x200(b *testing.B) {
	fb, err := render.NewFrameBuffer(benchW, benchH)
	if err != nil {
		b.Fatal(err)
	}
	tex := makeTex()
	verts := []render.TexturedVertex{
		{X: 0, Y: 0, U: 0, V: 0},
		{X: benchW, Y: 0, U: texW * 4, V: 0},
		{X: benchW, Y: benchH, U: texW * 4, V: texH * 4},
		{X: 0, Y: benchH, U: 0, V: texH * 4},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := render.FillTexturedPolygon(fb, tex, nil, 0, verts); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	pixels := float64(benchW * benchH)
	nsPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
	b.ReportMetric(nsPerOp/1e6, "ms/frame")
	b.ReportMetric(nsPerOp/pixels, "ns/pixel")
}

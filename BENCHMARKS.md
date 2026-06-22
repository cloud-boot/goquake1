# Performance parity — go-quake1 vs tyrquake (2026-06-22)

Bar: **as fast as the original C engine** (tyrquake @6531579, the exact
upstream go-quake1 was hand-ported from).

## Methodology

* **Machine:** Apple-silicon Tart Linux VM (Debian 13 trixie, aarch64,
  4 vCPU). Both the Go engine and the C engine were built and run **in
  the same VM** — same CPU, same arch, same OS — so the comparison is a
  level playing field.
* **Go:** go1.26.4 linux/arm64, `CGO_ENABLED=0`.
* **C engine:** tyrquake **@6531579** (the commit recorded in the
  port's lineage), built with its own `-O3` CFLAGS.
* **Demo / scene:** the dominant software-render inner loop — the
  perspective-correct textured **span filler** — driven over a full
  **320x200** (canonical Quake software resolution) slanted textured
  plane, so the per-8-pixel `1/z` divide actually runs. Software
  renderer, headless, no GPU, no SIMD on either side.
* **Metric:** wall time per full 320x200 textured frame, and ns/pixel
  (median of repeated runs).

### Why a micro-benchmark and not a full `timedemo`

A full headless `timedemo` is **not yet shippable** for go-quake1: there
is no production host loop wiring the BSP-walk → face-transform →
rasterize pipeline end-to-end on a host OS (only the TamaGo bare-metal
`quake-tamago` target wires it, against a virtio framebuffer), and no
proprietary Quake map data (`pak0.pak`) is bundled. Rather than fabricate
a full-engine number, we benchmark the **hottest inner loop** that
dominates a Quake software frame — `D_DrawSpans8` — against tyrquake's
verbatim C. This is the apples-to-apples loop; the BSP walk and edge
stepping are comparatively cheap and map 1:1 to the same C.

The C reference is a **verbatim copy** of tyrquake's `D_DrawSpans8`
(`common/d_scan.c`); see [`benchmarks/c/cbench_spans.c`](benchmarks/c/cbench_spans.c).
go-quake1's counterpart is `render.FillPerspectiveTexturedPolygon`
([`render/texfill_persp.go`](render/texfill_persp.go)).

## Results (320x200, median)

| engine | loop | ours ms/frame (ns/px) | tyrquake C ms/frame (ns/px) | ratio | verdict |
|--------|------|-----------------------|------------------------------|-------|---------|
| go-quake1 | perspective textured span (`D_DrawSpans8` port) | **0.107** (1.68) | 0.0287 (0.45) | **3.7×** slower | gap |
| go-quake1 | + per-pixel light (colormap) | 0.119 (1.87) | — | — | (lit path) |
| go-quake1 | affine textured span (alias models) | 0.087 (1.36) | — | — | (affine path) |

**Frame-time ratio vs the original C: ~3.7× slower** on the perspective
span filler, the loop that dominates world rendering.

## Where the gap is

1. **Float UV vs 16.16 fixed-point.** tyrquake's `D_DrawSpans8` samples
   the texture with integer `s>>16`/`t>>16` fixed-point and steps with
   integer adds. go-quake1's `FillPerspectiveTexturedPolygon` carries
   `float32` U/V and does `math.Floor` + clamp **per pixel**. That's the
   single biggest cost: a float→int conversion and two branches per
   texel where C does a shift.
2. **No SIMD.** Both are scalar today, but the fixed-point integer inner
   loop vectorizes far more readily than the float+Floor one.
3. **Bounds checks.** Go inserts per-access bounds checks on
   `tex.Pixels[...]` and `fb.Pixels[...]`; the allocation profile is
   already 0 B/op, but the checks remain in the hot loop.

## Action items (to reach C parity)

- [ ] **Fixed-point hot path.** Replace per-pixel `float32`+`math.Floor`
      UV with 16.16 fixed-point `s`/`t` accumulators (`int32`), matching
      `D_DrawSpans8` exactly. Expected to close most of the 3.7× gap on
      its own — this is the dominant difference.
- [ ] **SIMD span draw via go-asmgen.** Once fixed-point, emit the
      texel gather + store with go-asmgen across the 6 64-bit arches
      (amd64/arm64/riscv64/loong64/ppc64le/s390x), CGO=0.
- [ ] **Hoist bounds checks.** Slice the destination row once
      (`row := fb.Pixels[base : base+count]`) and the texture row per
      sub-span so the compiler proves the indices in-range.
- [ ] Re-benchmark against this same tyrquake @6531579 baseline; target
      `ratio <= 1.0`.

## Reproduce

```sh
# Go (our engine), from engine/ root:
GOWORK=off go test -run=^$ -bench=. -benchtime=2s -count=3 ./benchmarks/

# C reference (tyrquake D_DrawSpans8):
cc -O3 -std=gnu99 -o /tmp/cbench_spans benchmarks/c/cbench_spans.c
/tmp/cbench_spans 8000
```

See [`benchmarks/README.md`](benchmarks/README.md) for details.

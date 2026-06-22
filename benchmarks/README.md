# go-quake1 performance-parity harness

Reproducible micro-benchmarks comparing go-quake1's software-render hot
loops against **tyrquake @6531579** (the C engine this was hand-ported
from). See [`../BENCHMARKS.md`](../BENCHMARKS.md) for results and analysis.

This directory carries **no production code** — only `Benchmark*`
functions and a standalone C reference — so it is outside the engine's
per-package 100%-coverage gate.

## Go side (our engine)

```sh
# from engine/ root
GOWORK=off go test -run=^$ -bench=. -benchtime=2s -count=3 ./benchmarks/
```

Benchmarks (320x200, the canonical Quake software resolution):

| benchmark | engine path | tyrquake counterpart |
|-----------|-------------|----------------------|
| `BenchmarkPerspSpanFill320x200`    | `render.FillPerspectiveTexturedPolygon` | `D_DrawSpans8` (common/d_scan.c) |
| `BenchmarkPerspSpanFillLit320x200` | same + per-pixel colormap | `D_DrawSpans8` + light |
| `BenchmarkAffineSpanFill320x200`   | `render.FillTexturedPolygon` | affine span (alias models) |

The reported `ms/frame` is the wall time to paint one full 320x200
perspective-textured surface; `ns/pixel` divides by 320*200.

## C reference (tyrquake D_DrawSpans8)

[`c/cbench_spans.c`](c/cbench_spans.c) embeds a **verbatim** copy of
tyrquake @6531579's `D_DrawSpans8` (only the surrounding plain-`extern`
globals are provided locally so it links standalone) and drives it over
the same full-screen slanted textured plane.

```sh
cc -O3 -std=gnu99 -o cbench_spans c/cbench_spans.c   # -O3 = tyrquake's CFLAGS
./cbench_spans 8000
```

## Fairness

* Same machine, same arch (run both in one Linux VM), same resolution,
  same 8-px perspective-subdivision algorithm, software renderer,
  headless. No GPU, no SIMD on either side.
* go-quake1 cannot yet run a full headless `timedemo` (no production host
  loop drives the BSP-walk → rasterize pipeline end-to-end outside the
  TamaGo bare-metal target, and no shippable Quake map data is bundled),
  so we benchmark the **dominant inner loop** — the textured span filler —
  honestly, not a fabricated full-engine number.

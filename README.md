# goquake1

Pure-Go Quake 1 (software renderer) for bare-metal TamaGo+UEFI, **hand-
ported** from the [tyrquake][tyr] NetQuake (single-player) branch and
wrapped in cloud-boot virtio adapters.

Sibling of [cloud-boot/godoom][godoom] in the [DOOM -> Quake roadmap][roadmap].

[tyr]: https://github.com/sezero/tyrquake
[godoom]: https://github.com/cloud-boot/godoom
[roadmap]: https://github.com/cloud-boot/docs/blob/main/docs/architecture/quake-roadmap.md

## Status

**Phase Q-1a in flight.** Hand-porting tyrquake-NQ to pure Go, one
module at a time, with parity tests against the upstream C behaviour.

| Component                          | State                                |
|------------------------------------|--------------------------------------|
| Porting conventions ([CONVENTIONS.md](CONVENTIONS.md)) | done                  |
| `reference/` tyrquake source mirror | pending                             |
| engine/mathlib                     | pending (first port; proof of approach) |
| engine/crc, mdfour, zone, cvar, cmd | pending (foundational utils)        |
| engine/common (string ops, parsing) | pending                             |
| engine/server                      | pending                              |
| engine/client + soft renderer      | pending                              |
| backend/tamago/ virtio adapters    | pending (mirrors godoom shape)       |
| embedpak shareware loader          | pending                              |
| cmd/harvest-reference oracle       | pending                              |
| phaseQ1_oci_quake1_soft_boot.go    | pending (in cloud-boot/tamago-uefi)  |
| provable-test gates A/B/C-1/C-2    | pending                              |

## Why hand-port (not ccgo, not ironwail-go)

Phase Q-1a's [open question Q-1][roadmap-q1] was first answered
"ccgo-transpile" -- a mechanical C-to-Go translation via
`modernc.org/ccgo/v4` of a Linux-portable Quake source tree. We tried
two upstreams in earnest and documented why both failed:

- **quakeforge** uses C23 (`nullptr`, `#embed`, etc.); ccgo's parser
  predates C23. See [internal/transpile/FINDING-C23.md](internal/transpile/FINDING-C23.md).
- **tyrquake** is clean C99 but ccgo's internal checker drops symbols
  on real game-code constructs (extern globals, function-pointer
  dispatch tables, packed structs). The link stage then reports
  "undefined: com_argc" even though com_argc IS defined in common.c.
  See [internal/transpile/FINDING-CCGO-LIMITS.md](internal/transpile/FINDING-CCGO-LIMITS.md).

ccgo is production-quality for SQLite-class C (it's literally how
modernc.org/sqlite is built), but Quake's surface exposes the v4
checker's soft spots faster than ccgo's curated test corpus does.

The remaining options were:

| Option                              | Velocity | Audit posture | Picked |
|-------------------------------------|----------|---------------|--------|
| Fork `darkliquid/ironwail-go`       | fastest  | low (AI-assisted upstream, unreviewed) | no |
| Hand-port (LLM-assisted, from tyrquake-NQ) | medium | high (every line under operator's git history) | **YES** |
| Wait on ccgo upstream maturity      | slowest  | high          | deferred (track as a watch) |

The hand-port also gives us natural alignment with TamaGo constraints
(no malloc churn, no goroutines for the main loop, careful with `go`
keyword pressure on the bare-metal scheduler) that no transpiled engine
would ship with out of the box.

[roadmap-q1]: https://github.com/cloud-boot/docs/blob/main/docs/architecture/quake-roadmap.md

## How a port lands

For each tyrquake C module:

1. **Mirror** the C source verbatim into `reference/` (no edits) so the
   diff against upstream stays inspectable.
2. **Port** the module under `engine/<module>/<module>.go` following
   the rules in [CONVENTIONS.md](CONVENTIONS.md). Hand-written or LLM-
   drafted then operator-reviewed; either way the final Go is operator-
   owned.
3. **Test** parity against the C upstream: feed the same input through
   both and assert byte-equal output where the operation is
   deterministic (math, hash, parse), or bounded-tolerance where it is
   not (float trig).
4. **Coverage** target = 100% of the new Go package (the project-wide
   convention inherited from go-virtio and go-deltasync).
5. **Commit** with a `port: <module>` prefix. Each commit lands one
   buildable, tested module so bisecting the engine remains practical.

## Data: shareware `pak0.pak`

`pak0.pak` for the shareware Episode 1 (Dimension of the Doomed) is
freely redistributable per id Software's grant and lands in-tree under
`embedpak/`. Operators can override at boot with their own pak0+pak1
via a probe env. CI gates always exercise the committed shareware pak
so the reference oracle is reproducible.

## Provable test protocol (inherited)

Every Quake phase inherits the four-gate protocol shipped with DOOM
([doom-provable-protocol.md][protocol]):

- **GATE A** -- engine determinism, BYTE-EQUAL frames at checkpoint tics
- **GATE B** -- guest virtio-gpu fidelity, chi-squared <= tolerance
- **GATE C-1** -- audio event stream, BYTE-EQUAL CacheSound/PlaySound log
- **GATE C-2** -- guest WAV bounded tolerance (per-second RMS envelope)

[protocol]: https://github.com/cloud-boot/docs/blob/main/docs/architecture/doom-provable-protocol.md

## License

- Wrapper code (backend, embedpak, cmd, internal, .github): **BSD-3-Clause**
- Ported engine subtree under `engine/`: **GPL-2.0-or-later** (inherited
  from tyrquake / upstream id Quake source)
- The `reference/` mirror of tyrquake C: **GPL-2.0-or-later** (verbatim)

See [LICENSE](LICENSE) for the full split.

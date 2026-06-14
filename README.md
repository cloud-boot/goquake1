# goquake1

Pure-Go Quake 1 (software renderer) for bare-metal TamaGo+UEFI, built by
ccgo-transpiling the [quakeforge][qf] engine and wrapping it in
cloud-boot virtio adapters.

Sibling of [cloud-boot/godoom][godoom] in the [DOOM -> Quake roadmap][roadmap].

[qf]: https://github.com/quakeforge/quakeforge
[godoom]: https://github.com/cloud-boot/godoom
[roadmap]: https://github.com/cloud-boot/docs/blob/main/docs/architecture/quake-roadmap.md

## Status

**Phase Q-1a in flight.** The ccgo pipeline is validated (see
internal/transpile/), but the full quakeforge-to-Go translation is the
sprint that this README will track as it lands.

| Component                         | State                                |
|-----------------------------------|--------------------------------------|
| ccgo binary + Debian 13 toolchain | done (`internal/transpile/`)         |
| quakeforge preprocessing pass     | in progress                          |
| engine.go (transpiled engine)     | pending                              |
| backend/tamago/ virtio adapters   | pending (mirrors godoom shape)       |
| embedpak shareware loader         | pending                              |
| cmd/harvest-reference oracle      | pending                              |
| phaseQ1_oci_quake1_soft_boot.go   | pending (in cloud-boot/tamago-uefi)  |
| provable-test gates A/B/C-1/C-2   | pending                              |

## Why ccgo + quakeforge

Phase Q-1a's open questions Q-1 (engine choice) and Q-2 (PAK source) were
[decided][roadmap] in favour of:

- **Source of the engine**: mechanical translation via `modernc.org/ccgo/v4`.
  Yields pure-Go output that imports `modernc.org/libc` only -- no cgo,
  no vendored binaries, audit-friendly because every Go function maps to
  one C function. Same posture as the rest of the cloud-boot stack
  ("compile from source is the proof of independence").

- **Upstream**: quakeforge instead of id-Software/Quake. id-Software's tree
  is 1996-era Win/DOS-centric; even `menu.c` and `snd_dma.c` pull
  `winquake.h`. quakeforge is the most portable Quake re-implementation
  with a real Linux-first build, and most of its headers are BSD/MIT.
  The transpiled engine still inherits quakeforge's GPL-2.0; the wrapper
  layer in this repo stays BSD-3-Clause.

- **Game data**: shareware `pak0.pak` (Dimension of the Doomed, episode 1)
  embedded in-tree via `embedpak/`. Same pattern as `embedwad` for DOOM.
  Operators can override with their own pak0+pak1 via a probe env, but
  CI gates always exercise the committed shareware pak so the reference
  oracle is reproducible.

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
- Transpiled engine subtree under `engine/`: **GPL-2.0-or-later** (inherited
  from quakeforge)

See [LICENSE](LICENSE) for the full split.

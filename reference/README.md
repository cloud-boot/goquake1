# reference/

Verbatim mirror of tyrquake's `common/`, `include/`, and `NQ/`
subdirectories, pinned to a known-good upstream commit.

**Upstream**: <https://github.com/sezero/tyrquake>
**Pinned commit**: `653157915975b196e36980a1ef7146485509b69a`
**License**: GPL-2.0-or-later (inherits from id Software's Quake source
release)
**Layout**:

- `common/` -- C source shared between NetQuake and QuakeWorld engines
- `include/` -- shared C headers
- `NQ/` -- NetQuake (single-player) engine entry points + main loop

The `QW/` (QuakeWorld) and `gas2masm/`, `wine-dx/` directories are NOT
mirrored because Q-1a only ports the single-player NetQuake engine.

## Why mirror at all

This subtree is the **diff-against-upstream** anchor for every port
that lands in `engine/`. When a tyrquake bugfix or upstream-rebase
question comes up, you can:

```sh
git diff reference/common/cmd.c                       # what did we change?
git log --follow reference/common/cmd.c               # when last refreshed?
diff -ru reference/ /tmp/fresh-tyrquake-clone/        # what's drifted?
```

without leaving the goquake1 repository.

## Refreshing

To re-pin to a newer tyrquake:

```sh
git clone --depth 1 https://github.com/sezero/tyrquake /tmp/tyrquake
( cd /tmp/tyrquake && git rev-parse HEAD )            # new pin SHA
rm -rf reference/{common,include,NQ}
cp -r /tmp/tyrquake/{common,include,NQ} reference/
# Update the pinned-commit line above + the per-module port commits
git commit -am "reference: refresh tyrquake mirror to <new-sha>"
```

Re-pinning is a deliberate operator action, NOT continuous. The
`engine/` ports reference specific upstream behaviour that may change
across pins; each refresh needs the ports' parity tests re-validated.

## What is NOT here

- Build files (`Makefile`, `Makefile.*`): we hand-port; no make.
- `gas2masm/`: assembly translation helpers; the port stays in C
  semantics, not asm.
- `QW/`: QuakeWorld engine; out of scope for Q-1a (single-player only).
- `wine-dx/`: Wine DirectX header shim; we have virtio-gpu directly.
- `docs/`, `data/`: not source code.

If a port discovers it needs a header from one of these excluded
directories, copy it in deliberately with a one-line note in this
README explaining why.

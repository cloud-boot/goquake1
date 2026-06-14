# Transpile finding: quakeforge requires C23, ccgo only supports C11/C17

Date: 2026-06-14 (Q-1a kickoff)

## Summary

The first end-to-end pipeline run succeeded structurally (configure passed,
454 .c files fed to ccgo, exit code 0) but `engine/engine.go` was NOT
emitted because of widespread parse failures upstream of the link stage.

The root cause is a **language-version mismatch** between the upstream
(quakeforge `master`, dated 2026 work, written in C23) and the transpiler
(`modernc.org/ccgo/v4` v4.34.4, which currently parses C11/C17).

## Reproducer

Inside the `goquake-build` Tart VM (debian:13 trixie arm64, clang-19):

```sh
cd /tmp/goquake1
bash internal/transpile/run.sh
```

Exit code 0. `internal/transpile/transpile.log` is 3231 lines, 315 KB,
and the FIRST 10 errors are parse failures inside quakeforge headers:

```
libs/audio/cd_sdl.c:38:10: include file not found: <SDL.h>          # skip.txt over-skips
libs/audio/cd_sgi.c:33:10: include file not found: <dmedia/cdaudio.h>
include/QF/cdaudio.h:34:35: unexpected identifier, expected ')'
include/QF/cbuf.h:53:14: unexpected '*', expected ';'
include/QF/cbuf.h:67:14: unexpected identifier, expected ';'
include/QF/cbuf.h:75:29: unexpected '*', expected ')'
include/QF/cbuf.h:76:28: unexpected '*', expected ')'
include/QF/cbuf.h:77:25: unexpected '*', expected ')'
include/QF/cbuf.h:78:23: unexpected '*', expected ')'
include/QF/cbuf.h:79:26: unexpected '*', expected ')'
```

The "undefined: nullptr" / "undefined: true" entries near the tail of the
log are the same root cause surfacing at the symbol-resolution stage:

```
libs/thread/deque.c:21:16: undefined: nullptr (check.go:5024:check:)
libs/thread/notifier.c:60:34: undefined: nullptr (check.go:5024:check:)
libs/util/bsearch.c:10:16: undefined: true (check.go:5024:check:)
libs/util/bsearch.c:40:16: undefined: true (check.go:5024:check:)
```

`nullptr` and bare `true`/`false` as expressions are C23 keywords. ccgo
v4.34.4 reports them as undefined identifiers because its parser predates
C23 keyword recognition.

## Decision (open for the operator to confirm)

The cleanest paths forward, in order of estimated effort:

1. **Re-pick the upstream to a C11-compatible Quake source.** Candidates
   surveyed at the original [Quake roadmap §2][roadmap]:
   - `tyrquake` (sezero/tyrquake) -- lightweight, C99, very portable;
     has no GL dep in the soft-renderer build target; no SDL pull at
     header level.
   - `darkplaces` (cloudwalk-hub/darkplaces) -- cross-platform but
     heavier; mostly C99 with a couple C11 atomics; pulls SDL by default.
   - `vkQuake` soft-renderer fork -- mostly C11.

   `tyrquake` looks like the best fit if it builds without SDL; needs
   a one-day spike to confirm.

2. **Wait for / push a fix into ccgo.** modernc.org/ccgo's C23 support is
   on the upstream roadmap; tracking issue exists but no ETA. Could file
   a minimal-repro PR upstream. High value (helps other ccgo users) but
   bypassed by option 1 for our immediate goal.

3. **Patch the C23-isms out of quakeforge.** Replace `nullptr` -> `NULL`,
   `(bool){true}` -> `(bool){1}`, replace `#embed` with a generated `.c`
   data array. ~50 sites in quakeforge core. Maintainable for our fork
   but creates a permanent rebase burden against upstream.

[roadmap]: https://github.com/cloud-boot/docs/blob/main/docs/architecture/quake-roadmap.md

## Sanity check of the pipeline itself

Despite the C23 mismatch, this run validated the pipeline machinery
end-to-end:

- `CC=clang-19` is auto-detected and passed through.
- `./bootstrap` + `./configure --with-clients=fbdev --with-servers='nq qw'
  --enable-static --disable-asmopt --without-libcurl` reaches "configured
  successfully".
- The candidate `.c` file enumeration produces 454 files after `skip.txt`
  filtering. (Note: `cd_sdl.c` and `cd_sgi.c` leaked through -- the
  patterns under `^libs/audio/targets/` don't match because quakeforge
  organises CD audio at `libs/audio/cd_*` flat. Tighten in follow-up.)
- ccgo loads the file set without crashing, parses ~75% of them, fails
  cleanly on the C23 hosts, and reports the gap count.

The pipeline is ready; the next operator decision is option 1, 2, or 3.

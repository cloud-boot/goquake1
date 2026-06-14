# Transpile finding: ccgo v4.34.4 is too immature for any real Quake source

Date: 2026-06-14 (Q-1a kickoff, second iteration after the quakeforge C23
finding in [FINDING-C23.md](FINDING-C23.md))

## TL;DR

After re-piloting to **tyrquake** (a C99 codebase, no SDL.h pulls in core
headers), the transpile pipeline now reaches a stricter test: feed
`modernc.org/ccgo/v4` v4.34.4 actual Quake C and see what comes out.

**Outcome**: ccgo handles tyrquake's small math/utility files cleanly,
but the moment we add anything substantial (common.c, pr_edict.c,
sv_main.c, menu.c, snd_mem.c, r_*.c, etc.) it emits Go that either:

- **Fails `gofmt -s` validation**, leaving an `.o.go` on disk that the
  linker then cannot include into the final engine.go.
- **Skips symbol export** under a "TODO *cc.PrimaryExpression
  missed/failed type check" path inside ccgo's internal checker
  (lib/check.go:5068), so downstream files that reference those symbols
  (e.g. `com_argc` defined in common.c, used in sys_null.c) fail to
  link with "undefined: X".

The link-stage "undefined: X" errors are NOT real missing symbols --
they are symbols ccgo silently dropped from earlier translation units.

## Reproducer

```sh
# Inside the goquake-build Tart VM
git clone --depth 1 https://github.com/sezero/tyrquake.git /tmp/tyrquake
mkdir -p /tmp/poc/{common,include,NQ}
cp /tmp/tyrquake/common/*.{c,h} /tmp/poc/common/
cp /tmp/tyrquake/include/*.h    /tmp/poc/include/
cp /tmp/tyrquake/NQ/*.{c,h}     /tmp/poc/NQ/
cd /tmp/poc
go mod init poc && go get modernc.org/libc

# Tiny set -- ccgo handles it (just one "Sys_Error" extern, as expected)
ccgo -DNQ_HACK -INQ -Iinclude -Icommon -include NQ/quakedef.h \
    -o mathlib.go common/mathlib.c
# Result: mathlib.o.go: 1820 lines, "Sys_Error external" link gap only.

# Add common.c + sys_null.c -- now common.c silently drops com_argc
ccgo -DNQ_HACK -INQ -Iinclude -Icommon -o engine.go \
    common/mathlib.c common/crc.c common/zone.c common/cvar.c \
    common/cmd.c common/common.c common/sys_null.c
# Result: "common/sys_null.c:155:18: undefined: com_argc"
#         "TODO *cc.PrimaryExpression missed/failed type check ..."
# com_argc IS defined in common.c (line 979). ccgo dropped it.
```

## Why both upstream candidates fail

| upstream     | language | obstacle                                                              |
|--------------|----------|-----------------------------------------------------------------------|
| quakeforge   | C23      | ccgo's parser predates C23 keywords (`nullptr`, `true`/`false`)       |
| tyrquake     | C99      | ccgo's internal checker has "TODO" gaps that drop symbols silently    |

ccgo IS production-quality for SQLite and similar carefully-curated C
projects (the modernc.org/sqlite Go module is famously transpiled this
way). Real-world game code -- with extern globals, function-pointer
tables, packed structs, `setjmp`/`longjmp`, and the surface-of-the-sun
of preprocessor gymnastics that Quake uses -- exposes the checker's
soft spots faster than the package's curated test corpus does.

## Revised options for Phase Q-1a (operator decision needed)

This is a real revision of the original [Quake roadmap Q-1
decision][roadmap-q1] (chosen path: ccgo-transpile). Three viable
alternatives, ranked by how soon a working Q-1a probe could land:

1. **Fork `darkliquid/ironwail-go`** (the choice the operator initially
   set aside in Q-1 for audit posture). Pure-Go, working today, AI-
   assisted but exists. Week-1 effort: wrap it with our virtio
   adapters and `embedpak`. Risk: the upstream is unreviewed; we
   inherit whatever bugs it has, OR we audit it line-by-line which
   undoes the time savings.

2. **Hand-port one of the candidate engines via an LLM-assisted
   workflow** (basically what ironwail-go did, but us doing it
   ourselves on a known-good base like tyrquake-NQ). Predictable
   ownership but several weeks of effort. Aligns with the operator's
   standing rule "compile from source = proof of independence" if you
   read "source" as "C source" -- the Go output is OUR translation.

3. **Stay on ccgo, file upstream bug reports + workarounds.** Submit
   minimal reproducers of the "missed type check" paths to modernc.org
   ccgo upstream, contribute fixes, wait for releases. Months of
   calendar time. Highest-value contribution to the broader Go +
   modernc ecosystem; lowest velocity for our cloud-boot Q-1a goal.

4. (Rejected.) Patch ccgo's checker ourselves -- unscoped, ccgo's
   internals are not designed for downstream forks.

[roadmap-q1]: https://github.com/cloud-boot/docs/blob/main/docs/architecture/quake-roadmap.md

## What we keep regardless of which option wins

The scaffolding committed for the ccgo path is NOT wasted even if we
pivot away from ccgo for Q-1a:

- The `cloud-boot/goquake1` repo + BSD-3/GPL-2.0 license split.
- The `internal/transpile/Dockerfile` (any future C-to-Go work needs
  the same clang-19 + apt deps + Go toolchain).
- The `internal/transpile/run.sh` driver shape (clone -> bootstrap ->
  configure -> filter sources -> feed translator -> capture log).
- The `skip.txt` ERE-pattern approach -- generic across translators.
- The 4-gate provable-test protocol inherited from godoom.

What changes is the line "ccgo X" inside `run.sh`'s step 6: it becomes
either an ironwail-go fetch (option 1) or our own hand-port glue
(option 2) or stays put and waits (option 3).

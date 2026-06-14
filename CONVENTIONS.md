# CONVENTIONS.md -- hand-port rules for goquake1

How tyrquake C maps to goquake1 Go. Read this before writing or
reviewing any `engine/` code so the port stays consistent across the
~100 modules it spans.

## Mental model

The goal is a Go translation that is:

- **Readable** as Go, not as C-disguised-as-Go (no `void*`, no leading
  underscores, no all-caps function names).
- **Bit-exact** to upstream tyrquake for any deterministic operation
  (parser output, hash, fixed-point math, demo-replay byte stream).
- **Bare-metal-friendly**: no goroutines for the main loop, no
  background GC pressure from per-frame allocations, no os/syscall
  imports in `engine/`.
- **Inspectable** against upstream: every Go source file names its
  upstream `.c`/`.h` origin at the top.

## Per-module layout

```
engine/<module>/<module>.go        // ported code
engine/<module>/<module>_test.go   // parity + unit tests
engine/<module>/doc.go             // package doc + upstream origin
reference/<module>.c               // verbatim tyrquake source
reference/<module>.h               // verbatim tyrquake header
```

One Go package per logical C module. The package name matches the C
filename root (so `mathlib.c` -> `package mathlib`, `cvar.c` -> `package
cvar`). When two C files implement one logical module (e.g.
`pr_edict.c` + `pr_cmds.c` + `pr_exec.c`), they merge into a single Go
package (`engine/progs/`).

## C-to-Go mapping rules

### Identifiers

| C convention                | Go convention                          |
|-----------------------------|----------------------------------------|
| `Cmd_AddCommand`            | `cmd.Add` (drop module prefix; package qualifies it) |
| `cvar_t` typedef            | `cvar.Var` (typedef root, dropped `_t`) |
| `MAX_QPATH`                 | `MaxQPath` (or `const MaxQPath`)       |
| `extern int com_argc`       | `common.Argc int` (package-level var)  |
| `static void parse_token()` | `parseToken()` (unexported)            |
| `byte *buf`                 | `buf []byte` (slice, not pointer)      |

Keep the C name in a comment when the renaming is non-obvious:

```go
// Add registers a console command. (tyrquake: Cmd_AddCommand)
func Add(name string, fn Handler) { ... }
```

### Globals

C `extern` globals become package-level Go vars in the package that
**defines** them. Other packages access them via the qualified
identifier (`common.Argc`, not by re-declaring `extern`).

When the C global is a `static`, it stays unexported in Go.

### Function-pointer dispatch tables

C uses arrays of function pointers heavily (the command parser, the
progs VM opcode handlers, the renderer pipeline). Two valid Go shapes:

1. **Method dispatch** when each entry is a method on a common type:

   ```go
   type Handler interface { Handle(args []string) }
   var commands = map[string]Handler{...}
   ```

2. **Plain function-slice** when the entries are stateless:

   ```go
   var pr_opcodes = [...]func(*progs.State){ pr_op_done, pr_op_mul_f, ... }
   ```

Pick whichever stays closer to the upstream `.c` layout for diff-
readability.

### Packed structs (wire formats)

Quake's network packets, BSP file format, and MDL model format all use
packed C structs. Go doesn't have native packed struct support, so:

- Define a Go struct with the **same field order and types**.
- Add a `Marshal(io.Writer)` and `Unmarshal(io.Reader)` method that
  uses `encoding/binary.LittleEndian` explicitly.
- Add a `const SizeOf<Type> = N` matching the on-wire byte count.
- The parity test feeds a known C-side byte sequence and asserts the
  Go round-trip is byte-equal.

```go
type DemoMessageHeader struct {
    Time    float32
    Angles  [3]float32
    Length  int32
}
const SizeOfDemoMessageHeader = 4 + 12 + 4

func (h *DemoMessageHeader) Unmarshal(r io.Reader) error { ... }
func (h *DemoMessageHeader) Marshal(w io.Writer) error { ... }
```

### Memory: zone allocator

tyrquake has its own `Z_Malloc` / `Hunk_Alloc` allocator. The Go port
keeps the same arena shape (`Zone`, `Hunk`) so the engine's allocation
discipline carries over verbatim, but the backing store is a single
pre-allocated `[]byte` per arena instead of a `malloc()` chain.

This is critical for TamaGo: it avoids per-frame heap churn that would
trigger GC pauses inside the 35 Hz main loop.

### setjmp / longjmp

tyrquake uses `setjmp`/`longjmp` for the host error path (`Host_Error`
unwinds to `Host_Frame`). Go doesn't have these. The replacement is
**panic/recover**: `host.Error()` becomes `panic(host.ErrAbortFrame{})`,
and the main loop wraps each tic in `defer recover()`.

The recover handler must reset transient state (the parse buffer, the
client message queue) and continue with the next tic.

### Variadic + printf-style formatting

`Sys_Printf("foo %s\n", str)` becomes
`println(sys.Printf("foo %s\n", str))` -- but ONLY when the format
string is dynamic. For static fixed strings, prefer Go's built-in
formatters and drop the printf detour.

Match C's `%v` (vector) and `%4f` precision quirks: write a
goquake1-specific `fmtv.Sprintf` wrapper in `engine/sys/` if needed.

### Asserts + Sys_Error

C `Sys_Error("foo")` is a fatal exit. Go port:

```go
sys.Error("foo")  // panics with sys.FatalError{Message: "foo"}
```

`assert(cond, "msg")` becomes a build-tag-gated `engine/dbg`
package whose `Assert` is compiled out under `-tags release`.

### File I/O

tyrquake calls `fopen` / `fread` / `Sys_FileOpenRead`. The Go port uses
an `engine/vfs.FS` interface backed by:

- For host harvest tools: `os.DirFS` over the operator's filesystem.
- For bare-metal: an `embedpak.FS` that exposes the embedded `pak0.pak`
  as `fs.FS`.

NO direct `os` package imports inside `engine/`. They go through `vfs`.

### Math

`vec3_t` (3-element float32) becomes `mathlib.Vec3` (a `[3]float32` or
a struct with X/Y/Z fields -- decide once and stick with it; the
ported code references the upstream `vec3_t` heavily).

Math primitives go in `engine/mathlib/`. Use Go's `math` package where
the semantics match; for C-specific quirks (e.g. `Q_sqrt` -- the famous
fast inverse square root), preserve the original algorithm.

## Testing

Each ported module ships with `engine/<module>/<module>_test.go`:

- **Unit tests** covering each exported function, error path, and edge
  case. Coverage target = 100% (the inherited project rule).
- **Parity tests** where a deterministic transform exists -- e.g.
  hash output of a known input matches the tyrquake C output for the
  same input, captured once in a fixture file at `testdata/`.
- **Demo replay** tests once the engine reaches a runnable state: feed
  a recorded `.dem` file from upstream, replay it through the Go
  engine, assert the per-tic state hash matches a golden manifest.

## Engine-vs-wrapper boundary

The `engine/` subtree is GPL-2.0-or-later (inherits from tyrquake).
Everything outside (`backend/`, `embedpak/`, `cmd/`, `internal/`,
`.github/`) is BSD-3-Clause. Don't import `engine/` into a BSD-3 file
unless you're prepared to make that file GPL-compatible.

## Commit hygiene

Each port lands as ONE commit named `port: <c_filename> -> engine/<package>`,
with the commit body documenting:

- The upstream tyrquake commit SHA the source was taken from (so re-
  rebasing on a newer tyrquake is mechanical).
- Any deviations from the upstream behaviour, with the why.
- Parity-test results (e.g. "100% statement coverage; parity vs C on
  N random inputs").

When a module needs to be revised, the commit is `port-fix: <module>:
<short summary>`. Avoid amending; commits are the audit trail.

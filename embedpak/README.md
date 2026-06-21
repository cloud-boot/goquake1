# embedpak

In-binary PAK shipping for `github.com/go-quake1/engine`.

This package uses `//go:embed empty.pak` to bake a PAK archive into
the engine binary. The default `empty.pak` checked into the repository
is a **12-byte placeholder** -- a valid PACK header with zero
directory entries -- so that the package always builds without
shipping any third-party game data.

## Swapping in the real shareware pak0.pak

id Software granted free redistribution of the Episode 1 shareware
`pak0.pak` (palette, colormap, conchars, sprites, sounds, and the
first set of maps -- E1M1..E1M8 plus the start map). The asset blob
itself is **not** in this repository; operators install it locally
by overwriting `empty.pak`:

```sh
# from the engine repo root:
cp /path/to/quake/id1/pak0.pak embedpak/empty.pak
go build ./...
```

After the swap `embedpak.IsEmpty()` returns `false`, `embedpak.OpenAsFS()`
serves the real archive, and `embedpak.AddToVFS(sp)` prepends the
shareware pak to the engine's `vfs.SearchPath`.

`embedpak/empty.pak` is intentionally untracked-as-data: the
repository carries only the 12-byte stub. If you commit a real
`pak0.pak` in its place, **do not push** -- the redistribution grant
permits local use but does not place the file under this
repository's BSD-3 wrapper licence.

## API

| Function                          | Behaviour                                                                                       |
| --------------------------------- | ----------------------------------------------------------------------------------------------- |
| `Bytes() []byte`                  | Returns a copy of the embedded blob.                                                            |
| `IsEmpty() bool`                  | Reports whether the blob is the 12-byte placeholder.                                            |
| `OpenAsFS() (fs.FS, error)`       | Opens the blob via `pak.Open`; returns `ErrEmbedPakEmpty` for the placeholder.                  |
| `AddToVFS(*vfs.SearchPath) error` | One-call helper: opens the blob and prepends it to `sp`; returns `ErrEmbedPakEmpty` if placeholder. |

## Layout of the placeholder

The 12-byte stub is the bare PACK header:

```
offset  bytes  meaning
0       4      "PACK" magic
4       4      directory offset (int32 LE) = 12
8       4      directory length (int32 LE) = 0
```

`pak.Open` accepts the stub but `IsEmpty` short-circuits before it is
opened, so callers can pick the synthetic-asset fallback path without
walking an empty directory.

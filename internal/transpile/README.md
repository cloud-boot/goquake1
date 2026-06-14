# internal/transpile

The reproducible quakeforge -> Go transpile pipeline. Runs in a Tart
debian:13 VM (or any linux/arm64 environment with Go 1.23+ and
`modernc.org/ccgo/v4`).

## Pipeline

```
quakeforge git checkout
    |
    | (1) ./bootstrap.sh && ./configure --enable-static --disable-shared
    |     ...
    |     yields config.h, version.h, and the .c-file-set the build
    |     would otherwise feed to cc
    |
    v
strip platform-specific .c files
    |
    | (2) filter out gl_*, sdl_*, vid_x11, snd_alsa/pulse/jack, etc.
    |     keep only the soft renderer + null I/O + portable engine core
    |
    v
ccgo -DLINUX -I... <stripped-source-set>
    |
    | (3) modernc.org/ccgo/v4 produces ../engine/engine.go (and
    |     friends), pure-Go output that imports modernc.org/libc only.
    |
    v
go build ./engine/...
    |
    | (4) confirms link-clean translation. Any "undefined: X external"
    |     surfaces as a TODO in this directory.
```

## Why a separate VM

ccgo needs the host's libc headers to parse system includes. On
macOS-arm64 (the maintainer's workstation) it dies on Darwin's NEON
struct alignment in `_structs.h`. Docker on Apple Silicon hits emulation
issues (Rosetta lacks AVX2 / x86_64 gcc lacks `-m64`). A native
linux/arm64 Tart VM is the only reliable path. The same approach is
documented for go-virtio bring-up where real validation requires Tart
VMs to unlock vsock / fs / Venus-readback paths.

## Quick start (Tart)

```sh
# host
tart clone ghcr.io/cirruslabs/debian:latest goquake-build
tart run goquake-build --no-graphics &
IP=$(tart ip goquake-build)
# bring the host-side checkout into the VM via ssh+rsync, or sshfs.
rsync -a -e "sshpass -p admin ssh -o StrictHostKeyChecking=no" \
    /Users/me/.../goquake1/ admin@$IP:/work/

# in the VM (or via ssh-exec from the host)
cd /work && bash internal/transpile/run.sh
```

## Files in this directory (planned)

| File          | Purpose                                                       |
|---------------|---------------------------------------------------------------|
| `run.sh`      | End-to-end driver: prep quakeforge + run ccgo + diff `engine/` |
| `Dockerfile`  | Reproducible CI image (linux/arm64 debian:13 + Go + ccgo)      |
| `sources.txt` | Curated list of platform-indep quakeforge .c files to feed ccgo |
| `skip.txt`    | Pattern list of files NOT to transpile (GL, SDL, ALSA, etc.)  |
| `Taskfile.yaml` | pkgx-task targets: `task transpile`, `task verify-clean`      |

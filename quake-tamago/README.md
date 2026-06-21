# quake-tamago

Bare-metal Quake-on-TamaGo binary. Boots in QEMU as `-kernel`,
probes the virtio PCI bus, opens the `go-virtio` gpu / input / sound
drivers, wires them through `backend/virtio/realdev` into the engine's
[backend.Backend](../backend/backend.go) contract, then runs
[runloop.Runner.RunUntilQuit](../runloop/runloop.go).

The whole stack runs without a kernel: TamaGo provides the Go
runtime on x86_64 bare metal; `go-virtio` talks straight to the
virtqueues; the engine itself is pure-Go CGO=0.

## Quick start

```
task qemu-visible          # launch with a GTK window (interactive)
task qemu-headless         # boot headless + grab one screendump to /tmp/quake-frame.ppm
```

Both targets build the ELF first via `task build`. The Taskfile
uses the same toolchain conventions as `go-virtio/validate/run.sh`:

- `TAMAGO=<path-to-tamago/bin/go>` (default
  `~/Documents/VCS/GIT/github.com/tannevaled/tamago-go/bin/go`)
- `-ldflags "-T 0x10010000 -R 0x1000"` for the canonical TamaGo
  memory layout on amd64
- `qemu-system-x86_64 -M q35 -accel tcg -m 512M` plus
  `-device virtio-gpu-pci/virtio-keyboard-pci/virtio-mouse-pci/virtio-snd-pci`

## Architecture

```
main.go                            <- this binary
  ├─ board (PIC mask, IRQ routing)
  ├─ pci.Probe per virtio device
  ├─ gpu.OpenVirtioGPU + SetupFramebuffer
  ├─ input.OpenVirtioInput
  ├─ sound.OpenVirtioSound + stream setup
  ├─ realdev.WrapFramebuffer/WrapInput/WrapAudio
  ├─ virtio.New(...)              <- backend.Backend impl
  ├─ runloop.NewRunnerFromVFS(...)
  └─ runner.RunUntilQuit()
```

The hardest path is the input + sound stream setup; the realdev
adapter assumes those are already negotiated when `WrapInput` /
`WrapAudio` are called (which mirrors how the abstract interfaces
are tested in isolation).

## Asset bundle

The binary embeds a minimal synthetic VFS (palette + colormap +
conchars) for the initial bring-up. Once that works, the full
pak0.pak can be embedded via `embedpak/`.

## Validation

`task qemu-headless` captures one PPM frame via QEMU's `screendump`
monitor command. The frame is the engine's actual rendered output;
the project's autonomous-visual-verification protocol expects this
file to be inspected by a human or compared against a reference
hash.

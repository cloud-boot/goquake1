// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// quake-tamago is the bare-metal Quake-on-TamaGo entry point. It boots
// in QEMU as a `-kernel` ELF, probes the virtio PCI bus for gpu / input /
// sound devices, wires them through backend/virtio/realdev into the
// engine's backend.Backend contract, and drives runloop.Runner.RunUntilQuit.
//
// First-bring-up scope:
//
//   - Synthetic asset VFS (palette + colormap + conchars built in code).
//     The validation target is "a non-blank frame reaches the host
//     scanout", not "Quake plays". A follow-up batch embeds pak0.pak.
//
//   - stubHost satisfies runloop.HostFramer with a no-op per tic. The
//     real id-Software game-server tick (sv_main) wires in a follow-up
//     batch; for now the run loop just renders frames + processes input.
//
//   - Input + sound are best-effort. If virtio-input or virtio-snd are
//     absent from the QEMU command line the engine falls back to a
//     no-input / silent backend rather than panicking.
//
// Rationale (project-driver quote): "on a fait les pilote virtio pour
// eprouver tamago" — the go-virtio drivers exist so this binary can
// exercise the full pure-Go bare-metal stack end-to-end.
package main

import (
	"fmt"
	"io/fs"
	"testing/fstest"

	_ "github.com/go-virtio/validate/board"
	"github.com/go-virtio/validate/transport"

	"github.com/usbarmory/tamago/soc/intel/pci"

	"github.com/go-virtio/common"
	"github.com/go-virtio/gpu"
	"github.com/go-virtio/input"
	"github.com/go-virtio/sound"

	"github.com/go-quake1/engine/assets"
	"github.com/go-quake1/engine/backend/virtio"
	"github.com/go-quake1/engine/backend/virtio/realdev"
	"github.com/go-quake1/engine/client"
	"github.com/go-quake1/engine/render"
	"github.com/go-quake1/engine/runloop"
	engineserver "github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/vfs"
)

// fbWidth / fbHeight are the framebuffer dimensions handed to
// virtio-gpu's SetupFramebuffer. 320x240 matches the go-virtio/validate
// gpuvalidate harness baseline (the legacy 96x72 cap is gone now that
// validate/board masks the 8259 PIC).
const (
	fbWidth  = 320
	fbHeight = 240
)

// stubHost satisfies runloop.HostFramer for first bring-up. The real
// id-Software game-server tick wires in a follow-up batch; for now
// the loop just renders frames + processes input.
type stubHost struct{}

// Frame is a no-op: the server simulation is absent until embedpak
// + sv_main land.
func (stubHost) Frame(_ float32) error { return nil }

func main() {
	if err := run(); err != nil {
		fmt.Printf("QUAKE: FAIL %v\n", err)
		halt()
	}
}

// run is main's testability seam (mirrors the validate harness shape).
// It returns errors instead of halting so the QEMU serial log carries
// the failure reason; main then halts on receipt.
func run() error {
	// 1. Open virtio-gpu via PCI. This is the only mandatory device —
	//    without a framebuffer the engine has nowhere to render.
	gpuDev := pci.Probe(0, common.PCIVendorID, common.PCIDeviceIDModernGPU)
	if gpuDev == nil {
		return fmt.Errorf("no virtio-gpu-pci device found")
	}
	g, err := gpu.OpenVirtioGPU(transport.New(gpuDev))
	if err != nil {
		return fmt.Errorf("OpenVirtioGPU: %w", err)
	}
	fmt.Printf("QUAKE: GPU=%#04x:%#04x scanouts=%d features=%#x\n",
		gpuDev.Vendor, gpuDev.Device, g.NumScanouts, g.NegotiatedFeatures)
	fb, err := g.SetupFramebuffer(0, fbWidth, fbHeight)
	if err != nil {
		return fmt.Errorf("SetupFramebuffer: %w", err)
	}
	fmt.Printf("QUAKE: framebuffer %dx%d resource=%d\n",
		fb.Width, fb.Height, fb.ResourceID)

	// 2. Open virtio-input (best-effort; engine works without keyboard).
	//    virtio-keyboard-pci publishes vendor 0x1AF4 + device 0x1052
	//    (modern-transport virtio-input).
	var inputDev virtio.InputDevice
	inDev := pci.Probe(0, common.PCIVendorID, input.PCIDeviceIDModernInput)
	if inDev != nil {
		vi, err := input.OpenVirtioInput(transport.New(inDev))
		if err != nil {
			return fmt.Errorf("OpenVirtioInput: %w", err)
		}
		inputDev = realdev.WrapInput(vi)
		fmt.Printf("QUAKE: input=%#04x:%#04x\n", inDev.Vendor, inDev.Device)
	} else {
		fmt.Printf("QUAKE: no virtio-input device; engine runs without input\n")
	}

	// 3. Open virtio-snd (best-effort; engine works silent).
	//    virtio-snd-pci publishes vendor 0x1AF4 + device 0x1059
	//    (modern-transport virtio-sound).
	var audioDev virtio.AudioDevice
	sndDev := pci.Probe(0, common.PCIVendorID, sound.PCIDeviceIDModernSound)
	if sndDev != nil {
		vs, err := sound.OpenVirtioSound(transport.New(sndDev))
		if err != nil {
			return fmt.Errorf("OpenVirtioSound: %w", err)
		}
		// streamID 0; the PCMSetParams → PCMPrepare → PCMStart
		// handshake is a follow-up. SampleRate() returns 0 until then,
		// which the realdev wrapper documents as the "not yet
		// negotiated" sentinel.
		audioDev = realdev.WrapAudio(vs, 0)
		fmt.Printf("QUAKE: sound=%#04x:%#04x\n", sndDev.Vendor, sndDev.Device)
	} else {
		fmt.Printf("QUAKE: no virtio-snd device; engine runs silent\n")
	}

	// 4. Build the backend over the three (or fewer) devices. The
	//    Backend wraps the abstract Framebuffer / InputDevice /
	//    AudioDevice trio; nil in / au are accepted (the backend
	//    short-circuits the corresponding paths).
	be, err := virtio.New(realdev.WrapFramebuffer(fb), inputDev, audioDev, nil)
	if err != nil {
		return fmt.Errorf("virtio.New: %w", err)
	}

	// 5. Build a synthetic VFS with the minimum assets LoadStandard
	//    needs (palette + colormap + conchars). Real pak0.pak lives in
	//    a follow-up batch (embedpak/).
	v := vfs.New()
	v.Add(syntheticAssets())

	// 6. Build a loopback NetConn pair. The single-player path serves
	//    both client + server in this process; the engine's runloop
	//    only holds the client-side handle (the server-side handle is
	//    plumbed into the host package when it lands).
	cli, _ := engineserver.NewLoopbackConn()

	// 7. Construct the Runner via NewRunnerFromVFS.
	runner, err := runloop.NewRunnerFromVFS(runloop.SetupOpts{
		VFS:            v,
		Host:           stubHost{},
		Client:         client.NewState(),
		Conn:           cli,
		Backend:        be,
		BackgroundIdx:  0x20, // pleasant grey background from the synthetic palette
		NotifyLifetime: 3,
		MaxNotifyRows:  4,
	})
	if err != nil {
		return fmt.Errorf("NewRunnerFromVFS: %w", err)
	}

	// 8. Print something visible into the console so the rendered
	//    frame is not blank. Drop the console fully open so the lines
	//    are visible from frame 0 (otherwise ConCurrent=0 keeps the
	//    drop-down closed and the synthetic conchars sheet has nothing
	//    to draw against).
	runner.Console.Print("PURE-GO QUAKE 1 -- TamaGo + go-virtio bring-up\n")
	runner.Console.Print("framebuffer wired; per-tick Compose2D + Flush\n")
	runner.Screen.ConCurrent = runner.Screen.ConLines

	// 9. Drive the main loop. Returns when QuitRequested or error.
	fmt.Printf("QUAKE: entering RunUntilQuit\n")
	return runner.RunUntilQuit()
}

// syntheticAssets returns an fs.FS backed by fstest.MapFS that holds
// the three lumps assets.LoadStandard needs. The values are
// deterministic but synthetic — no real id-Software data ships in
// this binary. A follow-up batch swaps the synthetic FS for an
// embedded pak0.pak via embedpak.
//
// Lump contents (mirrors assets_test.go's make*Lump helpers):
//
//   - gfx/palette.lmp  : 768 bytes, 256 RGB triplets where
//     R=i, G=i^0xFF, B=i<<1.  Index 0x20 lands at
//     (0x20, 0xDF, 0x40) — the pleasant grey the
//     BackgroundIdx default points at.
//   - gfx/colormap.lmp : 16384 bytes, identity-mapped sequence
//     (cm[i] = byte(i)). LoadColorMap rejects any
//     other size, but this minimal map is enough
//     for the no-textures bring-up frame.
//   - gfx/conchars.lmp : 16384 bytes (128*128), each cell = byte(i)
//     so the synthetic glyph sheet looks like a
//     repeating gradient — DrawCharacter still
//     finds non-background pixels everywhere, so
//     the printed console lines show up.
func syntheticAssets() fs.FS {
	return fstest.MapFS{
		"gfx/palette.lmp":  &fstest.MapFile{Data: makePaletteLump()},
		"gfx/colormap.lmp": &fstest.MapFile{Data: makeColorMapLump()},
		"gfx/conchars.lmp": &fstest.MapFile{Data: makeConcharsLump()},
	}
}

// makePaletteLump returns a 768-byte synthetic palette. The pattern
// mirrors the assets test fixture so the engine's downstream code
// sees the same shape it does under `go test`.
func makePaletteLump() []byte {
	buf := make([]byte, render.PaletteLumpSize)
	for i := 0; i < 256; i++ {
		buf[i*3+0] = byte(i)
		buf[i*3+1] = byte(i ^ 0xFF)
		buf[i*3+2] = byte(i << 1)
	}
	return buf
}

// makeColorMapLump returns a 16384-byte identity-mapped colormap.
// cm[i] = byte(i) -- the "no lighting" baseline; sufficient for the
// 2D Compose path the first frame exercises.
func makeColorMapLump() []byte {
	buf := make([]byte, render.ColorMapRows*render.ColorMapCols)
	for i := range buf {
		buf[i] = byte(i)
	}
	return buf
}

// makeConcharsLump returns a 16384-byte synthetic 128x128 char sheet.
// Each pixel = byte(i & 0xFF) so the glyph cells contain a gradient
// of palette indices -- DrawCharacter treats non-zero as opaque, so
// every glyph cell paints something onto the framebuffer.
func makeConcharsLump() []byte {
	buf := make([]byte, assets.ConCharsLumpSize)
	for i := range buf {
		buf[i] = byte(i & 0xFF)
	}
	return buf
}

// halt is the tamago "spin forever after a fatal error" primitive.
// The board package's Exit handler also halts; this one covers the
// pre-board failure window + the in-engine return path.
func halt() {
	for {
	}
}

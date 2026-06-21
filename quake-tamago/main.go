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
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

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
	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bspfile/synthbsp"
	"github.com/go-quake1/engine/bsprender"
	"github.com/go-quake1/engine/client"
	"github.com/go-quake1/engine/embedpak"
	"github.com/go-quake1/engine/model"
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

	// 9. Drive a 3D demo loop instead of the (still-empty) RunUntilQuit
	//    path. Renders a rotating textured quad via the perspective
	//    rasterizer + presents each frame through the virtio-gpu
	//    Framebuffer.Flush. Proves the 3D path works end-to-end on
	//    real virtio-gpu hardware.
	fmt.Printf("QUAKE: entering demo3D loop\n")
	return runDemo3D(runner.FrameBuffer, runner.RGBA, runner.Palette, be)
}

// runDemo3D renders a BSP via the full engine pipeline:
// (embedpak | synthbsp) -> bspfile.Open -> model.LoadBrush ->
// bsprender.NewWalkContext + WalkWorld -> bsprender.TransformFace ->
// render.FillTexturedPolygon -> virtio-gpu Flush. Loops forever (the
// bare-metal binary has no clean exit path).
//
// BSP source selection (one-time, at startup):
//
//   - Try embedpak.OpenAsFS(). When the operator has dropped a real
//     pak0.pak into embedpak/empty.pak, the shareware archive is
//     available and we open "maps/start.bsp" (or "maps/e1m1.bsp" as a
//     fallback) out of it.
//   - When the placeholder is still in place (ErrEmbedPakEmpty) OR
//     neither canonical map is present, fall back to
//     synthbsp.BuildWithFaces(): the always-available synthetic BSP
//     that lets the rasterizer path stay exercised without any real
//     id-Software assets.
//
// Per-frame culling strategy:
//
//   - Real BSP (LumpMarksurfaces non-empty): use the canonical PVS
//     walk. PointInLeaf finds the viewer's leaf, MarkVisibleLeaves
//     stamps just the leaves in that leaf's PVS row (+ their parent
//     chains), and WalkWorld emits only those leaves' marksurfaces.
//     start.bsp has ~1865 leaves / 6187 faces; PVS culling typically
//     drops per-frame face emission to ~50-200 (vs 6187 with the
//     "stamp everything" hack, which trips a tamago OOM during the
//     SurfaceList growslice).
//
//   - Synthetic BSP (BuildWithFaces, no marksurfaces lump): keep the
//     "stamp every leaf + emit all faces from the one drawable leaf"
//     demo path. PointInLeaf would return the EMPTY leaf at index 0
//     (no outside sentinel in that fixture), which the spec-PVS path
//     would treat as "outside the map" and skip rendering -- not what
//     the synthbsp demo wants. The synth detection (zero-length
//     marksurfaces lump) routes around this.
func runDemo3D(fb *render.FrameBuffer, rgba []byte, palette *render.Palette, be *virtio.Backend) error {
	// 1. Source the BSP bytes: real pak0.pak first, synthbsp fallback.
	bspBytes, size, err := loadBSP()
	if err != nil {
		return fmt.Errorf("loadBSP: %w", err)
	}
	file, err := bspfile.Open(bytes.NewReader(bspBytes), size)
	if err != nil {
		return fmt.Errorf("bspfile.Open: %w", err)
	}
	bm, err := model.LoadBrush(file, 0)
	if err != nil {
		return fmt.Errorf("model.LoadBrush: %w", err)
	}
	faces, err := file.Faces()
	if err != nil {
		return fmt.Errorf("file.Faces: %w", err)
	}
	marks, _ := file.MarkSurfaces()
	isSynth := len(marks) == 0
	fmt.Printf("QUAKE: BSP loaded -- %d nodes, %d leaves (PVS), %d faces, %d marksurfaces (synth=%v)\n",
		bm.NumNodes(), bm.NumLeaves(), len(faces), len(marks), isSynth)

	// 2. Synthetic 16x16 checker texture. Real miptex decode (using
	//    the BSP's TexInfo -> Textures chain) is a follow-up; this
	//    surface stays visually distinct from the sky-index clear.
	tex := makeCheckerTex(16)

	// 3. Identity colormap: every (light, src) -> src. Lighting is
	//    full-bright in this MVP; the colormap reuse keeps the path
	//    identical to the future per-leaf-lighted version.
	var cm render.ColorMap
	for light := 0; light < render.ColorMapRows; light++ {
		for src := 0; src < render.ColorMapCols; src++ {
			cm[light][src] = byte(src)
		}
	}

	// 4. WalkContext. Real BSPs use NewWalkContext's defaults verbatim
	//    -- the marksurfaces lump drives LeafFaces, the file's bboxes
	//    drive culling. The synthbsp fixture ships none of that, so we
	//    overlay the same demo-only wrappers that worked before.
	walkCtx := bsprender.NewWalkContext(bm)
	if isSynth {
		allFaceIdx := make([]int, len(faces))
		for i := range allFaceIdx {
			allFaceIdx[i] = i
		}
		walkCtx.LeafFaces = func(id int) []int {
			if walkCtx.NodeKind(id) == bsprender.NodeKindLeaf {
				return allFaceIdx
			}
			return nil
		}
		// Promote every EMPTY-contents leaf to NodeKindLeaf -- the
		// synthbsp uses leaf 0 as a drawable leaf (no outside sentinel).
		walkCtx.NodeKind = func(id int) bsprender.NodeKind {
			if id < walkCtx.NumNodes {
				return bsprender.NodeKindInterior
			}
			leafIdx := id - walkCtx.NumNodes
			if leafIdx < 0 || leafIdx >= bm.TotalLeaves() {
				return bsprender.NodeKindEmpty
			}
			if bm.Leaf(leafIdx).Contents == bspfile.ContentsSolid {
				return bsprender.NodeKindEmpty
			}
			return bsprender.NodeKindLeaf
		}
		// The synthbsp ships zero-size node bboxes; widen them so the
		// walker's frustum test always passes for the demo.
		const bigF = float32(1e6)
		walkCtx.NodeBBox = func(id int) (mins, maxs [3]float32) {
			return [3]float32{-bigF, -bigF, -bigF}, [3]float32{bigF, bigF, bigF}
		}
	}

	// 5. Camera setup. For the synthbsp the face verts live at
	//    (0,0,0), (10,0,0), (0,10,0) on the Z=0 plane -- camera at
	//    (5, 5, +20) pitched 90 degrees down keeps the triangle
	//    dead-ahead at depth 20 (>= ParticleNearClip = 16). Yaw spins
	//    the view to animate.
	//
	//    For a real BSP the (5, 5, 20) point is almost certainly
	//    outside any leaf -- PointInLeaf returns leaf 0 (outside
	//    sentinel) and rendering is skipped. To land the camera inside
	//    the map we walk a small spiral of candidate points around the
	//    world model's bbox centre until PointInLeaf returns a non-zero
	//    leaf; that point becomes the viewpoint.
	const fovX = 90.0
	camOrigin := [3]float32{5, 5, 20}
	if !isSynth {
		camOrigin = pickInMapCamera(bm, file)
		fmt.Printf("QUAKE: camera origin %v\n", camOrigin)
	}
	markCtx := bsprender.NewMarkContext(bm)
	var surfaces bsprender.SurfaceList

	for frame := 0; ; frame++ {
		// Clear to sky background (palette idx 0x10).
		for i := range fb.Pixels {
			fb.Pixels[i] = 0x10
		}

		// Camera fixed at (5, 5, +20); yaw spins to animate.
		yawDeg := float32(frame) * 1.0
		rd := &render.RefDef{
			VRect:      render.VRect{Width: fb.Width, Height: fb.Height},
			ViewAngles: [3]float32{90, yawDeg, 0},
			ViewOrigin: camOrigin,
			FovX:       fovX,
			FovY:       fovX,
		}
		view := rd.SetupView()
		frustum := rd.BuildFrustum()
		stampFrame := int32(frame + 1)

		if isSynth {
			// Synth: no PVS, mark everything so WalkWorld visits the
			// single drawable leaf + emits the all-faces list.
			for n := 0; n < bm.NumNodes(); n++ {
				bm.SetNodeVisFrame(n, stampFrame)
			}
			for l := 0; l < bm.TotalLeaves(); l++ {
				bm.Leaf(l).VisFrame = stampFrame
			}
		} else {
			// Real BSP: locate the viewer's leaf, then stamp only the
			// PVS-visible leaves + their parent chains. Out-of-map
			// viewer (PointInLeaf returns <= 0) -> nothing to draw;
			// present the cleared background and move on.
			viewerLeaf := bm.PointInLeaf(rd.ViewOrigin)
			if viewerLeaf > 0 {
				if err := bsprender.MarkVisibleLeaves(markCtx,
					bsprender.VisLeafIdx(viewerLeaf),
					bsprender.FrameMarkSequence(stampFrame),
				); err != nil {
					return fmt.Errorf("MarkVisibleLeaves: %w", err)
				}
			} else {
				_ = fb.Expand(rgba, palette)
				_ = be.PresentFrame(rgba, fb.Width, fb.Height)
				continue
			}
		}

		surfaces.Reset()
		if err := bsprender.WalkWorld(walkCtx, 0, rd.ViewOrigin, frustum, stampFrame, &surfaces); err != nil {
			return fmt.Errorf("WalkWorld: %w", err)
		}

		// Per-frame face count log (sparse, every 60 frames) so the
		// serial log surfaces PVS culling effectiveness without
		// drowning the channel.
		if frame%60 == 0 {
			fmt.Printf("QUAKE: frame %d -- %d surfaces emitted\n", frame, surfaces.Len())
		}

		// Rasterize each visible face via TransformFace + FillTexturedPolygon.
		for i := 0; i < surfaces.Len(); i++ {
			ref := surfaces.Refs[i]
			fv, err := bsprender.NewBrushFaceVerts(bm, ref.FaceIdx)
			if err != nil {
				continue
			}
			verts, err := bsprender.TransformFace(view, fb, fovX, fv)
			if err != nil {
				continue
			}
			_ = render.FillTexturedPolygon(fb, tex, &cm, 0, verts)
		}

		// Per-frame runtime.GC() is no longer needed: bspfile.File
		// now caches the decoded Vertexes / Edges / Surfedges /
		// TexInfos / Textures lumps on first call, so
		// NewBrushFaceVerts allocates nothing per face on the hot
		// path. The 2 GB micro-VM stays well below the heap ceiling.

		_ = fb.Expand(rgba, palette)
		_ = be.PresentFrame(rgba, fb.Width, fb.Height)
	}
}

// pickInMapCamera returns a viewpoint that lands inside a valid leaf
// of bm. It starts from the world model's bbox centre (the most
// natural "centre of the map" the BSP carries on disk) and, if that
// point is in the outside-leaf sentinel, walks a 9x9x9 lattice of
// jittered candidates within the bbox until PointInLeaf returns a
// non-zero leaf index. Falls back to the bbox centre verbatim if every
// candidate is solid -- the per-frame PointInLeaf check then skips
// rendering rather than crashing.
//
// The lattice is coarse on purpose: with start.bsp's ~3000-unit bbox
// and a 9-step lattice we sample every ~375 units, which is well
// inside any playable Quake corridor.
func pickInMapCamera(bm *model.BrushModel, file *bspfile.File) [3]float32 {
	models, err := file.Models()
	if err != nil || len(models) == 0 {
		return [3]float32{0, 0, 0}
	}
	m := &models[0]
	centre := [3]float32{
		(m.Mins[0] + m.Maxs[0]) * 0.5,
		(m.Mins[1] + m.Maxs[1]) * 0.5,
		(m.Mins[2] + m.Maxs[2]) * 0.5,
	}
	if leaf := bm.PointInLeaf(centre); leaf > 0 {
		return centre
	}
	const steps = 9
	for ix := 0; ix < steps; ix++ {
		for iy := 0; iy < steps; iy++ {
			for iz := 0; iz < steps; iz++ {
				p := [3]float32{
					m.Mins[0] + (m.Maxs[0]-m.Mins[0])*float32(ix+1)/float32(steps+1),
					m.Mins[1] + (m.Maxs[1]-m.Mins[1])*float32(iy+1)/float32(steps+1),
					m.Mins[2] + (m.Maxs[2]-m.Mins[2])*float32(iz+1)/float32(steps+1),
				}
				if leaf := bm.PointInLeaf(p); leaf > 0 {
					return p
				}
			}
		}
	}
	return centre
}

// loadBSP returns the BSP bytes + size to render. It walks the
// preferred sources in order:
//
//  1. embedpak.OpenAsFS() — when a real pak0.pak has been dropped
//     into embedpak/empty.pak, try "maps/start.bsp" (canonical entry
//     map) then "maps/e1m1.bsp" (episode 1 first map).
//  2. synthbsp.BuildWithFaces() — the always-available synthetic
//     fallback. Used when the embedded pak is still the placeholder
//     OR when neither canonical map is present in the supplied pak.
//
// The chosen path is logged on the serial console so the QEMU log
// makes the source unambiguous.
func loadBSP() ([]byte, int64, error) {
	pakFS, err := embedpak.OpenAsFS()
	if err == nil {
		for _, mapName := range []string{"maps/start.bsp", "maps/e1m1.bsp"} {
			data, ok := tryReadPakFile(pakFS, mapName)
			if ok {
				fmt.Printf("QUAKE: loaded %s from embedded pak0.pak (%d bytes)\n",
					mapName, len(data))
				return data, int64(len(data)), nil
			}
		}
		fmt.Printf("QUAKE: embedded pak0.pak lacks maps/start.bsp and maps/e1m1.bsp; using synthbsp fallback\n")
	} else if errors.Is(err, embedpak.ErrEmbedPakEmpty) {
		fmt.Printf("QUAKE: using synthbsp fallback (drop real pak0.pak into embedpak/empty.pak)\n")
	} else {
		fmt.Printf("QUAKE: embedpak.OpenAsFS failed (%v); using synthbsp fallback\n", err)
	}
	return synthbsp.BuildWithFaces()
}

// tryReadPakFile opens name inside pakFS and returns its contents.
// Reports (nil, false) when the entry is missing or unreadable so the
// caller can probe the next candidate map without having to classify
// the error.
func tryReadPakFile(pakFS fs.FS, name string) ([]byte, bool) {
	f, err := pakFS.Open(name)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, false
	}
	return data, true
}

// makeCheckerTex returns an NxN texture with a 4-colour checker
// pattern (palette indices cycling through 0, 15, 31, 47 by tile).
// Used as the stand-in surface texture for every BSP face until the
// proper miptex chain (TexInfo -> Textures lump -> miptex pixels) is
// wired in.
func makeCheckerTex(n int) *render.Pic {
	pixels := make([]byte, n*n)
	colors := [4]byte{0, 15, 31, 47}
	tile := n / 4
	if tile < 1 {
		tile = 1
	}
	for v := 0; v < n; v++ {
		for u := 0; u < n; u++ {
			idx := ((u / tile) + (v/tile)*2) & 3
			pixels[v*n+u] = colors[idx]
		}
	}
	return &render.Pic{Width: n, Height: n, Pixels: pixels}
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
	return memFS{
		"gfx/palette.lmp":  makePaletteLump(),
		"gfx/colormap.lmp": makeColorMapLump(),
		"gfx/conchars.lmp": makeConcharsLump(),
	}
}

// memFS is a minimal in-memory fs.FS used in place of testing/fstest.MapFS.
// The testing package's init() pulls in signal handling + runtime metrics
// that don't link cleanly on bare-metal tamago; this hand-rolled
// equivalent stays runtime-free.
type memFS map[string][]byte

func (m memFS) Open(name string) (fs.File, error) {
	data, ok := m[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{name: name, data: data}, nil
}

type memFile struct {
	name string
	data []byte
	pos  int
}

func (f *memFile) Read(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func (f *memFile) Stat() (fs.FileInfo, error) {
	return &memFileInfo{name: f.name, size: int64(len(f.data))}, nil
}

func (f *memFile) Close() error { return nil }

type memFileInfo struct {
	name string
	size int64
}

func (i *memFileInfo) Name() string       { return i.name }
func (i *memFileInfo) Size() int64        { return i.size }
func (i *memFileInfo) Mode() fs.FileMode  { return 0o444 }
func (i *memFileInfo) ModTime() time.Time { return time.Time{} }
func (i *memFileInfo) IsDir() bool        { return false }
func (i *memFileInfo) Sys() any           { return nil }

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

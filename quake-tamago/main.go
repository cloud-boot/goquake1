// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// quake-tamago is the bare-metal Quake-on-TamaGo entry point. It boots
// in QEMU as a `-kernel` ELF, probes the virtio PCI bus for gpu / input /
// sound devices, wires them through backend/virtio/realdev into the
// engine's backend.Backend contract, and drives the runloop end-to-end:
// virtio-input -> client.Tick (clc_move) -> host.Frame (SV_Physics) ->
// Pre2DDraw (BSP walk + rasterize) -> Compose2D (console + notify) ->
// virtio-gpu PresentFrame, all from a single runloop.Runner.RunUntilQuit.
//
// First-bring-up scope:
//
//   - Asset VFS overlays the real pak (palette + colormap from
//     gfx/palette.lmp + gfx/colormap.lmp) on top of a synthetic
//     fallback (palette + colormap + conchars built in code). The
//     real pak takes precedence per-lump; lumps the pak lacks (the
//     Quake Remastered archive ships no gfx/conchars.lmp) fall
//     through to the synthetic copy. The BSP and progs.dat are
//     loaded out of the same embedded pak via embedpak.OpenAsFS.
//
//   - The real host.Host is constructed when embedpak.OpenAsFS yields a
//     non-placeholder pak: progs.Load + progs.NewVM + model.NewCache +
//     a pak-backed FileResolver + host.NewHost(..., 1 client). The
//     host's SpawnServer loads "maps/start.bsp", parses entities,
//     populates the edict pool. Per-tic the runloop calls
//     host.Frame(dt) -- this drives SV_Physics + SendClientFrames over
//     a 20Hz tic. The runner's Host field is wired to the real host
//     (no more stub bypass); RunUntilQuit drives the full pipeline.
//
//   - On the placeholder-pak path (no real pak0 present) the engine
//     falls back to the stubHost no-op + synthbsp rendering, matching
//     the previous bring-up behaviour. This keeps the binary boot-safe
//     in CI environments where the shareware archive is absent.
//
//   - Input + sound are best-effort. If virtio-input or virtio-snd are
//     absent from the QEMU command line the engine falls back to a
//     no-input / silent backend rather than panicking.
//
//   - Camera position follows the local player entity slot via the
//     wire-mirrored client.State.Entities map (proper client/server
//     split: the renderer reads what the server told the client over
//     svc_update, NOT the server edict pool directly). The runloop
//     looks up State.Entities[Client.PlayerNum].Origin per-tic and
//     hands the result to Pre2DDraw as viewOrigin; the Pre2DDraw
//     closure layers the client's ViewHeightOffset eye-height nudge
//     on top. When the player entity has not yet been received
//     (pre-signon, or the wire drain has not delivered the first
//     svc_update for the local slot) the runloop's fallback is the
//     zero vector; the closure detects that case + substitutes
//     pickInMapCamera's lattice anchor so the renderer always has a
//     valid leaf to walk against. Camera angles are driven by
//     virtio-input via client.Tick (the WASD + mouse + jump bindings
//     already in UpdateButtonsFromSnapshot).
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
	enginehost "github.com/go-quake1/engine/host"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/render"
	"github.com/go-quake1/engine/runloop"
	engineserver "github.com/go-quake1/engine/server"
	enginesound "github.com/go-quake1/engine/sound"
	"github.com/go-quake1/engine/vfs"
)

// fbWidth / fbHeight are the framebuffer dimensions handed to
// virtio-gpu's SetupFramebuffer. 1280x1024 is the boot resolution;
// QEMU GTK/Cocoa display is resizable so the host window scales the
// scanout buffer up or down freely.
const (
	fbWidth  = 1280
	fbHeight = 1024
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

	// 5. Open the embedded pak once. Shared between loadBSP (renderer),
	//    the host's FileResolver (server-side worldmodel + miptex bytes
	//    by name) AND the asset vfs (palette/colormap/conchars). nil
	//    means the placeholder is still installed; the renderer falls
	//    back to synthbsp + the host stays stubbed.
	pakFS, pakErr := embedpak.OpenAsFS()
	if pakErr != nil {
		fmt.Printf("QUAKE: embedpak.OpenAsFS failed (%v); host stays stubbed\n", pakErr)
	}

	// 6. Build the asset VFS as an ordered overlay: synthetic fallback
	//    first, real pak last. vfs.SearchPath.Add prepends to the probe
	//    chain, so the LAST Add wins -- the real pak's gfx/palette.lmp
	//    (768 real id-Software bytes) takes precedence over the
	//    deterministic synthetic palette, ditto for gfx/colormap.lmp.
	//    The Quake Remastered pak ships palette + colormap but not
	//    gfx/conchars.lmp; for that key the probe falls through to the
	//    synthetic glyph sheet. assets.LoadStandard inside
	//    NewRunnerFromVFS then sees the real bytes for the lumps the
	//    pak provides + the synthetic bytes for the ones it doesn't.
	v := vfs.New()
	syn := syntheticAssets()
	v.Add(syn) // fallback layer (prepended -> ends up last in probe order)
	if pakFS != nil {
		v.Add(pakFS) // real pak (prepended -> first in probe order)
	}
	reportLumpSources(v, pakFS, syn, []string{
		"gfx/palette.lmp",
		"gfx/colormap.lmp",
		"gfx/conchars.lmp",
	})

	// 7. Build the real Host when a real pak is available. Failures
	//    here log + fall back to stubHost so the binary still boots +
	//    renders something even if progs.dat is malformed / missing /
	//    the BSP can't be located inside the pak.
	var realHost *enginehost.Host
	if pakFS != nil {
		h, herr := buildHost(pakFS, "start")
		if herr != nil {
			fmt.Printf("QUAKE: buildHost failed (%v); host stays stubbed\n", herr)
		} else {
			realHost = h
			fmt.Printf("QUAKE: real host live -- sv.active=%v numEdicts=%d maxEdicts=%d\n",
				h.Server.Active, h.Server.NumEdicts, h.Server.MaxEdicts)
			// SpawnFn-driven entity census: numEdicts > MaxClients+1
			// proves the entity-spawn pass walked beyond the reserved
			// client slots (worldspawn + MaxClients player slots).
			// On id1/start.bsp the ~80-entry entity lump bumps
			// numEdicts well past 2.
			fmt.Printf("QUAKE: spawn census -- %d edicts populated past the world+client reserve (MaxClients=%d)\n",
				h.Server.NumEdicts-h.Static.MaxClients-1, h.Static.MaxClients)
		}
	}

	// 8. Build a loopback NetConn pair. The single-player path serves
	//    both client + server in this process; when a real host is wired
	//    we route the pair through Host.ConnectLoopback so the server-
	//    side handle is bound to a Static.Clients slot (active+spawned),
	//    which is what server.ReadClientMoves consumes per-tic.
	//
	//    Without a real host (stubHost path) ConnectLoopback can't run --
	//    there's no Static.Clients pool to bind into -- so we fall back
	//    to a bare loopback whose server side is silently dropped.
	var cli engineserver.NetConn
	playerSlot := 0 // 1-based edict index of the local player; 0 = no host
	if realHost != nil {
		conn, slotIdx, cerr := realHost.ConnectLoopback()
		if cerr != nil {
			return fmt.Errorf("ConnectLoopback: %w", cerr)
		}
		cli = conn
		// The host's Static.Clients[slotIdx] is now bound; its Edict
		// lives at Server.Edicts[slotIdx+1]. The single-player loop
		// expects slot 0 -> edict 1.
		playerSlot = slotIdx + 1
		// Queue the wire signon prefix (svc_serverinfo + signonnum 1/2/3)
		// into the server-side client Message buffer. The first per-tic
		// FlushClientMessage drains these bytes through the loopback
		// NetConn; client.Tick's SvcReader parses each pair and
		// applySignonNum walks the client into StateConnecting on stage 1.
		//
		// Stage 4 is INTENTIONALLY not queued here -- it now flows via
		// the wire as a side-effect of the client's "spawn" clc_stringcmd:
		// client.Tick sees StateConnecting + emits clc_stringcmd "spawn"
		// once; server.ReadClientMoves -> ParseClcStringCmd flips
		// hc.Spawned + queues svc_signonnum(4) onto hc.Message; the next
		// per-tic flush delivers stage 4 to the client + Apply transitions
		// to StateConnected. End-to-end wire-driven, no manual poke.
		hc := realHost.Static.Clients[slotIdx]
		info := engineserver.ServerInfo{
			Protocol:      protocol.VersionNQ,
			MaxClients:    realHost.Static.MaxClients,
			GameType:      engineserver.GameTypeCoop,
			LevelName:     "the slipgate complex",
			ModelPrecache: realHost.Server.ModelPrecache,
			SoundPrecache: realHost.Server.SoundPrecache,
		}
		if err := engineserver.SendServerInfo(hc, info); err != nil {
			return fmt.Errorf("SendServerInfo: %w", err)
		}
		// Queue svc_setview to tell the client which entity slot to
		// follow as the local view. The C upstream emits this from
		// SV_SendServerinfo right after the precache lists; the Go
		// SendServerInfo deliberately omits it (per its docstring) so
		// the per-client lifecycle code can pick the slot. Without
		// this byte the client's applyServerInfo zeroes PlayerNum (so
		// the wire-mirrored runloop's viewOrigin lookup at
		// State.Entities[PlayerNum] would miss every tic + fall back
		// to the zero vector). svc_setview restores PlayerNum to the
		// player edict slot the loopback bound us to, completing the
		// proper client/server split for the camera-anchor path.
		if err := engineserver.EncodeSetView(hc.Message, playerSlot); err != nil {
			return fmt.Errorf("EncodeSetView: %w", err)
		}
		// Append per-entity baselines (svc_spawnbaseline) onto the same
		// client.Message buffer. Baselines are queued up front (no
		// per-tic gating); the client's applyBaseline arm caches them
		// into State.Baselines as soon as the first Tick drains the
		// queue. Pre-stage-4 placement keeps the cache populated by
		// the time MarkSpawned fires.
		blStat, err := realHost.Server.SendBaselines(hc, realHost.Progs(), realHost.Static.MaxClients)
		if err != nil {
			return fmt.Errorf("SendBaselines: %w", err)
		}
		fmt.Printf("QUAKE: svc_spawnbaseline broadcast -- emitted=%d skipped_free=%d skipped_nomodel=%d (out of %d edicts; total queued bytes=%d)\n",
			blStat.Emitted, blStat.SkippedFree, blStat.SkippedNoModel,
			realHost.Server.NumEdicts, hc.Message.Len())
		fmt.Printf("QUAKE: loopback bound -- client slot %d -> edict %d (active, awaiting wire 'spawn'); wire signon prefix queued (%d bytes)\n",
			slotIdx, playerSlot, hc.Message.Len())
	} else {
		cli, _ = engineserver.NewLoopbackConn()
	}

	// 9. Build the client state. Connection stays at StateDisconnected;
	//    the wire signon bytes queued above drive the lifecycle
	//    transition client-side via applySignonNum stages 1..4 on the
	//    first per-tic client.Tick inbound drain. PlayerNum is re-set
	//    AFTER the runner exists (in step 12-equivalent) because
	//    applyServerInfo zeroes it when the queued svc_serverinfo
	//    bytes are parsed. For now the pre-tic value just makes the
	//    pre-first-frame log line non-zero.
	clientState := client.NewState()
	if realHost != nil {
		clientState.PlayerNum = playerSlot
		fmt.Printf("QUAKE: client state initialised StateDisconnected -- wire signon drives the lifecycle (PlayerNum=%d)\n",
			playerSlot)
	}

	// 9b. Pick the HostFramer the runner drives per-tic. When the real
	//     host built successfully it goes straight in; otherwise the
	//     stub keeps RunFrame's host.Frame call infallible.
	var hostFramer runloop.HostFramer = stubHost{}
	if realHost != nil {
		hostFramer = realHost
	}

	// 10. Construct the Runner via NewRunnerFromVFS. The runner now
	//     drives the FULL per-tic sequence (PollInput -> client.Tick
	//     -> host.Frame -> Pre2DDraw -> Compose2D -> PresentFrame);
	//     the renderer setup below wires its Pre2DDraw hook.
	runner, err := runloop.NewRunnerFromVFS(runloop.SetupOpts{
		VFS:            v,
		Host:           hostFramer,
		Client:         clientState,
		Conn:           cli,
		Backend:        be,
		BackgroundIdx:  0x20, // pleasant grey background from the synthetic palette
		NotifyLifetime: 3,
		MaxNotifyRows:  4,
		SoundChannels:  8, // ambient slots for the runloop's Paint/QueueAudio path
	})
	if err != nil {
		return fmt.Errorf("NewRunnerFromVFS: %w", err)
	}

	// 11. Print something visible into the console so the rendered
	//     frame is not blank. Drop the console fully open so the lines
	//     are visible from frame 0 (otherwise ConCurrent=0 keeps the
	//     drop-down closed and the synthetic conchars sheet has nothing
	//     to draw against).
	runner.Console.Print("PURE-GO QUAKE 1 -- TamaGo + go-virtio bring-up\n")
	runner.Console.Print("runloop wired: input -> client.Tick -> host.Frame -> Pre2DDraw\n")
	runner.Screen.ConCurrent = runner.Screen.ConLines

	// 11b. Seed the sound pool with a few WAV samples from the pak so
	//     the runloop's existing Paint + QueueAudio path has something
	//     to mix every tic. With sound.Paint's 16-bit path now wired
	//     (SND_PaintChannelFrom16 equivalent), the mixer accepts the
	//     stock id1 16-bit ambient/weapon/items WAVs alongside the
	//     8-bit nav_editor one-shots; the seed set below mixes a
	//     16-bit ambient track + an 8-bit one-shot to exercise BOTH
	//     paint paths in the same Paint call (Pool dispatches on
	//     ch.Sfx.BitsPerSam per channel).
	if pakFS != nil && runner.SoundPool != nil {
		seeded := seedSoundPool(runner.SoundPool, pakFS, []string{
			"sound/ambience/water1.wav",          // 16-bit (proves 16-bit playback)
			"sound/nav_editor/changed_edict.wav", // 8-bit (regression guard)
		})
		fmt.Printf("QUAKE: sound pool seeded -- %d sample(s) playing on reserved-static slots\n", seeded)
	}

	// 12. Build the Pre2DDraw hook (BSP load, mark/walk contexts,
	//     synthetic texture, identity colormap) + anchor the camera
	//     origin at pickInMapCamera. The closure is wired onto the
	//     runner; RunUntilQuit then drives the full pipeline. When a
	//     real host exists we pass it in so the per-frame camera can
	//     follow the player edict at slot 1; nil falls back to the
	//     static pickInMapCamera anchor.
	if err := setupRenderer(runner, pakFS, realHost, playerSlot); err != nil {
		return fmt.Errorf("setupRenderer: %w", err)
	}

	fmt.Printf("QUAKE: entering RunUntilQuit (realHost=%v)\n", realHost != nil)
	return runner.RunUntilQuit()
}

// setupRenderer loads the BSP, builds the mark/walk contexts +
// synthetic texture + identity colormap, anchors the camera origin,
// and installs runner.Pre2DDraw as a closure that rasterizes one
// frame of the visible BSP for each call.
//
// Per-frame culling (real vs synth BSP) and the camera anchor logic
// mirror the legacy runDemo3D this replaces; the difference is that
// the runner drives the call instead of an inline forever-loop, so
// host.Frame + client.Tick + Compose2D + PresentFrame are all wired
// into the same per-tic schedule.
//
// CAMERA: the per-frame Pre2DDraw closure receives viewOrigin already
// sourced by the runloop from the wire-mirrored client state at
// runner.Client.Entities[Client.PlayerNum].Origin (the proper client/
// server split: the renderer reads what the server told the client
// via svc_update, NOT the server edict pool directly). The closure
// layers a ViewHeightOffset eye-height nudge on top. When the runloop
// hands a zero vector (no State.Entities entry yet -- pre-signon or
// pre-first-svc_update) the closure falls back to camOrigin -- the
// static pickInMapCamera anchor that's guaranteed to land inside a
// valid leaf.
//
// PLAYER MOVEMENT: each tic the closure first drains the loopback
// server-side queue via server.ReadClientMoves -- consuming whatever
// clc_move the runner's client.Tick step just sent -- then applies the
// resulting UserCmd to the player edict's "origin" field via a minimal
// in-line integrator (forward/right basis from viewangles * forwardMove/
// sideMove * dt). This bypasses the full SV_Physics_Walk + worldmodel
// trace stack on purpose: the goal of this bring-up is "key press moves
// the camera", not "physically correct movement". A follow-up batch
// swaps the integrator for the real SV_Physics tick.
func setupRenderer(runner *runloop.Runner, pakFS fs.FS, realHost *enginehost.Host, playerSlot int) error {
	// 1. Source the BSP bytes: real pak0.pak first, synthbsp fallback.
	bspBytes, size, err := loadBSP(pakFS)
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

	// 2. Synthetic 16x16 checker texture stays available as the
	//    fallback for faces whose TexInfo points at a missing miptex
	//    slot (offset == -1) or an out-of-range texinfo.
	fallbackTex := makeCheckerTex(16)

	// 2b. Real WAD/miptex bridge: pull every miptex's mip0 pixels out
	//     of the BSP's LUMP_TEXTURES and stash one *render.Pic per
	//     slot. Per-face texture pick happens inside Pre2DDraw via
	//     BrushModel.FaceMipTexIdx. The pixels are palette-indexed in
	//     the BSP's own (id1) palette; the asset VFS now serves the
	//     real gfx/palette.lmp out of the embedded pak, so the
	//     destination RGBA the renderer emits is in true id1 colours.
	miptexPics, loaded, total, err := loadMiptexPics(file)
	if err != nil {
		return fmt.Errorf("loadMiptexPics: %w", err)
	}
	fmt.Printf("QUAKE: loaded %d miptexes from BSP (total slots: %d, loaded: %d, null: %d)\n",
		loaded, total, loaded, total-loaded)

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

	// 5. Camera anchor. Synthbsp wants (5,5,20) so the triangle on
	//    Z=0 stays in front of the camera; real BSP wants an in-map
	//    point so PointInLeaf returns a non-zero leaf.
	const fovX = 90.0
	camOrigin := [3]float32{5, 5, 20}
	if !isSynth {
		camOrigin = pickInMapCamera(bm, file)
		fmt.Printf("QUAKE: camera origin %v\n", camOrigin)
	}
	runner.ViewOrigin = camOrigin

	markCtx := bsprender.NewMarkContext(bm)
	var surfaces bsprender.SurfaceList
	frameCount := 0
	// loggedWireSpawn latches the one-shot "server flipped Spawned via
	// wire 'spawn' clc_stringcmd" trace so the line appears at most
	// once per process lifetime. Pre-batch-73 the flip happened via a
	// manual hc.Spawned = true poke in run(); the closure now watches
	// the server-side client struct each tic + logs the transition.
	loggedWireSpawn := false

	// Seed the player edict so the host's per-tic RunPhysics actually
	// dispatches PhysicsWalk for slot 1:
	//
	//   - origin -- pickInMapCamera anchor when the QC spawn pass left
	//     it at the bytecode zero vector (info_player_start isn't being
	//     applied by SpawnFn yet). PhysicsWalk-from-the-world-origin
	//     would trap the player inside a solid leaf forever.
	//   - movetype = MOVETYPE_WALK + solid = SOLID_SLIDEBOX -- without
	//     these the RunPhysics dispatcher hits its (movetype==None &&
	//     solid==Not) free-entity skip and PhysicsWalk never runs.
	//   - mins/maxs = standard Quake hull-1 player bounds (-16,-16,-24
	//     .. 16,16,32). PhysicsWalk's PushEntity trace needs a real bbox
	//     so the world collision actually clips against the BSP brushes.
	//   - velocity / v_angle / flags / gravity zeroed so the first tic
	//     starts from a known rest state; PhysicsWalk's CheckBottom will
	//     latch FL_ONGROUND once the gravity pull settles the player
	//     onto the floor.
	//
	// The full QC PutClientInServer would set additional fields
	// (health, model, weapon, ...); we skip those -- they're not read
	// by PhysicsWalk and the rendering path takes the origin from
	// EdictOrigin directly.
	if realHost != nil && playerSlot > 0 && !isSynth {
		if eo, err := realHost.EdictOrigin(playerSlot); err == nil {
			if eo[0] == 0 && eo[1] == 0 && eo[2] == 0 {
				_ = writePlayerOrigin(realHost, playerSlot, camOrigin)
				fmt.Printf("QUAKE: seeded player edict %d origin = %v (was zero)\n",
					playerSlot, camOrigin)
			}
		}
		if err := initPlayerForPhysicsWalk(realHost, playerSlot); err != nil {
			fmt.Printf("QUAKE: initPlayerForPhysicsWalk(%d) failed: %v -- PhysicsWalk may not dispatch\n",
				playerSlot, err)
		} else {
			fmt.Printf("QUAKE: player edict %d primed for PhysicsWalk (movetype=Walk solid=SlideBox hull1 mins/maxs)\n",
				playerSlot)
		}
		// Flip Free=false on the player edict. SpawnServer's
		// arena.Reset starts every slot with Free=true; the entity-
		// spawn pass flips Free for parsed entities, but the reserved
		// client slots (1..MaxClients) are skipped by that loop. The
		// per-tic SendEntityUpdates filter (`if e == nil || e.Free`)
		// would then skip the player slot every tic, leaving
		// State.Entities[PlayerNum] unpopulated -- exactly the gap
		// the wire-mirrored viewOrigin lookup needs filled. The flip
		// here aligns the client slot with the same "claimed" semantics
		// the upstream SV_ConnectClient applies via ED_Alloc.
		if playerSlot < len(realHost.Server.Edicts) {
			if pe := realHost.Server.Edicts[playerSlot]; pe != nil && pe.Free {
				pe.Free = false
				fmt.Printf("QUAKE: claimed player edict %d (Free=true -> false) so per-tic svc_update emits it\n",
					playerSlot)
			}
		}
	}

	// 6. Pre2DDraw closure. Runs per-tic from RunFrame BEFORE the 2D
	//    Compose; viewAngles is the (pitch, yaw, roll) the client tick
	//    has just refreshed from mouse + arrow keys. viewOrigin is the
	//    runloop's wire-mirrored player anchor sourced from
	//    runner.Client.Entities[Client.PlayerNum].Origin -- the
	//    snapshot the server broadcast via svc_update + the client
	//    cached into State.Entities. The closure offsets it by
	//    client.State.ViewHeightOffset so jumping/crouching still
	//    nudges the camera.
	//
	//    Player physics is owned by host.Frame (called BEFORE
	//    Pre2DDraw by the runloop): host.runClientCmds drains each
	//    loopback inbox + mirrors cmd.ViewAngles into edict.v_angle,
	//    then RunPhysics dispatches PhysicsWalk which integrates
	//    gravity + accelerate + PushEntity-traces against the world.
	//    The post-physics origin is then transmitted to the client
	//    via svc_update + lands in State.Entities[PlayerNum].Origin,
	//    which the runloop hands to Pre2DDraw as viewOrigin -- the
	//    proper client/server split, no more EdictOrigin reach-around
	//    into the server-side edict pool.
	//
	//    Fallback: when the runloop hands a zero vector (no
	//    State.Entities[PlayerNum] entry yet -- pre-signon or pre-
	//    first-svc_update) the closure substitutes camOrigin -- the
	//    static pickInMapCamera anchor that always lands inside a
	//    valid leaf -- so the BSP walk has something to walk against.
	runner.Pre2DDraw = func(fb *render.FrameBuffer, viewOrigin, viewAngles [3]float32) error {
		frame := frameCount
		frameCount++

		// Clear to sky background (palette idx 0x10). Compose2D is
		// told to SkipBackgroundFill (via the Runner.Pre2DDraw != nil
		// branch in RunFrame), so the 3D pixels we write here survive
		// past the 2D overlay.
		for i := range fb.Pixels {
			fb.Pixels[i] = 0x10
		}

		// Pick the camera anchor for this frame. The runloop has
		// already sourced viewOrigin from the wire-mirrored
		// runner.Client.Entities[Client.PlayerNum].Origin (proper
		// client/server split). A zero vector means the wire has not
		// yet delivered the player's svc_update; fall back to
		// camOrigin -- the static pickInMapCamera anchor that always
		// lands inside a leaf -- so the BSP walk doesn't run against
		// the world origin (which sits in a solid leaf).
		origin := viewOrigin
		fromEntities := true
		if origin[0] == 0 && origin[1] == 0 && origin[2] == 0 {
			origin = camOrigin
			fromEntities = false
		}

		// Bias the anchor by the client's view-height offset (the
		// vertical bob/crouch nudge). When the origin comes from the
		// wire-mirrored entity state the offset is the eye-height
		// delta above the entity's base origin.
		origin[2] += runner.Client.ViewHeightOffset

		rd := &render.RefDef{
			VRect:      render.VRect{Width: fb.Width, Height: fb.Height},
			ViewAngles: viewAngles,
			ViewOrigin: origin,
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
			// viewer (PointInLeaf returns <= 0) -> nothing to draw.
			viewerLeaf := bm.PointInLeaf(rd.ViewOrigin)
			if viewerLeaf > 0 {
				if err := bsprender.MarkVisibleLeaves(markCtx,
					bsprender.VisLeafIdx(viewerLeaf),
					bsprender.FrameMarkSequence(stampFrame),
				); err != nil {
					return fmt.Errorf("MarkVisibleLeaves: %w", err)
				}
			} else {
				return nil
			}
		}

		surfaces.Reset()
		if err := bsprender.WalkWorld(walkCtx, 0, rd.ViewOrigin, frustum, stampFrame, &surfaces); err != nil {
			return fmt.Errorf("WalkWorld: %w", err)
		}

		// Early-tic signon trace: the wire-driven handshake should
		// transition Disconnected -> Connecting (tic 0, stage 1) ->
		// Connected (tic 0, stage 4) inside a single client.Tick call.
		// Logging the first few tics surfaces a regression if the
		// loopback flush stops delivering bytes to the client decoder.
		if frame < 6 {
			fmt.Printf("QUAKE: signon trace tic %d -- clientConn=%d Spawned=%v viewh=%v health=%d baselines=%d\n",
				frame, int(runner.Client.Connection),
				runner.Client.Spawned,
				runner.Client.ViewHeightOffset, runner.Client.Health,
				len(runner.Client.Baselines))
		}

		// One-shot wire-driven Spawned trace. The server-side flip
		// happens inside server.ParseClcStringCmd when the client's
		// "spawn" stringcmd hits ReadClientMoves; logging it here
		// proves the wire path drove the transition (vs the legacy
		// manual hc.Spawned = true poke that lived in run()).
		if !loggedWireSpawn && realHost != nil && playerSlot > 0 {
			if c := realHost.Static.Clients[playerSlot-1]; c != nil && c.Spawned {
				fmt.Printf("QUAKE: server flipped Spawned via 'spawn' clc_stringcmd (tic %d, slot %d)\n",
					frame, playerSlot-1)
				loggedWireSpawn = true
			}
		}

		// Per-frame face count log (sparse, every 60 frames) so the
		// serial log surfaces PVS culling effectiveness + the chosen
		// camera origin (player-edict-follow vs pickInMapCamera
		// fallback) without drowning the channel. Audio activity is
		// piggy-backed onto the same cadence: the runloop's Paint +
		// QueueAudio path runs immediately AFTER Pre2DDraw, so the
		// channel count we read here is what the next mix call will
		// process, and the mix size is constant (MixBufferStereoFrames).
		if frame%60 == 0 {
			active := 0
			if runner.SoundPool != nil {
				active = runner.SoundPool.ActiveCount()
			}
			cmdFwd, cmdSide := float32(0), float32(0)
			if realHost != nil && playerSlot > 0 {
				if c := realHost.Static.Clients[playerSlot-1]; c != nil {
					cmdFwd = c.Cmd.ForwardMove
					cmdSide = c.Cmd.SideMove
				}
			}
			// viewOrigin source classification. fromEntities=true
			// means the runloop pulled the origin out of the wire-
			// mirrored runner.Client.Entities map; fromEntities=false
			// means the wire has not yet delivered svc_update for the
			// local player slot + the closure substituted the
			// pickInMapCamera fallback. Cross-reference: the entity
			// Origin BEFORE the ViewHeightOffset Z-nudge AND the
			// Entities-map presence/origin even when the runloop's
			// fast path took a zero vector (helps diagnose
			// "wire delivered entities but the player slot is empty"
			// vs "wire hasn't delivered anything yet" gaps).
			viewSrc := "state.Entities"
			if !fromEntities {
				viewSrc = "fallback(pickInMapCamera)"
			}
			entOrigin := [3]float32{}
			entPresent := false
			if es, ok := runner.Client.Entities[runner.Client.PlayerNum]; ok {
				entOrigin = es.Origin
				entPresent = true
			}
			fmt.Printf("QUAKE: tic %d -- viewOrigin=%v src=%s entOrigin=%v entPresent=%v (PlayerNum=%d, %d entities cached) viewAngles=%v cmd.fwd=%v cmd.side=%v clientConn=%d cl.vel=%v cl.viewh=%v cl.health=%d; %d surfaces; audio: %d active, %d mixed\n",
				frame, origin, viewSrc, entOrigin, entPresent,
				runner.Client.PlayerNum, len(runner.Client.Entities),
				viewAngles, cmdFwd, cmdSide,
				int(runner.Client.Connection),
				runner.Client.Velocity, runner.Client.ViewHeightOffset, runner.Client.Health,
				surfaces.Len(),
				active, enginesound.MixBufferStereoFrames)

			// One-shot Entities-map census on the first per-60 log
			// after the wire drain has populated svc_updates. Surfaces
			// which entity slots actually land in State.Entities so
			// the serial log makes off-by-one + slot-skip bugs
			// (e.g. "player slot 1 was Free, SendEntityUpdates
			// skipped it") immediately visible without re-derivation.
			if frame == 60 && len(runner.Client.Entities) > 0 {
				minK, maxK := -1, -1
				hasPlayer := false
				for k := range runner.Client.Entities {
					if minK == -1 || k < minK {
						minK = k
					}
					if k > maxK {
						maxK = k
					}
					if k == runner.Client.PlayerNum {
						hasPlayer = true
					}
				}
				fmt.Printf("QUAKE: Entities-map census tic 60 -- count=%d minKey=%d maxKey=%d hasPlayerKey(PlayerNum=%d)=%v\n",
					len(runner.Client.Entities), minK, maxK,
					runner.Client.PlayerNum, hasPlayer)
			}

			// Per-tic svc_update flow. The server-side host.Frame stamps
			// the cumulative emit count onto LastEntityUpdatesSent every
			// tic; the client-side State.Entities map is the matching
			// receive cache (applyUpdate writes into it). Comparing the
			// two surfaces a "channel works" signal: M sent should equal
			// (or eventually equal, after the first tic) K received.
			if realHost != nil {
				fmt.Printf("QUAKE: updates tic %d -- %d entities sent / %d entities received in state.Entities\n",
					frame, realHost.LastEntityUpdatesSent, len(runner.Client.Entities))
			}
		}

		// Rasterize each visible face via TransformFace + FillTexturedPolygon.
		// Per-face texture pick: TexInfo.MiptexIdx -> miptexPics[idx].
		// Faces that resolve to a null miptex slot OR a synthetic BSP
		// with no Textures lump fall back to the checker.
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
			tex := fallbackTex
			if mtIdx, ok, _ := bm.FaceMipTexIdx(ref.FaceIdx); ok && mtIdx >= 0 && mtIdx < len(miptexPics) {
				if p := miptexPics[mtIdx]; p != nil {
					tex = p
				}
			}
			_ = render.FillTexturedPolygon(fb, tex, &cm, 0, verts)
		}
		return nil
	}
	return nil
}

// loadMiptexPics decodes the BSP's LUMP_TEXTURES into one *render.Pic
// per miptex slot, using each miptex's mip0 (full-resolution) pixels.
// Null slots (the upstream "missing texture" sentinel, offset == -1)
// land in the returned slice as nil; the per-face draw loop falls back
// to the synthetic checker for those.
//
// The pixels are palette-indexed in the BSP's own (id1) palette; the
// engine now loads the real gfx/palette.lmp out of the embedded pak
// (reportLumpSources in run() logs the swap), so the destination RGBA
// the renderer emits is in true id1 colours.
//
// Returns (slice, loaded, total, err) where loaded is the count of
// non-nil entries and total is the directory's slot count. A synthetic
// BSP that lacks a textures lump returns ([], 0, 0, nil).
func loadMiptexPics(file *bspfile.File) ([]*render.Pic, int, int, error) {
	mtl, err := file.Textures()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("file.Textures: %w", err)
	}
	total := int(mtl.NumMipTex)
	out := make([]*render.Pic, total)
	loaded := 0
	for i := 0; i < total; i++ {
		mt, ok, err := mtl.MipTex(i)
		if err != nil {
			// Skip the slot -- a single corrupt miptex shouldn't sink
			// the whole bridge; the per-face draw loop falls back to
			// the synthetic checker.
			continue
		}
		if !ok || mt == nil {
			continue
		}
		px, err := mt.Pixels(0)
		if err != nil {
			continue
		}
		// Pixels aliases the lump bytes; copy so the *render.Pic owns
		// a stable buffer (the lump cache is long-lived but defensive
		// copy keeps the renderer's invariants self-contained).
		buf := make([]byte, len(px))
		copy(buf, px)
		out[i] = &render.Pic{
			Width:  int(mt.Width),
			Height: int(mt.Height),
			Pixels: buf,
		}
		loaded++
	}
	return out, loaded, total, nil
}

// buildHost wires the embedded pak0 into a fully constructed
// host.Host: progs.Load -> progs.NewVM -> model.NewCache -> pak-backed
// FileResolver -> host.NewHost(maxClients=1) -> host.SpawnServer(map).
//
// Returns the SpawnServer'd host on success; any failure (missing
// progs.dat, malformed BSP, entity-parse error) is propagated to the
// caller, which falls back to the stubHost.
//
// mapSlug is the bare map name ("start", "e1m1") -- SpawnServer
// expands it to "maps/<slug>.bsp" internally via MapBSPPath.
func buildHost(pakFS fs.FS, mapSlug string) (*enginehost.Host, error) {
	// 1. progs.dat -> VM. Quake's bytecode lives at the top of the pak
	//    under "progs.dat"; failures here mean the pak is malformed
	//    (id Software's shareware ships it; community paks may not).
	progsBytes, ok := tryReadPakFile(pakFS, "progs.dat")
	if !ok {
		return nil, fmt.Errorf("buildHost: progs.dat missing from pak")
	}
	p, err := progs.Load(bytes.NewReader(progsBytes), int64(len(progsBytes)))
	if err != nil {
		return nil, fmt.Errorf("buildHost: progs.Load: %w", err)
	}
	vm := progs.NewVM(p)
	fmt.Printf("QUAKE: progs.dat loaded -- %d bytes, %d functions, %d global defs\n",
		len(progsBytes), len(p.Functions), len(p.GlobalDefs))

	// 2. Model cache + pak-backed FileResolver. The resolver fetches
	//    bytes by name out of the embedded pak so SpawnServer's
	//    LoadModelByName worldmodel-load sees the real BSP. Submodels
	//    are reused from the same File without re-resolving.
	cache := model.NewCache()
	resolver := func(name string) (int64, io.ReaderAt, error) {
		data, ok := tryReadPakFile(pakFS, name)
		if !ok {
			return 0, nil, fmt.Errorf("pak: %s missing", name)
		}
		return int64(len(data)), bytes.NewReader(data), nil
	}

	// 3. Host. maxClients=1 = the local-player loop. NewHost
	//    pre-allocates the Server + Static + World pools; SetProgs
	//    binds the bytecode the per-tic dispatcher consults for
	//    named-global hand-off.
	h, err := enginehost.NewHost(vm, cache, resolver, 1)
	if err != nil {
		return nil, fmt.Errorf("buildHost: NewHost: %w", err)
	}
	h.SetProgs(p)

	// 4. Builtin table. RegisterMathBuiltins wires the 9 pure-math
	//    builtins (normalize / vlen / vectoangles / random / ...);
	//    registerSpawnTimeBuiltins layers no-op stubs on top of every
	//    builtin a typical Q1 entity-spawn QC function calls
	//    (precache_model / precache_sound / setmodel / setorigin /
	//    setsize / lightstyle / dprint / stuffcmd / cvar / particle /
	//    objerror / sound). Without these the very first OP_CALL on
	//    a spawn function returns ErrBadBuiltin + the SpawnFn loop
	//    skips the rest of that entity. The stubs read nothing + do
	//    nothing -- the QC code's side effects (model = "blah";
	//    health = 60) live in the bytecode AFTER the builtin call,
	//    so the per-entity field assignment still lands on the edict.
	vm.RegisterMathBuiltins()
	if err := registerSpawnTimeBuiltins(vm); err != nil {
		return nil, fmt.Errorf("buildHost: registerSpawnTimeBuiltins: %w", err)
	}
	// random() seed. The math builtin BuiltinFnRandom returns
	// ErrRandomNotSeeded until SetRandomSource is wired -- spawn-time
	// QC (misc_fireball, monster ambient picks, ...) hits it the
	// instant the entity-spawn loop reaches one of those classnames.
	// A deterministic 32-bit LCG (Numerical Recipes constants) gives
	// a stable, side-effect-free float-in-[0,1) the spawn pass can
	// consume without pulling math/rand (the tamago std-lib subset
	// is intentionally minimal; an LCG is one multiply + one add per
	// call + zero allocations).
	vm.SetRandomSource(newLCGRandom(0xC0FFEE))

	// 5. Arena hand-off. SpawnServer allocates the per-map EdictArena
	//    BEFORE the entity-spawn pass walks the entities lump; the
	//    OnArenaReady hook fires there so vm.SetArena lands BEFORE
	//    the first SpawnFn dispatches. Without this, every entity-
	//    pointer opcode (OP_ADDRESS / OP_LOAD_ENT / OP_STORE_P_*)
	//    the spawn QC issues for "self.field = X" returns
	//    progs.ErrNoArena + the per-entity SpawnFn aborts. The
	//    hook also prints a one-line census so the serial console
	//    shows the wiring took effect.
	h.SetOnArenaReady(func(arena *progs.EdictArena) {
		vm.SetArena(arena)
		fmt.Printf("QUAKE: arena attached -- %d edicts in arena\n", arena.Cap())
	})

	// 5b. OP_STATE wiring. Monster-spawn QC (monster_zombie, ...)
	//     invokes OP_STATE to seed the entity's animation state +
	//     schedule the first think (".frame = N; .nextthink = time+0.1;
	//     .think = fn"). The VM defers the three field writes to the
	//     embedder so the entvars_t layout stays per-Progs rather than
	//     hard-coded; without SetStateHooks + SetStateFieldOffsets the
	//     spawn function aborts with ErrNoStateHooks. The selfEdict
	//     callback reads the "self" QC global the SpawnFn dispatch
	//     just seeded (step 6) -- a single source of truth for "which
	//     edict is OP_STATE writing into". timeSource pulls sv.time
	//     from the host so the scheduled nextthink uses the same clock
	//     the per-tic runthink loop will eventually consult; the
	//     reference scheduler is a separate concern, this wiring just
	//     makes the spawn-time field assignment succeed.
	if selfDef := p.FindGlobal("self"); selfDef != nil {
		selfOfs := int(selfDef.Ofs)
		vm.SetStateHooks(
			func() float32 { return float32(h.Server.Time) },
			func() int32 {
				v, _ := vm.GlobalInt(selfOfs)
				return v
			},
		)
	}
	if frameDef, nextThinkDef, thinkDef := p.FindField("frame"), p.FindField("nextthink"), p.FindField("think"); frameDef != nil && nextThinkDef != nil && thinkDef != nil {
		vm.SetStateFieldOffsets(int(nextThinkDef.Ofs), int(frameDef.Ofs), int(thinkDef.Ofs))
	}

	// 6. SpawnFn classname dispatch. Resolves the entity's classname
	//    to a QC function via FindFunction, sets the QC "self" global
	//    to the (slot-indexed) edict pointer, and calls VM.Run on
	//    the resolved index. A nil function (classname has no QC
	//    counterpart -- light_torch_small_walltorch and friends) is
	//    silently skipped. A VM.Run error is logged to the serial
	//    console + the loop continues with the next entity; the
	//    project-scope is "monsters get edicts" + "missing builtins
	//    are diagnosed", not "QC runs to completion".
	h.SetSpawnFn(func(ent *progs.Edict, classname string) {
		_, idx := p.FindFunction(classname)
		if idx < 1 {
			return
		}
		// Self global: spawn-time QC reads + writes ent->v.* via the
		// "self" pointer. With the arena now wired (step 5), the
		// "self" value is the real per-edict byte-offset pointer the
		// arena's MakePointer produces -- the entity-pointer opcodes
		// in spawn QC will resolve it back to ent's field block via
		// arena.ResolvePointer. A nil Server.Arena (test stubs that
		// skip SpawnServer) falls back to the slot index.
		if def := p.FindGlobal("self"); def != nil {
			_ = vm.SetGlobalInt(int(def.Ofs), edictSelfPointer(h, ent))
		}
		if err := vm.Run(int32(idx)); err != nil {
			fmt.Printf("QUAKE: SpawnFn %s err: %v\n", classname, err)
		}
	})

	// 7. SpawnServer. Loads the BSP, builds the area tree, parses the
	//    entities lump, populates the edict pool, fires SpawnFn per
	//    entity. The default no-op interner stores every string field
	//    as offset 0 (the empty-string sentinel) -- field structure is
	//    preserved; only the human-readable string payload is dropped.
	if err := h.SpawnServer(mapSlug, protocol.VersionNQ); err != nil {
		return nil, fmt.Errorf("buildHost: SpawnServer(%q): %w", mapSlug, err)
	}

	// 8. PutClientInServer dispatch. The QC "PutClientInServer" function
	//    is the canonical NQ id1 entrypoint that initialises a fresh
	//    player edict's stats (.health = 100, .items = IT_SHOTGUN|IT_AXE,
	//    .weapon = IT_SHOTGUN, .view_ofs = '0 0 22', ammo counts, etc.)
	//    via the QC "self" pointer. In the C upstream it runs from
	//    SV_SendClientReconnect (NQ/sv_user.c:890) after ClientConnect,
	//    once per client per signon stage 4 + on every respawn.
	//
	//    The Go port doesn't have the full signon-stage-4 + respawn
	//    cycle wired yet (Server.Static.Clients are bound via
	//    ConnectLoopback in the run() caller AFTER buildHost returns,
	//    and the wire-driven "spawn" stringcmd isn't parsed yet). This
	//    one-shot dispatch fires PutClientInServer ONCE post-SpawnServer
	//    so the player edict carries non-zero health/items/weapon/view_ofs
	//    by the time the first per-tic ComposeClientDataFromEdict reads
	//    them off the edict for the svc_clientdata payload -- otherwise
	//    every wire-borne ClientData frame would carry the bytecode
	//    defaults (health = 0, items = 0, view_ofs = '0 0 0') and the
	//    client-side State.Health / State.ViewHeightOffset stay zero.
	//
	//    Sequence:
	//      a. Locate the player edict (Server.Edicts[1] -- slot 0 is
	//         the world). Missing pool = silent skip (the test stub
	//         path that never reaches here).
	//      b. SetNewParms() (if the function exists). The upstream calls
	//         this from SV_ConnectClient (NQ/sv_main.c:457) to seed the
	//         per-client parm1..parm16 globals with the starting
	//         spawn-state. PutClientInServer then reads those parms +
	//         copies them into the per-edict fields. Skip silently when
	//         the function isn't defined; the parms stay at their
	//         bytecode defaults.
	//      c. Set the QC "self" global to point at the player edict
	//         (same encoding as the SpawnFn dispatch in step 6 -- the
	//         arena-MakePointer byte-offset, fallback to slot index for
	//         arena-less test stubs).
	//      d. Set the QC "time" global to sv.time (matching the
	//         thinkCaller pattern in host.go:497). PutClientInServer
	//         reads it for time-stamping the spawn (e.g. .takedamage
	//         deadline). Silently skip when the global isn't defined.
	//      e. vm.Run("PutClientInServer"). VM errors are logged + the
	//         buildHost continues; the field defaults left after a
	//         partial dispatch are still better than the pure-zero
	//         pre-dispatch state.
	//      f. Log the post-dispatch field readout (health, view_ofs[2],
	//         items, weapon) so the serial console proves the QC
	//         actually populated the entvars.
	//
	//    Scope deliberately narrow: this does NOT wire the full
	//    signon-4 + respawn cycle (SetNewParms-per-respawn,
	//    ClientConnect-per-connect, PutClientInServer-per-respawn);
	//    one initial pass is enough to prove the dispatch path + give
	//    the per-tic svc_clientdata back-channel real values to
	//    propagate.
	dispatchPutClientInServer(h, vm, p)

	return h, nil
}

// dispatchPutClientInServer runs the NQ id1 QC "PutClientInServer"
// function (with a SetNewParms warm-up when defined) against the
// player edict at Server.Edicts[1]. See the step-8 comment in
// [buildHost] for the rationale + the full upstream-mapping. Logs
// the post-dispatch entvars readout to the serial console.
//
// All lookups are tolerant: a progs.dat that strips any of these
// symbols (test fixtures, custom QC) silently skips the affected
// step + the rest of the dispatch continues. A vm.Run error is
// logged + execution proceeds -- a partial dispatch still leaves
// the player edict closer to the canonical spawn state than the
// pure-zero pre-dispatch defaults.
func dispatchPutClientInServer(h *enginehost.Host, vm *progs.VM, p *progs.Progs) {
	if h == nil || vm == nil || p == nil {
		return
	}
	// Player edict lives at slot 1 (slot 0 is the world, slots
	// 1..MaxClients are clients). A short pool (the test stub path
	// that never reaches here) is silently skipped.
	if len(h.Server.Edicts) < 2 {
		return
	}
	player := h.Server.Edicts[1]
	if player == nil {
		return
	}

	// Seed the "time" global so QC reads of "time" inside the dispatch
	// see the current sv.time. Mirrors host.thinkCaller (host.go:497).
	if timeDef := p.FindGlobal("time"); timeDef != nil {
		_ = vm.SetGlobalFloat(int(timeDef.Ofs), float32(h.Server.Time))
	}

	// Seed "self" -- the entity-pointer encoding the spawn QC uses to
	// resolve "self.field" reads/writes back to the player edict's
	// field block. Same encoding as the SpawnFn dispatch (step 6).
	selfDef := p.FindGlobal("self")
	if selfDef != nil {
		_ = vm.SetGlobalInt(int(selfDef.Ofs), edictSelfPointer(h, player))
	}

	// SetNewParms warm-up: populates parm1..parm16 globals with the
	// starting spawn-state (health, weapon, ammo). PutClientInServer
	// reads these to seed the per-edict fields. The C upstream calls
	// SetNewParms from SV_ConnectClient before PutClientInServer; we
	// fold them together because the Go port has no per-client
	// connect step here yet. Skipped silently when undefined.
	if _, snpIdx := p.FindFunction("SetNewParms"); snpIdx >= 1 {
		if err := vm.Run(int32(snpIdx)); err != nil {
			fmt.Printf("QUAKE: SetNewParms vm.Run err: %v\n", err)
		} else {
			fmt.Printf("QUAKE: SetNewParms dispatched -- starting spawn parms seeded\n")
		}
	}

	// PutClientInServer: the actual edict-init function. Re-seed "self"
	// in case SetNewParms clobbered it (the upstream rebinds self
	// before every dispatch; cheap insurance).
	if selfDef != nil {
		_ = vm.SetGlobalInt(int(selfDef.Ofs), edictSelfPointer(h, player))
	}
	_, pcisIdx := p.FindFunction("PutClientInServer")
	if pcisIdx < 1 {
		fmt.Printf("QUAKE: PutClientInServer not found in progs.dat -- player edict stays at bytecode defaults\n")
		return
	}
	if err := vm.Run(int32(pcisIdx)); err != nil {
		fmt.Printf("QUAKE: PutClientInServer vm.Run err: %v\n", err)
		// Fall through: log whatever the partial dispatch left behind.
	}

	// Post-dispatch readout. Proves the QC actually populated the
	// entvars: a successful PutClientInServer leaves
	// health=100, view_ofs=(0,0,22), items=(IT_SHOTGUN|IT_AXE)=4097,
	// weapon=IT_SHOTGUN=1 on the player edict. Field-not-found
	// errors are surfaced as "<unset>" so callers can distinguish
	// "progs strips this field" from "field present but zero".
	v, _ := progs.NewEntVars(p, player)
	healthStr := "<unset>"
	if hv, err := v.ReadFloat("health"); err == nil {
		healthStr = fmt.Sprintf("%g", hv)
	}
	viewOfsStr := "<unset>"
	if vo, err := v.ReadVec3("view_ofs"); err == nil {
		viewOfsStr = fmt.Sprintf("(%g,%g,%g)", vo[0], vo[1], vo[2])
	}
	itemsStr := "<unset>"
	if it, err := v.ReadFloat("items"); err == nil {
		itemsStr = fmt.Sprintf("%g (0x%x)", it, int32(it))
	}
	weaponStr := "<unset>"
	if wp, err := v.ReadFloat("weapon"); err == nil {
		weaponStr = fmt.Sprintf("%g", wp)
	}
	fmt.Printf("QUAKE: PutClientInServer dispatched -- player edict 1 health=%s view_ofs=%s items=%s weapon=%s\n",
		healthStr, viewOfsStr, itemsStr, weaponStr)
}

// edictSelfPointer returns the QC "self" pointer for ent: the per-
// edict byte offset the arena's MakePointer encodes when the host
// has an arena attached (the production path now that step 5 wires
// vm.SetArena via OnArenaReady), falling back to the slot index for
// test stubs that skip SpawnServer entirely (no arena -> the VM
// won't see entity-pointer opcodes anyway, so a self-consistent int
// is sufficient).
func edictSelfPointer(h *enginehost.Host, ent *progs.Edict) int32 {
	if h.Server.Arena != nil {
		return h.Server.Arena.PointerForEdict(ent)
	}
	return edictSlot(h, ent)
}

// edictSlot returns the index of ent inside h.Server.Edicts. Used
// as the no-arena fallback for [edictSelfPointer]; spawn-time QC
// reads back what we wrote, so a self-consistent integer satisfies
// the "self" hand-off when the entity-pointer opcodes don't fire.
func edictSlot(h *enginehost.Host, ent *progs.Edict) int32 {
	for i, e := range h.Server.Edicts {
		if e == ent {
			return int32(i)
		}
	}
	return 0
}

// registerSpawnTimeBuiltins installs no-op stubs for the builtin
// indices typical Q1 entity-spawn QC functions hit before they get
// to the field-assignment half of their body. The stubs do nothing
// + return nil; the spawn function's per-classname field writes
// (self.health = 60, self.model = "...", ...) still land on the
// edict because they're plain bytecode after the builtin returns.
//
// Coverage matches tyrquake's pr_cmds.c pr_builtins[] indices the
// shareware progs.dat references during entity spawn: setorigin,
// setmodel, setsize, sound, precache_sound, precache_model,
// stuffcmd, lightstyle, cvar, particle, objerror, dprint, bprint,
// sprint, eprint, error, walkmove, droptofloor, checkbottom,
// pointcontents, find, findradius, traceline, checkclient, aim,
// nextent, traceon, traceoff, coredump, break, makestatic,
// changelevel, setspawnparms, makevectors, spawn, remove, ftos,
// vtos, localcmd, changeyaw, cvar_set.
//
// Functional builtins (the math 9 from RegisterMathBuiltins) stay
// real; only the spawn-time side-effect builtins are stubbed here.
// A real implementation would precache assets, link entities into
// the world tree, etc.; for "prove the SpawnFn dispatch works" the
// no-op shape is sufficient + safer than a half-port that crashes.
func registerSpawnTimeBuiltins(vm *progs.VM) error {
	noop := func(_ *progs.VM) error { return nil }
	// makevectors writes v_forward/right/up; spawn-time entities
	// rarely consult those, so the stub leaves them untouched. A
	// future batch wires the real math.
	vm.RegisterBuiltin(progs.BuiltinMakeVectors, noop)
	vm.RegisterBuiltin(progs.BuiltinSetOrigin, noop)
	vm.RegisterBuiltin(progs.BuiltinSetModel, noop)
	vm.RegisterBuiltin(progs.BuiltinSetSize, noop)
	vm.RegisterBuiltin(progs.BuiltinBreak, noop)
	vm.RegisterBuiltin(progs.BuiltinSound, noop)
	vm.RegisterBuiltin(progs.BuiltinError, noop)
	vm.RegisterBuiltin(progs.BuiltinObjError, noop)
	vm.RegisterBuiltin(progs.BuiltinSpawn, noop)
	vm.RegisterBuiltin(progs.BuiltinRemove, noop)
	vm.RegisterBuiltin(progs.BuiltinTraceLine, noop)
	vm.RegisterBuiltin(progs.BuiltinCheckClient, noop)
	vm.RegisterBuiltin(progs.BuiltinFind, noop)
	vm.RegisterBuiltin(progs.BuiltinPrecacheSound, noop)
	vm.RegisterBuiltin(progs.BuiltinPrecacheModel, noop)
	vm.RegisterBuiltin(progs.BuiltinStuffCmd, noop)
	vm.RegisterBuiltin(progs.BuiltinFindRadius, noop)
	vm.RegisterBuiltin(progs.BuiltinBPrint, noop)
	vm.RegisterBuiltin(progs.BuiltinSPrint, noop)
	vm.RegisterBuiltin(progs.BuiltinDPrint, noop)
	vm.RegisterBuiltin(progs.BuiltinFToS, noop)
	vm.RegisterBuiltin(progs.BuiltinVToS, noop)
	vm.RegisterBuiltin(progs.BuiltinCoreDump, noop)
	vm.RegisterBuiltin(progs.BuiltinTraceOn, noop)
	vm.RegisterBuiltin(progs.BuiltinTraceOff, noop)
	vm.RegisterBuiltin(progs.BuiltinEPrint, noop)
	vm.RegisterBuiltin(progs.BuiltinWalkMove, noop)
	vm.RegisterBuiltin(progs.BuiltinDropToFloor, noop)
	vm.RegisterBuiltin(progs.BuiltinLightStyle, noop)
	vm.RegisterBuiltin(progs.BuiltinCheckBottom, noop)
	vm.RegisterBuiltin(progs.BuiltinPointContents, noop)
	vm.RegisterBuiltin(progs.BuiltinAim, noop)
	vm.RegisterBuiltin(progs.BuiltinCVar, noop)
	vm.RegisterBuiltin(progs.BuiltinLocalCmd, noop)
	vm.RegisterBuiltin(progs.BuiltinNextEnt, noop)
	vm.RegisterBuiltin(progs.BuiltinParticle, noop)
	vm.RegisterBuiltin(progs.BuiltinChangeYaw, noop)
	// High-index builtins. tyrquake's pr_builtin[] indices 68..79 are
	// the second-half table that defs.qc exposes as precache_file,
	// makestatic, changelevel, cvar_set, centerprint, ambientsound,
	// precache_model2, precache_sound2, precache_file2, setspawnparms.
	// The shareware progs.dat calls #72 from worldspawn (precache_file
	// in some defs.qc rev) and #74 from every light_* / trigger_teleport
	// (centerprint). All are pure side-effect (precache / HUD print /
	// link-to-static) so the no-op is faithful to "the spawn pass
	// reaches the field-assignment half"; the per-classname state
	// writes still land on the edict because they're bytecode after
	// the builtin returns. Indices in between (68/69/70/71/73/75/...)
	// get stubbed too so the next undefined-slot won't surface as the
	// progs.dat exercises further functions on subsequent ticks.
	for _, idx := range []int{68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79} {
		vm.RegisterBuiltin(idx, noop)
	}
	// WriteByte / WriteChar / WriteShort / WriteLong / WriteCoord /
	// WriteAngle / WriteString / WriteEntity occupy slots 52..60 in
	// tyrquake's table. Server-side QC emits client-message bytes
	// through these; no client is reading, so swallowing them is
	// safe for the spawn-time + early-tic phase.
	for _, idx := range []int{52, 53, 54, 55, 56, 57, 58, 59, 60} {
		vm.RegisterBuiltin(idx, noop)
	}
	return nil
}

// newLCGRandom returns a float-in-[0,1) callback suitable for
// VM.SetRandomSource. The PRNG is the Numerical-Recipes 32-bit LCG
// (multiplier 1664525, increment 1013904223): cheap, deterministic,
// and seedable so demo-replay parity is achievable without pulling
// math/rand (tamago's std-lib subset omits a fair amount of the
// stock pkg surface; an LCG is one multiply + one add per call).
func newLCGRandom(seed uint32) func() float32 {
	state := seed
	return func() float32 {
		state = state*1664525 + 1013904223
		// Top 24 bits / 2^24 -> a float32 in [0, 1). The 0x7fff
		// shape of tyrquake's PF_random is preserved-in-spirit but
		// uses the full 24-bit mantissa for a smoother distribution.
		return float32(state>>8) / float32(1<<24)
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

// writePlayerOrigin overwrites the QC "origin" vector on the player
// edict at slot. Returns nil on success, or the propagated EntVars
// error -- typically [progs.ErrFieldNotFound] when the bound Progs's
// FieldDefs table lacks "origin" (test stubs that strip the field).
//
// Used by setupRenderer at startup to seed the player edict's origin
// with the pickInMapCamera lattice anchor when the QC spawn pass left
// it at the zero vector (which sits inside a solid leaf and would
// trap the per-tic integrator below at the world origin).
func writePlayerOrigin(h *enginehost.Host, slot int, origin [3]float32) error {
	if h == nil || slot < 0 || slot >= len(h.Server.Edicts) {
		return enginehost.ErrNoEdict
	}
	ent := h.Server.Edicts[slot]
	if ent == nil {
		return enginehost.ErrNoEdict
	}
	p := h.Progs()
	if p == nil {
		return enginehost.ErrNoProgs
	}
	v, err := progs.NewEntVars(p, ent)
	if err != nil {
		return err
	}
	return v.WriteVec3("origin", origin)
}

// initPlayerForPhysicsWalk seeds the per-edict entvars fields the
// per-tic RunPhysics dispatcher + PhysicsWalk handler require to take
// the player edict at slot through one tic of the MOVETYPE_WALK arm:
//
//   - movetype = MOVETYPE_WALK         (selects the PhysicsWalk handler)
//   - solid    = SOLID_SLIDEBOX        (lets PushEntity actually trace)
//   - mins/maxs = hull-1 (-16,-16,-24 .. 16,16,32) -- the standard Q1
//     player hull. Without a real bbox the world-trace collapses to a
//     ray and the player can clip through faces.
//   - velocity / v_angle / flags / gravity = zero / 1.0 -- a clean
//     rest state from which gravity can settle the player onto the
//     floor and CheckBottom can latch FL_ONGROUND.
//
// The full QC PutClientInServer would set additional fields (health,
// model, weapon, ...); we skip those -- they're not read by PhysicsWalk
// and the rendering path takes the origin from EdictOrigin directly.
//
// Returns the first EntVars error (typically ErrFieldNotFound on a
// progs that strips one of these standard fields -- not a real Q1
// progs.dat), or nil on success. Per-write errors are surfaced
// verbatim so the caller can log + decide whether to abort.
func initPlayerForPhysicsWalk(h *enginehost.Host, slot int) error {
	if h == nil || slot < 0 || slot >= len(h.Server.Edicts) {
		return enginehost.ErrNoEdict
	}
	ent := h.Server.Edicts[slot]
	if ent == nil {
		return enginehost.ErrNoEdict
	}
	p := h.Progs()
	if p == nil {
		return enginehost.ErrNoProgs
	}
	v, err := progs.NewEntVars(p, ent)
	if err != nil {
		return err
	}
	if err := v.WriteFloat("movetype", float32(int32(engineserver.MoveTypeWalk))); err != nil {
		return err
	}
	if err := v.WriteFloat("solid", float32(int32(engineserver.SolidSlideBox))); err != nil {
		return err
	}
	if err := v.WriteVec3("mins", [3]float32{-16, -16, -24}); err != nil {
		return err
	}
	if err := v.WriteVec3("maxs", [3]float32{16, 16, 32}); err != nil {
		return err
	}
	if err := v.WriteVec3("velocity", [3]float32{0, 0, 0}); err != nil {
		return err
	}
	if err := v.WriteVec3("v_angle", [3]float32{0, 0, 0}); err != nil {
		return err
	}
	if err := v.WriteFloat("flags", 0); err != nil {
		return err
	}
	// gravity is QuakeWorld-only -- stock NQ id1 defs.qc does not
	// declare it. PhysicsWalk's readStepGravityFactor handles the
	// absent-field case by defaulting to 1.0, so the silent skip here
	// is functionally identical to a successful write of 1.0.
	if err := v.WriteFloat("gravity", 1.0); err != nil && !errors.Is(err, progs.ErrFieldNotFound) {
		return err
	}
	return nil
}

// loadBSP returns the BSP bytes + size to render. Sources, in order:
//
//  1. The shared embedpak fs.FS opened by run() -- try "maps/start.bsp"
//     (canonical entry map) then "maps/e1m1.bsp" (episode 1 first map).
//  2. synthbsp.BuildWithFaces() -- the always-available synthetic
//     fallback. Used when pakFS is nil (placeholder pak installed)
//     OR when neither canonical map is present in the supplied pak.
//
// The chosen path is logged on the serial console so the QEMU log
// makes the source unambiguous.
func loadBSP(pakFS fs.FS) ([]byte, int64, error) {
	if pakFS != nil {
		for _, mapName := range []string{"maps/start.bsp", "maps/e1m1.bsp"} {
			data, ok := tryReadPakFile(pakFS, mapName)
			if ok {
				fmt.Printf("QUAKE: loaded %s from embedded pak0.pak (%d bytes)\n",
					mapName, len(data))
				return data, int64(len(data)), nil
			}
		}
		fmt.Printf("QUAKE: embedded pak0.pak lacks maps/start.bsp and maps/e1m1.bsp; using synthbsp fallback\n")
	} else {
		fmt.Printf("QUAKE: using synthbsp fallback (no pak FS available)\n")
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

// reportLumpSources probes each named lump against the live SearchPath
// and prints which source (real pak vs synthetic fallback) wins.
// The classification compares the resolved bytes (from v) against the
// real pak's bytes for the same key: a match -> "real pak"; a mismatch
// or missing-from-pak entry -> "synthetic". This gives the QEMU serial
// log an unambiguous one-line confirmation that the palette swap
// landed (the whole point of this batch) without having to eyeball
// the PPM colours through a screendump.
func reportLumpSources(v *vfs.SearchPath, pakFS fs.FS, syn fs.FS, lumps []string) {
	for _, name := range lumps {
		got, ok := tryReadFromFS(v, name)
		if !ok {
			fmt.Printf("QUAKE: %s NOT FOUND in any source\n", name)
			continue
		}
		source := "synthetic"
		if pakFS != nil {
			if real, okp := tryReadFromFS(pakFS, name); okp && bytes.Equal(real, got) {
				source = "real pak"
			}
		}
		fmt.Printf("QUAKE: %s from %s (%d bytes)\n", name, source, len(got))
	}
}

// tryReadFromFS opens name on src and returns its contents. Reports
// (nil, false) on any failure so the caller can fall through without
// classifying the underlying error.
func tryReadFromFS(src fs.FS, name string) ([]byte, bool) {
	f, err := src.Open(name)
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

// seedSoundPool loads each candidate WAV name out of pakFS, parses it
// via sound.LoadWav, and parks it on one of the pool's reserved-static
// channel slots (slots 0..ReservedStatic-1, the bank the upstream
// engine carved out for level-ambient loops). Each seeded channel
// plays at full volume (LeftVol/RightVol = 200) from Position 0 to
// EndPos = sample.NumSamples, then retires when sound.Paint advances
// past EndPos.
//
// The per-sample header info (rate, bits, channels, size) is logged to
// the serial console so the QEMU run-log makes the loaded asset set
// unambiguous. Missing assets + parse errors are logged but otherwise
// skipped -- the engine stays boot-safe when the shareware archive's
// nav-editor subset is absent.
//
// Returns the count of seeded channels (<= len(names) and <=
// pool.ReservedStatic).
//
// Channels are NOT looped here: LoopStart == -1 in the candidate WAVs,
// and the runloop's Paint path will Stop() them when their data is
// consumed. This is enough to prove the audio pipeline reaches
// virtio-sound (the goal of this batch); a follow-up wires the looped
// 16-bit ambient track once sound.Paint gains the 16-bit mix path.
func seedSoundPool(pool *enginesound.Pool, pakFS fs.FS, names []string) int {
	seeded := 0
	for _, name := range names {
		if seeded >= pool.ReservedStatic {
			break
		}
		blob, ok := tryReadPakFile(pakFS, name)
		if !ok {
			fmt.Printf("QUAKE: sound asset missing: %s\n", name)
			continue
		}
		s, err := enginesound.LoadWav(name, blob)
		if err != nil {
			fmt.Printf("QUAKE: sound asset load failed: %s -- %v\n", name, err)
			continue
		}
		fmt.Printf("QUAKE: loaded WAV %s -- rate=%dHz bits=%d numSamples=%d loopStart=%d dataLen=%d\n",
			name, s.SampleRate, s.BitsPerSam, s.NumSamples, s.LoopStart, len(s.Data))
		ch := &pool.Channels[seeded]
		ch.Sfx = s
		ch.Position = 0
		ch.EndPos = s.NumSamples
		ch.LeftVol = 200
		ch.RightVol = 200
		ch.Master = true
		seeded++
	}
	return seeded
}

// halt is the tamago "spin forever after a fatal error" primitive.
// The board package's Exit handler also halts; this one covers the
// pre-board failure window + the in-engine return path.
func halt() {
	for {
	}
}

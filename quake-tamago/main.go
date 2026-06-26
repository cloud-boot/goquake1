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
	"strings"
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
	"github.com/go-quake1/engine/demo"
	"github.com/go-quake1/engine/embedmusic"
	"github.com/go-quake1/engine/embedpak"
	enginehost "github.com/go-quake1/engine/host"
	"github.com/go-quake1/engine/mathlib"
	"github.com/go-quake1/engine/mdl"
	"github.com/go-quake1/engine/menu"
	"github.com/go-quake1/engine/model"
	enginemusic "github.com/go-quake1/engine/music"
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/quake-tamago/concharsfont"
	"github.com/go-quake1/engine/render"
	"github.com/go-quake1/engine/runloop"
	engineserver "github.com/go-quake1/engine/server"
	enginesound "github.com/go-quake1/engine/sound"
	enginespr "github.com/go-quake1/engine/spr"
	"github.com/go-quake1/engine/vfs"
	"github.com/go-quake1/engine/wad"
	"github.com/go-quake1/engine/world"
)

// fbWidth / fbHeight are the framebuffer dimensions handed to
// virtio-gpu's SetupFramebuffer. 320x240 matches vanilla DOS Quake
// (320x200/320x240) so the software span rasterizer's per-pixel
// perspective-correct texture mapping stays affordable in QEMU TCG.
//
// History: this used to be 1280x1024 = 1.3 megapixels per frame,
// which in pure-Go on QEMU TCG (no KVM) clocked ~0.85 tics per
// wall-clock second -- effectively a slideshow, AND the engine's
// renderer only paints the central viewport so the outer pixels
// stayed at their (palette idx 0x02 / 0x20) clear colour. From a
// distance that read as "colours wrong" + "doesn't look like
// 1280x1024" because the rendered scene was a small inset in a
// mostly-grey field. 320x240 is 76800 pixels (16.8x less work) so
// the same TCG host now targets 30+ fps, the renderer fills the
// whole surface, and QEMU's cocoa / gtk display layer (zoom-to-fit
// = on, set in Taskfile.yml) upscales the 320x240 scanout to
// whatever size the user drags the host window to.
const (
	fbWidth  = 320
	fbHeight = 240
)

// demoOrbit toggles the programmatic camera walk that the headless
// QEMU validation harness needs. With virtio-input absent (the
// `-display none` boot path the screendump task uses) client.Tick
// never refreshes ViewAngles -- so without the override every
// captured frame would be the same fixed-yaw shot from the player
// spawn. demoOrbit=true makes Pre2DDraw rewrite (viewAngles, viewOrigin)
// each tic:
//
//   - viewAngles.Yaw = frame % 360 (full panoramic every 360 frames,
//     ~6 s at 60 Hz). Pitch + roll forced to 0.
//   - viewOrigin steps through demoWaypoints every
//     demoWaypointPeriodSeconds seconds of sv.time so multi-shot
//     screendump runs land in geometrically distinct parts of the
//     map -- exposing different miptex sets, different alias
//     entities, different leaves. Driving the cadence off sv.time
//     (not the frame counter) keeps dwells consistent across TCG /
//     hardware regardless of the Pre2DDraw frame rate.
//
// The waypoint set is seeded from pickInMapCamera at runtime (so the
// anchor is always inside a valid leaf) + the BSP bbox corners
// snapped back into a valid leaf via the same lattice walk.
//
// PLAYABLE-MODE OVERRIDE: this is a runtime [var] (not a const) so
// the playable QEMU launchers (task qemu-playable / qemu-visible with
// virtio-keyboard) can flip it off without a rebuild. The runtime
// Pre2DDraw closure ALSO auto-disables it on the first observed
// virtio-input key event -- so a user pressing W on a session that
// booted with demoOrbit=true (the default) immediately gets the real
// player-driven camera. This keeps the default safe for headless
// screendump captures (no input device -> demoOrbit stays on -> the
// captured frame is the panorama waypoint) while still enabling
// interactive play the moment a key fires.
var demoOrbit = true

// demoOrbitAutoDisableOnInput, when true, makes the runtime Pre2DDraw
// closure flip demoOrbit to false on the first observed key event.
// Default true: keeps headless screendumps producing the panorama
// AND lets a human pressing W on a virtio-keyboard session take over
// without restarting. Flip to false to keep the demo orbit running
// even when the user is playing (handy for promo-shot capture).
var demoOrbitAutoDisableOnInput = true

// demoYawPeriodFrames is the frame count over which Yaw winds from
// 0 to 360. One degree per frame at 60 Hz gives a 6-second panorama,
// which is fast enough that a 30 s headless capture hits ~5 full
// orbits + slow enough that any single screendump catches a clean
// instantaneous angle (no motion-blur to worry about: the renderer
// is a one-shot per-frame rasterize, not a temporal sampler).
const demoYawPeriodFrames = 360

// demoWaypointPeriodSeconds is how long (in sv.time seconds) each
// waypoint holds before the next one takes over. Driving the cadence
// off sv.time instead of the frame counter decouples waypoint dwells
// from the Pre2DDraw frame rate -- QEMU TCG headless lands around 1
// frame/s (the BSP walk + alias pass + miptex rasterize is the slow
// path on TCG without KVM) while hardware builds run 60+ frames/s,
// and a fixed frame-count period would mean the cycle takes minutes
// in one environment + sub-second in the other.
//
// 2 s/waypoint means the 4-waypoint set cycles once every 8 s of
// game time regardless of whether the host is rendering 1 fps or
// 60 fps, which matches the capture_waypoints.sh harness cadence
// (one PPM per waypoint at t = 1 s, 3 s, 5 s, 7 s).
const demoWaypointPeriodSeconds = 2.0

// stubHost satisfies runloop.HostFramer for first bring-up. The real
// id-Software game-server tick wires in a follow-up batch; for now
// the loop just renders frames + processes input.
type stubHost struct{}

// Frame is a no-op: the server simulation is absent until embedpak
// + sv_main land.
func (stubHost) Frame(_ float32) error { return nil }

// changelevelHostFramer wraps an [enginehost.Host] so the per-tic
// [runloop.Runner.RunFrame] step also polls
// [enginehost.Host.ConsumeChangelevel] post-Frame. On a positive
// poll the wrapper logs the requested map slug + drives the
// SpawnServer re-entry into the new map.
//
// Without this wrapper the QC `changelevel` builtin would flip
// [enginehost.Host.PendingChangelevel] = true but nobody would act
// on it; the next per-tic Frame would just re-run the same map
// indefinitely, so the player can never progress past
// trigger_changelevel volumes.
//
// SpawnServer failures (corrupt BSP for the requested map, missing
// pak entry, etc.) are logged + the player stays on the current
// map (the previous Server.Active state is preserved by the
// in-place SpawnServer call). The Frame error itself is not
// surfaced -- a bad changelevel must not kill the runloop.
type changelevelHostFramer struct {
	host *enginehost.Host
}

// Frame runs one host tic, then polls + acts on any pending
// changelevel request. The Frame error is surfaced verbatim; the
// post-Frame SpawnServer error (if any) is logged but not returned
// so a bad map name can't kill the loop.
func (c *changelevelHostFramer) Frame(dt float32) error {
	if err := c.host.Frame(dt); err != nil {
		return err
	}
	if pending, mapSlug := c.host.ConsumeChangelevel(); pending {
		fmt.Printf("QUAKE: level change requested -- nextmap=%q\n", mapSlug)

		// Push svc_intermission + the four end-of-level stat
		// slots into the server's ReliableDatagram BEFORE
		// re-spawning into the new map -- the SpawnServer call
		// would otherwise clear the buffers. HarvestIntermissionStats
		// pulls the per-map tally from QC named globals
		// (total_secrets / found_secrets / total_monsters /
		// killed_monsters); missing globals leave the existing
		// stats untouched (= zero on first call). EmitIntermission
		// is logged but its failure is not surfaced -- a bad write
		// must not kill the loop.
		c.host.HarvestIntermissionStats()
		if err := c.host.EmitIntermission(); err != nil {
			fmt.Printf("QUAKE: EmitIntermission failed: %v\n", err)
		} else {
			s := c.host.LastIntermissionStats
			fmt.Printf("QUAKE: intermission active=1 (secrets=%d/%d monsters=%d/%d)\n",
				s.FoundSecrets, s.TotalSecrets, s.KilledMonsters, s.TotalMonsters)
		}

		if err := c.host.SpawnServer(mapSlug, c.host.Server.Protocol); err != nil {
			fmt.Printf("QUAKE: changelevel SpawnServer(%q) failed: %v -- staying on previous map\n",
				mapSlug, err)
		} else {
			fmt.Printf("QUAKE: changelevel SpawnServer(%q) ok -- now on new map\n", mapSlug)
		}
	}
	return nil
}

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
		// Diagnostic PCMInfo dump -- helps debug rate / format
		// mismatches when SetupAudio rejects a tuple.
		if infos, ierr := vs.PCMInfo(); ierr == nil {
			for i, e := range infos {
				fmt.Printf("QUAKE: snd-stream[%d] dir=%d rates=%#x formats=%#x ch=%d-%d\n",
					i, e.Direction, e.Rates, e.Formats, e.ChannelsMin, e.ChannelsMax)
			}
		} else {
			fmt.Printf("QUAKE: snd-PCMInfo err=%v\n", ierr)
		}
		// Drive the PCM lifecycle: PCMInfo -> PCMSetParams -> PCMPrepare
		// -> PCMStart. After this the device DMA-consumes from the tx
		// virtqueue and emits PCM to the host audio backend (the
		// -audiodev wav,id=audio0 path used by `task qemu-headless`
		// writes the mixed result to /tmp/quake-audio.wav). The engine
		// mixer paints 11025 Hz stereo S16 frames; the WrapAudio
		// adapter upsamples to the negotiated device rate before
		// pushing to the tx virtqueue. A failure here logs + falls
		// through to silent operation so the renderer still boots and
		// we can see the PCMInfo dump above.
		res, serr := realdev.SetupAudio(vs, realdev.DefaultAudioStreamConfig)
		if serr != nil {
			fmt.Printf("QUAKE: SetupAudio err=%v (engine runs silent)\n", serr)
		} else {
			audioDev = realdev.WrapAudio(vs, res.StreamID, enginesound.DefaultSampleRate)
			fmt.Printf("QUAKE: sound=%#04x:%#04x stream=%d device-rate=%dHz mixer-rate=%dHz ch=%d fmt=%d (virtio-snd stream started)\n",
				sndDev.Vendor, sndDev.Device, res.StreamID, res.Rate, enginesound.DefaultSampleRate, res.Channels, res.Format)
		}
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
		// Wrap with wadOverlay so gfx/X.lmp probes fall through to
		// gfx.wad inner lumps when the pak doesn't expose them as
		// standalone files (Quake Remastered case for conchars.lmp,
		// sbar.lmp, etc.). Non-gfx paths still resolve via the base
		// pak unchanged.
		v.Add(newWADOverlay(pakFS, "gfx.wad")) // real pak (prepended -> first in probe order)
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
			// ModelPrecache census. Proves [builtinPrecacheModel] +
			// [builtinSetModel] actually populated the slice past the
			// world+submodel slots SpawnServer seeded. Counts every
			// non-empty entry + how many of those end in ".mdl" (the
			// alias-model extension; .bsp = brush submodels seeded by
			// SpawnServer, .spr = sprites). A zero .mdl count means the
			// real builtins aren't firing + the alias-render pass
			// downstream will draw nothing.
			total, mdlCount := 0, 0
			for _, n := range h.Server.ModelPrecache {
				if n == "" {
					break
				}
				total++
				if len(n) >= 4 && n[len(n)-4:] == ".mdl" {
					mdlCount++
				}
			}
			fmt.Printf("QUAKE: ModelPrecache populated -- %d entries (%d .mdl alias models)\n",
				total, mdlCount)
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
		// to StateConnected.
		//
		// Stage-4 emission stays wire-driven; the only manual poke is
		// the pre-flip of hc.Spawned below (matches the parallel
		// player-edict Free=false poke). See the inline comment at the
		// poke site for the rationale -- short version: tic 0's
		// host.Frame runs BEFORE client.Tick gets to flush the inbound
		// "spawn" stringcmd, so without the pre-flip the per-tic
		// SendEntityUpdates filter (`if !client.Spawned ... return`)
		// skips every emission on the first tic.
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
		// Pre-flip hc.Spawned = true so the FIRST per-tic Host.Frame
		// call already passes the SendEntityUpdates gate (`if !client.
		// Spawned ... return`). The wire-driven "spawn" clc_stringcmd
		// still flips the SAME field a tic or two later when the client
		// emits it -- the operation is idempotent (already-true stays
		// true), so the wire flow is preserved end-to-end; this poke
		// just plugs the one-frame gap where tic 0's host.Frame runs
		// BEFORE client.Tick gets a chance to flush the inbound
		// "spawn" stringcmd back to the server.
		//
		// Mirrors the parallel `pe.Free = false` poke below at the
		// player-edict slot (without which the same SendEntityUpdates
		// filter -- `e == nil || e.Free` -- skipped the player every
		// tic). The two poke-shapes share rationale: "the per-tic
		// broadcast filter needs both client.Spawned + edict.Free
		// flipped before the first emission, and the wire-driven
		// flow can't deliver either flip in time for tic 0".
		//
		// Before this poke, the regression manifested as:
		//   QUAKE: updates tic 0 -- 0 entities sent / 0 entities ...
		//   QUAKE: tic 0 rendered 0 alias entities
		// After: 538 entities sent + 538 received at tic 0 (matching
		// the batch-71 baseline before the wire-signon-handshake
		// refactor removed the explicit hc.Spawned = true pre-poke).
		hc.Spawned = true
		fmt.Printf("QUAKE: loopback bound -- client slot %d -> edict %d (active+Spawned, wire 'spawn' will re-flip idempotently); wire signon prefix queued (%d bytes)\n",
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
	//
	//     The real host is wrapped in a [changelevelHostFramer] so the
	//     post-Frame poll of [enginehost.Host.ConsumeChangelevel] fires
	//     each tic; on a positive poll the wrapper logs the transition
	//     + drives a fresh SpawnServer into the requested map slug.
	//     Without the wrapper the QC `changelevel` builtin would flip
	//     the flag but nobody would act on it, and the player would
	//     stay parked on the previous map forever.
	var hostFramer runloop.HostFramer = stubHost{}
	if realHost != nil {
		hostFramer = &changelevelHostFramer{host: realHost}
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

	// 11. Seed the console buffer with a couple of bring-up lines. The
	//     console drop-down stays closed (ConCurrent=0) so it doesn't
	//     overlay the 3D scene; the user can drop it open via the
	//     in-game console binding when that lands.
	runner.Console.Print("PURE-GO QUAKE 1 -- TamaGo + go-virtio bring-up\n")
	runner.Console.Print("runloop wired: input -> client.Tick -> host.Frame -> Pre2DDraw\n")

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

	// 11c. Unify the mixer pool: the runner currently owns a per-Runner
	//      pool (allocated by NewRunnerFromVFS); replace it with the
	//      host's pool (allocated INSIDE buildHost, BEFORE SpawnServer,
	//      so the spawn-time QC's sound/ambientsound builtins landed
	//      channels on a real pool from the start). After this swap
	//      the per-tic runloop's Paint walks the SAME bank the
	//      QC-driven StartSound / AmbientSound calls fill. Without
	//      this the per-tic Paint would walk an unused empty pool and
	//      the QC-channels' samples would never reach the mixer's
	//      Paint output. Census of precached + mixer-loaded sounds is
	//      emitted so the serial log proves the wiring took effect.
	if realHost != nil && realHost.SoundPool() != nil {
		runner.SoundPool = realHost.SoundPool()
		precached := 0
		for i := 1; i < len(realHost.Server.SoundPrecache); i++ {
			if realHost.Server.SoundPrecache[i] != "" {
				precached++
			}
		}
		mixerSamples := 0
		for _, s := range realHost.Sounds {
			if s != nil {
				mixerSamples++
			}
		}
		active := realHost.SoundPool().ActiveCount()
		fmt.Printf("QUAKE: mixer pool unified host<->runner -- %d sounds precached (wire), %d samples loaded (mixer), %d channels already active (ambient tracks parked at spawn time)\n",
			precached, mixerSamples, active)
	}

	// 11c-bis. Music driver wiring. Hand the runloop a pak-backed
	//          resolver for the canonical "music/track%02d.ogg" path
	//          + the pure-Go vorbis decoder factory. The svc_cdtrack
	//          arm in the client's Apply layer writes the wire bytes
	//          onto client.State.MusicTrack + MusicLoopTrack + bumps
	//          State.MusicEpoch; the runloop's per-tic tickMusic
	//          dispatch detects the epoch advance, opens the matching
	//          OGG, and mixes the decoded PCM on top of the SFX
	//          accumulator before QueueAudio. Missing tracks log ONCE
	//          per (track, loopTrack) pair so a shareware pak (which
	//          omits the music/ assets) stays boot-clean instead of
	//          spamming the QEMU serial stream on every signon.
	runner.MusicOpen = func(track int) ([]byte, bool) {
		if track <= 0 {
			return nil, false
		}
		// Canonical 2-digit zero-padded form: track01.ogg .. track99.ogg.
		path := fmt.Sprintf("%s%02d%s", enginemusic.PathPrefix, track, enginemusic.PathSuffix)
		// Try the pak first (user can override embedded tracks by
		// shipping music/trackXX.ogg inside pak0).
		if pakFS != nil {
			if blob, ok := tryReadPakFile(pakFS, path); ok && len(blob) > 64 {
				return blob, true
			}
		}
		// Fall back to the embedded id1/music/ tracks. The placeholder
		// files committed to embedmusic/ are 12 bytes (oggvorbis will
		// reject them, music.Streamer logs silent fallback); replacing
		// them with real id1/music/trackXX.ogg from the Quake archive
		// enables in-game music with no other change.
		blob, err := embedmusic.MusicFS.ReadFile(fmt.Sprintf("track%02d.ogg", track))
		if err != nil || len(blob) < 64 {
			return nil, false
		}
		return blob, true
	}
	runner.MusicDecoder = enginemusic.NewVorbisDecoder
	runner.MusicVolume = enginemusic.DefaultVolume
	runner.MusicMissingLog = func(track int) {
		fmt.Printf("QUAKE: music track%02d.ogg missing -- silent\n", track)
	}

	// 11d. Particle pool wiring. One 2048-slot pool per process; the
	//      runner steps it each tic (Pool.Run with sv_gravity = 800);
	//      the Pre2DDraw closure draws it after the alias entities;
	//      QC builtin #48 emits into it via the VM's particleSink
	//      bridge; the client's svc_particle + svc_temp_entity arms
	//      emit into it via the State.EmitParticles / .EmitTempEntity
	//      callbacks. A shared cheap-LCG-derived byte source feeds
	//      both Emit (which needs ~6 bytes per particle) and
	//      EmitTrail (which needs ~3 per step); using the gameplay
	//      random seed +0x01 keeps the streams decorrelated.
	particlePool := render.NewPool()
	particleRNG := newLCGByteSource(0xC0FFEF)
	runner.ParticlePool = particlePool
	runner.ParticleGravity = 800

	// QC builtin #48 -- particle(org, dir, color, count). Bridges the
	// VM into the pool via Pool.Emit (= R_RunParticleEffect). Without
	// this the SpawnBlood / TraceAttack puff-spawn QC calls every
	// gunshot makes are silent no-ops + the demo runs particle-free.
	if realHost != nil && realHost.VM != nil {
		realHost.VM.SetParticleSink(func(origin, dir [3]float32, color, count int) {
			particlePool.Emit(origin, dir, byte(color), count, float32(realHost.Server.Time), particleRNG)
		})
	}

	// svc_particle wire arm -- the same shape as the QC builtin but
	// driven directly by the server (e.g. flash-only effects the
	// progs.dat fires without going through PF_particle). Apply's
	// DecodedParticle arm now dispatches into this sink.
	clientState.EmitParticles = func(origin, dir [3]float32, color, count int) {
		now := clientState.MsgTime
		particlePool.Emit(origin, dir, byte(color), count, now, particleRNG)
	}

	// Load the s_explod.spr explosion sprite. id1 ships it at one of
	// two canonical paths (progs/ on early shareware, sprites/ on the
	// retail release); try both, and fall back to nil so the temp-
	// entity arm degrades to particles-only when the asset is missing.
	// The sprite count log goes out unconditionally so the QEMU serial
	// stream proves whether the bring-up has billboarded explosions
	// available or is running particles-only.
	explosionSprite, explosionPath := loadExplosionSprite(pakFS)
	spritesLoaded := 0
	if explosionSprite != nil {
		spritesLoaded = 1
	}
	fmt.Printf("QUAKE: sprites loaded %d (s_explod path=%q)\n",
		spritesLoaded, explosionPath)

	// Ephemeral client-side billboard pool. Holds in-flight explosion
	// sprites: TE_EXPLOSION spawns one, the per-tic Pre2DDraw walks
	// them via DrawSpriteAtTime. Spawning a sprite is additive on top
	// of the particle burst -- the upstream renders BOTH at impact
	// (R_ParticleExplosion fires the 1024-particle shower AND the
	// CL_NewTempEntity arm queues the s_explod.spr billboard).
	tempSpritePool := client.NewTempSpritePool()

	// Lightning-bolt alias models (bolt1/bolt2/bolt3.mdl, one per
	// TE_LIGHTNING* sub-type). Each is a 30-unit +X-aligned tile mesh
	// the per-tic Pre2DDraw chains end-to-end along the beam vector.
	// Missing entries (placeholder pak, or a build that ships only a
	// subset) silently degrade: the EmitBeam arm logs nothing + the
	// per-tic Walk renders the segments only when the matching mdl
	// resolved. tyrquake: cl_main.c precaches progs/bolt[1-3].mdl
	// statically; the Go port pulls them out of pak0 on demand.
	boltModels, boltSkins, boltLoaded := loadBoltModels(pakFS)
	fmt.Printf("QUAKE: bolt models loaded %d/3 (TE_LIGHTNING1/2/3 + TE_BEAM)\n", boltLoaded)

	// Client-side lightning-beam pool. tyrquake: cl_beams[MAX_BEAMS].
	// Holds in-flight bolts: each TE_LIGHTNING* / TE_BEAM message
	// spawns or extends a slot; the per-tic Pre2DDraw walks them via
	// BeamPool.Walk and draws each tile via DrawAliasInterpLit. The
	// pool ages out slots whose .die < now (BeamLifetime = 0.2 s).
	beamPool := client.NewBeamPool()

	// svc_temp_entity point-effect arm. Wire the bigger-burst pool
	// helpers (R_ParticleExplosion / R_LavaSplash / R_TeleportSplash)
	// per TE_* kind; the smaller dust-burst kinds (Spike / Gunshot /
	// SuperSpike / WizSpike / KnightSpike) collapse to Emit with the
	// upstream's canonical (count, color) shorthand.
	clientState.EmitTempEntity = func(kind int, origin [3]float32) {
		now := clientState.MsgTime
		switch kind {
		case protocol.TEExplosion, protocol.TETarExplosion:
			render.ParticleExplosion(particlePool, origin, now, particleRNG)
			// Stack the billboard on top when the sprite is present;
			// silent fall-back to particles-only otherwise.
			if explosionSprite != nil {
				tempSpritePool.Spawn(origin, now, 0)
			}
		case protocol.TELavaSplash:
			render.LavaSplash(particlePool, origin, now, particleRNG)
		case protocol.TETeleport:
			render.TeleportSplash(particlePool, origin, now, particleRNG)
		case protocol.TEGunshot:
			particlePool.Emit(origin, [3]float32{}, 0, 20, now, particleRNG)
		case protocol.TESpike, protocol.TESuperSpike:
			particlePool.Emit(origin, [3]float32{}, 0, 10, now, particleRNG)
		case protocol.TEKnightSpike:
			particlePool.Emit(origin, [3]float32{}, 226, 20, now, particleRNG)
		case protocol.TEWizSpike:
			particlePool.Emit(origin, [3]float32{}, 20, 30, now, particleRNG)
		}
	}
	fmt.Printf("QUAKE: particle pool wired -- cap=%d gravity=%v (QC #48 + svc_particle + svc_temp_entity)\n",
		render.MaxParticles, runner.ParticleGravity)

	// svc_temp_entity lightning arm. The wire decoder fires this for
	// TE_LIGHTNING1 / 2 / 3 / TE_BEAM with the owner entity + start +
	// end of the traceline the server emitted. We queue the bolt into
	// the client-side BeamPool; the per-tic Pre2DDraw walks it and
	// renders each 30-unit tile via DrawAliasInterpLit on the matching
	// bolt mdl. tyrquake: CL_ParseBeam in cl_tent.c -- the same shape.
	clientState.EmitBeam = func(kind, ent int, start, end [3]float32) {
		beamPool.Spawn(kind, ent, start, end, clientState.MsgTime)
	}
	fmt.Printf("QUAKE: beam pool wired -- cap=%d lifetime=%vs segment=%v (TE_LIGHTNING1/2/3 + TE_BEAM)\n",
		client.MaxBeams, client.BeamLifetime, client.BeamSegmentLength)

	// 12. Build the Pre2DDraw hook (BSP load, mark/walk contexts,
	//     synthetic texture, identity colormap) + anchor the camera
	//     origin at pickInMapCamera. The closure is wired onto the
	//     runner; RunUntilQuit then drives the full pipeline. When a
	//     real host exists we pass it in so the per-frame camera can
	//     follow the player edict at slot 1; nil falls back to the
	//     static pickInMapCamera anchor.
	//
	//     Alias models (monsters/items/players .mdl) are pre-loaded
	//     here from the authoritative server.Server.ModelPrecache so
	//     the Pre2DDraw hook can iterate State.Entities + dispatch
	//     render.DrawAlias per entity without re-opening the pak each
	//     tic. Server-side precache is identical to what the client
	//     mirrors over the wire (it IS the source the queued
	//     svc_serverinfo bytes carry), so loading from the server side
	//     avoids the chicken-and-egg of "client.State.ModelPrecache is
	//     not populated until the first client.Tick drains the queue".
	var (
		aliasPrecache []string
		aliasModels   []*mdl.Model
		aliasSkins    []*render.Pic
		aliasLoaded   int
		aliasNames    int
	)
	if realHost != nil {
		aliasPrecache = realHost.Server.ModelPrecache
	} else {
		aliasPrecache = clientState.ModelPrecache
	}
	aliasModels, aliasSkins, aliasLoaded, aliasNames = loadAliasModels(pakFS, aliasPrecache)
	fmt.Printf("QUAKE: alias models loaded: %d / %d names\n",
		aliasLoaded, aliasNames)

	// 12b. Load HUD/sbar assets. tyrquake's Sbar_Init walks a fixed list
	//      of gfx/*.lmp pic lumps; the loader returns a populated
	//      [render.SBarAssets] plus the count of slots that resolved +
	//      a short sample of missing names so the QEMU serial log proves
	//      which assets the pak shipped. nil pakFS -> empty bundle +
	//      DrawSBar's early-return-on-nil-BG keeps the renderer silent.
	sbarAssets, sbarLoaded, sbarTotal, sbarMissing := loadSBarAssets(pakFS)
	sample := sbarMissing
	if len(sample) > 8 {
		sample = sample[:8]
	}
	fmt.Printf("QUAKE: sbar -- loaded %d/%d assets, missing: %v\n",
		sbarLoaded, sbarTotal, sample)

	if err := setupRenderer(runner, pakFS, realHost, playerSlot, aliasPrecache, aliasModels, aliasSkins, sbarAssets, particleRNG, tempSpritePool, explosionSprite, beamPool, boltModels, boltSkins); err != nil {
		return fmt.Errorf("setupRenderer: %w", err)
	}

	// 12c. Wire the main menu. The engine boots into StateMain so the
	//      first frame the QEMU display surfaces is the title plaque,
	//      NOT the 3D scene -- matches the upstream cold-boot flow
	//      (M_Menu_Main_f called from Host_InitLocal). The runloop
	//      freezes the world pass while the menu is up + draws the
	//      menu overlay into r.FrameBuffer in the Pre2DDraw slot;
	//      pressing Enter on the skill picker transitions to
	//      StateNone + the next tic kicks the host into per-tic
	//      simulation. Esc-while-playing reopens the menu so a paused
	//      runloop is one keystroke away at any time.
	menuAssets, menuLoaded, menuTotal := loadMenuAssets(pakFS)
	fmt.Printf("QUAKE: menu -- loaded %d/%d assets, dots=%d\n",
		menuLoaded, menuTotal, len(menuAssets.MenuDots))
	runner.Menu = menu.New()
	runner.MenuAssets = menuAssets
	fmt.Printf("QUAKE: menu state=%s cursor=%d (boot lands on the main menu, world pass frozen until player picks Skill)\n",
		runner.Menu.State, runner.Menu.CursorIndex)

	// 12d. Wire the attract-loop demo. demo1.dem is the vanilla
	//      cold-boot teaser ("you can't quit, you can only quit later"
	//      narration tour through E1M1); when the pak ships it the
	//      runloop plays it under the title menu + restarts on EOF so
	//      the player sees motion behind the overlay. Any KeyDown
	//      event halts the demo (vanilla "any key drops you out of the
	//      attract loop"). With no pak (placeholder build) the demo
	//      slot is left nil + the runloop falls back to the menu-only
	//      title screen.
	if pakFS != nil {
		if d := loadAttractDemo(pakFS, "demo1.dem"); d != nil {
			d.PlayerOpts = demo.PlayerOpts{
				Protocol:       protocol.VersionNQ,
				TickDelta:      1.0 / 20.0,
				SkipUnknownSvc: true,
			}
			d.OnFrame = logDemoFrame
			runner.Demo = d
			fmt.Printf("QUAKE: attract-loop demo wired -- source=%q\n", "demo1.dem")
		} else {
			fmt.Printf("QUAKE: attract-loop demo skipped -- demo1.dem not in pak\n")
		}
	} else {
		fmt.Printf("QUAKE: attract-loop demo skipped -- no pak available\n")
	}

	fmt.Printf("QUAKE: entering RunUntilQuit (realHost=%v)\n", realHost != nil)
	return runner.RunUntilQuit()
}

// loadAttractDemo opens name inside pakFS + builds a [runloop.Demo]
// with a Restart closure that re-opens the same entry on io.EOF (so
// the attract loop runs forever in the background). Returns nil when
// the entry is missing OR malformed -- the runloop's nil-Demo path
// degrades gracefully to a static title screen.
func loadAttractDemo(pakFS fs.FS, name string) *runloop.Demo {
	open := func() (*demo.Reader, error) {
		data, ok := tryReadPakFile(pakFS, name)
		if !ok {
			return nil, fmt.Errorf("attract demo %q not found in pak", name)
		}
		return demo.NewReader(bytes.NewReader(data))
	}
	rd, err := open()
	if err != nil {
		return nil
	}
	return &runloop.Demo{
		Reader:  rd,
		Restart: open,
	}
}

// logDemoFrame is the [runloop.Demo.OnFrame] callback the embed wires
// so the QEMU serial trace surfaces actual per-tic demo progression
// without flooding the log (printed every 60 tics, ~once per second
// at the 20-Hz default tic rate). Headless validation runs grep for
// "QUAKE: demo playback frame" to prove the attract loop is alive.
func logDemoFrame(frame int, angles [3]float32) {
	if frame%60 == 0 {
		fmt.Printf("QUAKE: demo playback frame %d viewAngles=%v\n", frame, angles)
	}
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
func setupRenderer(runner *runloop.Runner, pakFS fs.FS, realHost *enginehost.Host, playerSlot int, aliasPrecache []string, aliasModels []*mdl.Model, aliasSkins []*render.Pic, sbarAssets *render.SBarAssets, particleRNG func() byte, tempSpritePool *client.TempSpritePool, explosionSprite *enginespr.Sprite, beamPool *client.BeamPool, boltModels [3]*mdl.Model, boltSkins [3]*render.Pic) error {
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
	miptexPics, miptexNames, loaded, total, err := loadMiptexPicsNamed(file)
	if err != nil {
		return fmt.Errorf("loadMiptexPics: %w", err)
	}
	fmt.Printf("QUAKE: loaded %d miptexes from BSP (total slots: %d, loaded: %d, null: %d)\n",
		loaded, total, loaded, total-loaded)
	// Census the special-surface population for the boot log so we
	// can confirm sky + liquid dispatch will actually fire on this map.
	var nSky, nTurb int
	for _, n := range miptexNames {
		switch {
		case strings.HasPrefix(n, "sky"):
			nSky++
		case strings.HasPrefix(n, "*"):
			nTurb++
		}
	}
	fmt.Printf("QUAKE: miptex specials -- sky=%d liquid=%d\n", nSky, nTurb)

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

	// 5b. Demo-orbit waypoints. The headless QEMU validation harness
	//     boots with virtio-input absent so client.Tick can't refresh
	//     viewAngles or move the player edict -- without this lattice
	//     every screendump would be the same shot. Each waypoint is
	//     guaranteed-in-a-leaf via PointInLeaf; demoOrbit=false (the
	//     interactive path) skips the build + the per-tic override.
	var demoWaypoints [][3]float32
	if demoOrbit && !isSynth {
		demoWaypoints = buildDemoWaypoints(bm, file, camOrigin)
		fmt.Printf("QUAKE: demoOrbit ON -- %d waypoints + 1-degree-per-frame yaw spin\n",
			len(demoWaypoints))
		for i, wp := range demoWaypoints {
			fmt.Printf("QUAKE:   waypoint[%d] = %v\n", i, wp)
		}
	}

	markCtx := bsprender.NewMarkContext(bm)
	var surfaces bsprender.SurfaceList
	frameCount := 0
	// prevEntityOrigin tracks the last-seen Origin per entity slot so
	// per-tic rocket/grenade/gib trails can emit one particle stream
	// per cm of motion between ticks. Mirrors tyrquake's per-entity
	// trail_origin field in CL_LinkEntities -- on each tic the
	// closure asks `if model is trail-bearing AND prevOrigin known`,
	// emits a Pool.EmitTrail from prevOrigin to current Origin, then
	// updates prevOrigin. Map keys are entity numbers (matches the
	// client State.Entities key space).
	prevEntityOrigin := make(map[int][3]float32)
	// loggedWireSpawn latches the one-shot "server-side Spawned
	// observed true" trace so the line appears at most once per
	// process lifetime. Since the per-tic SendEntityUpdates broadcast
	// regression fix, hc.Spawned is pre-poked true in run() right
	// after the signon prefix queueing (see the inline rationale at
	// the poke site); the wire "spawn" clc_stringcmd still flips the
	// SAME field a tic or two later (idempotent re-flip). So this
	// closure observes Spawned=true at tic 0 -- the trace continues
	// to serve as a single-line "the server-side client struct is
	// alive + ready for per-tic broadcast" smoke signal.
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

		// Auto-disable demo-orbit on first observed input. When the
		// virtio-keyboard is wired the runloop fills runner.Buttons +
		// runner.Triggers BEFORE Pre2DDraw runs; the first non-zero
		// state (any movement key held or pressed this frame, or any
		// trigger-button held) means a human is driving and the
		// demo-orbit override should yield to the real player camera.
		// The flag latches once and stays off for the rest of the
		// process lifetime.
		if demoOrbit && demoOrbitAutoDisableOnInput && observedAnyInput(runner) {
			demoOrbit = false
			fmt.Printf("QUAKE: demo-orbit auto-disabled at tic %d (input observed -- player takes over)\n", frame)
		}

		// Demo-orbit override: with no input device the wire-mirrored
		// viewAngles never moves, so the headless QEMU run would
		// capture the same frame forever. The override winds Yaw at
		// one degree per frame + (optionally) steps viewOrigin
		// through demoWaypoints every demoWaypointPeriodSeconds of
		// sv.time so multi-shot screendump runs expose geometrically
		// distinct parts of the map (different miptex sets, different
		// alias entities visible per shot). The waypoint selector is
		// time-based (sv.time / period) rather than frame-based so
		// the dwell stays consistent across TCG (~1 fps) + hardware
		// (~60+ fps) -- the capture_waypoints.sh harness depends on
		// this to land one PPM cleanly inside each waypoint window.
		waypointIdx := -1
		if demoOrbit && len(demoWaypoints) > 0 {
			svTime := float32(0)
			if realHost != nil {
				svTime = float32(realHost.Server.Time)
			}
			waypointIdx = int(svTime/demoWaypointPeriodSeconds) % len(demoWaypoints)
			if waypointIdx < 0 {
				waypointIdx = 0
			}
			viewOrigin = demoWaypoints[waypointIdx]
			viewAngles = [3]float32{
				0, // pitch
				float32(frame % demoYawPeriodFrames),
				0, // roll
			}
			// Per 60-frame cadence matches the existing audio/
			// surface-count log just below; demo-orbit progression
			// joins the same line set so the serial timeline stays
			// scannable. On QEMU TCG (≈1 frame/s without KVM) the
			// log fires roughly once per minute; on hardware (~60+
			// frames/s) it's once per second.
			if frame%60 == 0 {
				fmt.Printf("QUAKE: demo-orbit tic %d -- waypoint[%d]=%v yaw=%v\n",
					frame, waypointIdx, viewOrigin, viewAngles[1])
			}
		}

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

		// Publish the listener basis to the host so per-tic QC-driven
		// sound dispatch (builtinSound / builtinAmbientSound) can drive
		// sound.Spatialize via [enginehost.Host.StartSoundAt] /
		// [enginehost.Host.AmbientSoundAt]. The right axis is the
		// second return of mathlib.AngleVectors -- the listener's right
		// ear projection that determines stereo balance. tyrquake's
		// S_Update updates the listener every frame via
		// AngleVectors(cl.viewangles, ...); this is the equivalent.
		if realHost != nil {
			_, lright, _ := mathlib.AngleVectors(mathlib.Vec3(viewAngles))
			realHost.SetListener(origin, [3]float32(lright))
		}

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

		// One-shot Spawned trace. With the per-tic broadcast fix in
		// place hc.Spawned is pre-poked true before the loop starts
		// (so SendEntityUpdates emits on tic 0 -- see the run() poke
		// site for the rationale); the wire "spawn" clc_stringcmd
		// still re-flips the same field idempotently a tic later.
		// The log fires the first tic the closure observes the flag
		// as a single-line "server-side client struct ready for per-
		// tic broadcast" smoke signal.
		if !loggedWireSpawn && realHost != nil && playerSlot > 0 {
			if c := realHost.Static.Clients[playerSlot-1]; c != nil && c.Spawned {
				fmt.Printf("QUAKE: server-side Spawned observed true (tic %d, slot %d) -- per-tic svc_update broadcast enabled\n",
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
			soundsStarted := 0
			ambientsStarted := 0
			if realHost != nil {
				soundsStarted = realHost.LastSoundsStarted
				ambientsStarted = realHost.LastAmbientsStarted
			}
			activeParticles := 0
			if runner.ParticlePool != nil {
				activeParticles = runner.ParticlePool.NumAlive
			}
			fmt.Printf("QUAKE: tic %d -- viewOrigin=%v src=%s entOrigin=%v entPresent=%v (PlayerNum=%d, %d entities cached) viewAngles=%v cmd.fwd=%v cmd.side=%v clientConn=%d cl.vel=%v cl.viewh=%v cl.health=%d; %d surfaces; audio: %d active, %d mixed; sounds_started=%d ambients_started=%d; particles: %d active\n",
				frame, origin, viewSrc, entOrigin, entPresent,
				runner.Client.PlayerNum, len(runner.Client.Entities),
				viewAngles, cmdFwd, cmdSide,
				int(runner.Client.Connection),
				runner.Client.Velocity, runner.Client.ViewHeightOffset, runner.Client.Health,
				surfaces.Len(),
				active, enginesound.MixBufferStereoFrames,
				soundsStarted, ambientsStarted,
				activeParticles)

			// Sample spatialized channel: scan the dynamic + reserved
			// channel pools for the FIRST active channel and log its
			// (L, R) per-ear volumes. With Spatialize wired the values
			// should diverge whenever the source isn't directly in
			// front of the listener (centered = balanced; right of
			// listener -> R > L; behind/far -> both attenuated).
			// Without the listener basis OR with AttenuationNone the
			// fall-through path leaves L == R (= master volume).
			if realHost != nil && realHost.SoundPool() != nil {
				pool := realHost.SoundPool()
				for i := range pool.Channels {
					if pool.Channels[i].Sfx == nil {
						continue
					}
					ch := &pool.Channels[i]
					fmt.Printf("QUAKE: spatialize sample tic %d -- ch[%d] ent=%d L=%d R=%d master=%v\n",
						frame, i, ch.EntNum, ch.LeftVol, ch.RightVol, ch.Master)
					break
				}
			}

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

		// Per-tic SV_RunThink dispatch + .frame census. Fires on a
		// sparse "early then per-30" cadence so the serial log
		// captures (a) the post-spawn baseline (tic 0), (b) the
		// first few tics where deadlines accumulate, (c) a periodic
		// snapshot. Surfaces the SV_RunThink wire as a dispatch
		// count and a .frame-distribution shift between snapshots.
		// The .frame field is what monster QC think functions write
		// on every per-tic dispatch to step through their animation
		// cycle; a non-zero "dispatched" count + a growing
		// max-frame value across snapshots proves the SV_RunThink
		// walker is firing the QC bodies that animate monsters.
		if realHost != nil && (frame < 12 || frame%30 == 0) {
			fmt.Printf("QUAKE: thinks tic %d -- %d dispatched, %d errored (missing builtins are non-fatal)\n",
				frame, realHost.LastThinksDispatched, realHost.LastThinkErrors)
			// Surface the first 8 unique error strings runThink swallowed
			// this tic so the serial log names the failing builtin index
			// instead of just counting (turns the count into actionable
			// "stub THIS index" signal).
			for _, msg := range realHost.LastThinkErrorMsgs {
				fmt.Printf("QUAKE: think error -- %s\n", msg)
			}
			// Touch-trigger walk counters: per-tic count of QC
			// .touch dispatches the post-RunPhysics SV_TouchLinks-
			// equivalent walk fired, plus any per-dispatch error
			// the walker swallowed. A non-zero "touches" count the
			// moment the demo orbit grazes an item proves item
			// pickup is wired end-to-end (item_*_touch invoked ->
			// QC writes self.ammo_*/.health on the player + calls
			// setorigin(-8000) to remove the item).
			fmt.Printf("QUAKE: touches tic %d -- %d dispatched, %d errored\n",
				frame, realHost.LastTriggerTouches, realHost.LastTouchErrors)
			for _, msg := range realHost.LastTouchErrorMsgs {
				fmt.Printf("QUAKE: touch error -- %s\n", msg)
			}
			if p := realHost.Progs(); p != nil {
				base := realHost.Static.MaxClients + 1
				scheduled := 0
				framesAdvanced := 0
				maxFrame := float32(0)
				minNext := float32(0)
				sample := ""
				for i := base; i < realHost.Server.NumEdicts; i++ {
					e := realHost.Server.Edicts[i]
					if e == nil || e.Free {
						continue
					}
					ev, evErr := progs.NewEntVars(p, e)
					if evErr != nil {
						continue
					}
					nt, _ := ev.ReadFloat("nextthink")
					f, _ := ev.ReadFloat("frame")
					if nt > 0 {
						scheduled++
						if minNext == 0 || nt < minNext {
							minNext = nt
						}
					}
					if f > 0 {
						framesAdvanced++
						if f > maxFrame {
							maxFrame = f
						}
						if sample == "" {
							th, _ := ev.ReadInt32("think")
							sample = fmt.Sprintf(" first-with-frame=[slot=%d frame=%.0f nextthink=%.3f think=%d]",
								i, f, nt, th)
						}
					}
				}
				fmt.Printf("QUAKE: think-census tic %d sv.time=%.3f -- %d edicts with future nextthink (soonest=%.3f), %d edicts with frame>0 (max=%.0f)%s\n",
					frame, realHost.Server.Time, scheduled, minNext, framesAdvanced, maxFrame, sample)
			}
		}

		// Rasterize each visible face. Per-face dispatch on the miptex
		// name prefix:
		//
		//   "sky*" -> two-layer sky composite (FillSkyPolygon). The
		//             cloud overlay scrolls 2x the dome, both wrap
		//             modulo the texture half-width.
		//   "**"   -> liquid (water/lava/slime) warp via
		//             FillTurbulentPolygon. Per-pixel UV is offset
		//             by an LUT-sampled sin amplitude (TurbScale=8
		//             texels) keyed off the wall-clock so the surface
		//             ripples frame-to-frame.
		//   else   -> the stock affine FillTexturedPolygon path.
		//
		// Faces that resolve to a null miptex slot OR a synthetic BSP
		// with no Textures lump fall back to the checker + the plain
		// textured path (the dispatch only fires for real miptex hits).
		skyTimeSec := float32(realHost.Server.Time)
		turbTimeSec := float32(realHost.Server.Time)
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
			var name string
			if mtIdx, ok, _ := bm.FaceMipTexIdx(ref.FaceIdx); ok && mtIdx >= 0 && mtIdx < len(miptexPics) {
				if p := miptexPics[mtIdx]; p != nil {
					tex = p
					name = miptexNames[mtIdx]
				}
			}
			switch {
			case strings.HasPrefix(name, "sky"):
				_ = render.FillSkyPolygon(fb, tex, verts, skyTimeSec)
			case strings.HasPrefix(name, "*"):
				_ = render.FillTurbulentPolygon(fb, tex, &cm, 0, verts, turbTimeSec)
			default:
				_ = render.FillTexturedPolygon(fb, tex, &cm, 0, verts)
			}
		}

		// Per-tic projectile trail emission. Walk the wire-mirrored
		// State.Entities map; for each entity whose ModelIdx resolves
		// to a precache name flagged with a trail (rocket = grenade
		// smoke trail, gib = blood drip, knight/scrag = tracer line,
		// Voor = sucking-orb trail), emit Pool.EmitTrail between the
		// previous-tic origin and the current origin. Skips entities
		// we have no prior-origin for (first observation -- the next
		// tic seeds the trail). tyrquake equivalent: the per-entity
		// EF_ROCKET / EF_GRENADE / EF_GIB / EF_ZOMGIB / EF_TRACER /
		// EF_TRACER2 / EF_TRACER3 dispatch inside CL_LinkEntities ->
		// R_RocketTrail.
		trailNow := runner.Client.MsgTime
		if realHost != nil && runner.ParticlePool != nil {
			precache := realHost.Server.ModelPrecache
			seenThisTic := make(map[int]struct{}, len(runner.Client.Entities))
			for entNum, es := range runner.Client.Entities {
				seenThisTic[entNum] = struct{}{}
				if es.ModelIdx <= 0 || es.ModelIdx >= len(precache) {
					continue
				}
				kind, ok := trailKindForModel(precache[es.ModelIdx])
				if !ok {
					continue
				}
				prev, hadPrev := prevEntityOrigin[entNum]
				prevEntityOrigin[entNum] = es.Origin
				if !hadPrev {
					continue
				}
				runner.ParticlePool.EmitTrail(prev, es.Origin, kind, trailNow, particleRNG)
			}
			// Garbage-collect prevOrigin entries whose entity slot no
			// longer exists in the live map (entity freed / detonated /
			// out-of-PVS). Keeps the map size bounded.
			for k := range prevEntityOrigin {
				if _, ok := seenThisTic[k]; !ok {
					delete(prevEntityOrigin, k)
				}
			}
		}

		// Alias-model pass. Iterate the wire-mirrored State.Entities
		// (the client's per-tic snapshot the server broadcast over
		// svc_update) and rasterize one DrawAliasInterpLit per entity
		// whose ModelIdx resolves to a loaded .mdl. Entries with
		// ModelIdx == 0 (no model) or aliasModels[ModelIdx] == nil
		// (BSP submodel like "*1", or a missing/un-loadable .mdl) are
		// silently skipped -- BSP submodels are already drawn by the
		// world walk above, alien-format / missing entries have
		// nothing to render here.
		//
		// SCOPE: smooth-animation draw + per-vertex gouraud shading
		// fused via [render.DrawAliasInterpLit]. The client cache
		// carries the entity's prior Frame (PrevFrame) + the wall-
		// clock stamp at which Frame last changed (LerpStartTime).
		// The per-tic lerp fraction is
		//
		//	lerp = clamp((now - LerpStartTime) / aliasFramePeriod, 0, 1)
		//
		// with aliasFramePeriod = 0.1 s (10 Hz upstream cadence). The
		// combined variant takes the AliasShadeRange that DrawAliasLit
		// consumes -- the gourand shading from batch 79 is restored on
		// top of the frame interpolation from batch 80, so monsters now
		// animate smoothly AND shade per-face. Shade envelope: Ambient
		// 0.3 (unlit side stays visible) + DirectMax 0.7 (lit side
		// adds up to +0.7) + LightDir (0,0,-1) (light from above) --
		// same parameters as the pre-interp DrawAliasLit path.
		// Skin texture: the per-entity SkinNum field is clamped to
		// the model's available skins via the parallel aliasSkins
		// slice (one *render.Pic per precache slot -- a single-skin
		// model exposes its sole skin, multi-skin variants land in a
		// future batch). On a render error we log + continue with the
		// next entity so one bad entity doesn't sink the rest of the
		// frame.
		const (
			aliasFramePeriod = float32(0.1) // 10 Hz upstream cadence
		)
		aliasShade := render.AliasShadeRange{
			Ambient:   0.3,
			DirectMin: 0.0,
			DirectMax: 0.7,
			LightDir:  [3]float32{0, 0, -1},
		}
		now := runner.Client.MsgTime
		aliasRendered := 0
		var (
			sampleES   client.EntityState
			sampleAM   *mdl.Model
			haveSample bool
		)
		for _, es := range runner.Client.Entities {
			if es.ModelIdx <= 0 || es.ModelIdx >= len(aliasModels) {
				continue
			}
			am := aliasModels[es.ModelIdx]
			if am == nil {
				continue
			}
			skin := aliasSkins[es.ModelIdx]
			if skin == nil {
				skin = fallbackTex
			}
			frameIdx := es.Frame
			if frameIdx < 0 || frameIdx >= len(am.Frames) {
				frameIdx = 0
			}
			prevIdx := es.PrevFrame
			if prevIdx < 0 || prevIdx >= len(am.Frames) {
				prevIdx = frameIdx
			}
			// Compute lerp fraction. A zero LerpStartTime means the
			// renderer is racing the first applyUpdate (no frame
			// transition yet observed) -- clamp to 0 so the draw
			// degenerates to a single-frame snapshot.
			var lerp float32
			if es.LerpStartTime > 0 && now > es.LerpStartTime {
				lerp = (now - es.LerpStartTime) / aliasFramePeriod
				if lerp < 0 {
					lerp = 0
				} else if lerp > 1 {
					lerp = 1
				}
			}
			ent := render.AliasEntityInterp{
				AliasEntity: render.AliasEntity{
					Origin:     es.Origin,
					AnglePitch: es.Angles[0],
					AngleYaw:   es.Angles[1],
					AngleRoll:  es.Angles[2],
					FrameIdx:   prevIdx,
					SkinIdx:    es.SkinNum,
				},
				FrameIdxNext: frameIdx,
				Lerp:         lerp,
			}
			if err := render.DrawAliasInterpLit(fb, rd, &cm, aliasShade, am, skin, ent); err != nil {
				// Per-entity errors are non-fatal; log sparsely so
				// one bad entity per tic doesn't drown the channel.
				if frame%60 == 0 {
					fmt.Printf("QUAKE: DrawAliasInterpLit modelIdx=%d from=%d to=%d lerp=%v err: %v\n",
						es.ModelIdx, prevIdx, frameIdx, lerp, err)
				}
				continue
			}
			if !haveSample {
				sampleES = es
				sampleAM = am
				haveSample = true
			}
			aliasRendered++
		}
		if frame%60 == 0 {
			fmt.Printf("QUAKE: tic %d rendered %d alias entities (interp+lit)\n",
				frame, aliasRendered)
			// Spot-check BOTH the interp lerp window AND the per-vertex
			// shade variance for a representative entity, so the serial
			// log surfaces whether the combined DrawAliasInterpLit
			// path is actually firing:
			//   - lerp > 0 + prev != cur = pose blend is active
			//   - distinct > 1 + min != max = gouraud shading varies
			// (Both flat = collapse to single-frame + uniform light.)
			if haveSample {
				var sampleLerp float32
				if sampleES.LerpStartTime > 0 && now > sampleES.LerpStartTime {
					sampleLerp = (now - sampleES.LerpStartTime) / aliasFramePeriod
					if sampleLerp > 1 {
						sampleLerp = 1
					}
				}
				fmt.Printf("QUAKE: alias interp sample modelIdx=%d frames=%d prev=%d cur=%d lerpStart=%v now=%v lerp=%v\n",
					sampleES.ModelIdx, len(sampleAM.Frames),
					sampleES.PrevFrame, sampleES.Frame,
					sampleES.LerpStartTime, now, sampleLerp)
				// Shade variance summary across all vertices of the
				// sample entity's current frame pose (mirrors batch
				// 79's QUAKE: alias shade sample log).
				fIdx := sampleES.Frame
				if fIdx < 0 || fIdx >= len(sampleAM.Frames) {
					fIdx = 0
				}
				verts := render.FramePose(sampleAM.Frames[fIdx])
				if lights, err := render.ComputeAliasVertexLights(verts, aliasShade); err == nil && len(lights) > 0 {
					lmin, lmax := lights[0], lights[0]
					seen := make(map[int]struct{}, len(lights))
					for _, v := range lights {
						if v < lmin {
							lmin = v
						}
						if v > lmax {
							lmax = v
						}
						seen[v] = struct{}{}
					}
					fmt.Printf("QUAKE: alias shade sample modelIdx=%d verts=%d distinct=%d min=%d max=%d\n",
						sampleES.ModelIdx, len(lights), len(seen), lmin, lmax)
				}
			}
		}

		// Particle pass. Runs AFTER alias entities (so monsters don't
		// paint over particle bursts they triggered, e.g. blood from a
		// player gibbing) and BEFORE the HUD/sbar pass (so particles
		// land in the world layer, not on top of the status bar).
		// DrawParticleQuads early-returns on a nil pool / rd; pool was
		// wired at runner-build time so nil is a structural impossibility
		// here, but the no-op guard keeps the call site safe regardless.
		if err := render.DrawParticleQuads(fb, rd, runner.ParticlePool, runner.Client.MsgTime); err != nil {
			if frame%60 == 0 {
				fmt.Printf("QUAKE: DrawParticleQuads err: %v\n", err)
			}
		}

		// Temp-sprite pass. Walks the explosion-billboard pool and
		// blits each live sprite via DrawSpriteAtTime. Runs AFTER the
		// particle pass so the bright sprite flash overlays the
		// scatter-shower (the upstream draw order in
		// R_DrawViewModel + R_DrawSpriteModel).  Skips silently when
		// the sprite is nil (asset missing -- the temp-sprite pool is
		// also kept empty by the EmitTempEntity arm in that case).
		//
		// The walk ALSO ages out expired slots even when explosionSprite
		// is nil, so a future hot-reload that swaps the sprite in
		// mid-run starts from a clean pool.
		spritesDrawn := 0
		tempSpritePool.Walk(now, func(origin [3]float32, elapsed float32) {
			if explosionSprite == nil {
				return
			}
			if err := render.DrawSpriteAtTime(fb, rd, explosionSprite, origin, elapsed); err != nil {
				if frame%60 == 0 {
					fmt.Printf("QUAKE: DrawSpriteAtTime err: %v\n", err)
				}
				return
			}
			spritesDrawn++
		})

		// Lightning-beam pass. Walks the BeamPool and draws one alias
		// model per 30-unit tile of every live bolt via
		// DrawAliasInterpLit. Per-tile (yaw, pitch) come from
		// BeamSegments so each bolt mdl visually points end-ward; the
		// (FrameIdx, Lerp) pair is pinned to frame 0 because bolt
		// models are single-frame meshes (no animation to interp).
		// Skips silently when the matching bolt mdl is nil (asset
		// missing in this pak); the Walk still ages the slot so a
		// future hot-reload starts from a clean pool. Runs AFTER the
		// temp-sprite pass so the bright bolt overlays any concurrent
		// explosion. tyrquake: CL_UpdateBeams in cl_tent.c.
		beamsDrawn := 0
		beamSegmentsDrawn := 0
		beamPool.Walk(now, func(seg client.BeamSegment) {
			var bm *mdl.Model
			var bskin *render.Pic
			switch seg.Kind {
			case protocol.TELightning1:
				bm, bskin = boltModels[0], boltSkins[0]
			case protocol.TELightning2, protocol.TEBeam:
				bm, bskin = boltModels[1], boltSkins[1]
			case protocol.TELightning3:
				bm, bskin = boltModels[2], boltSkins[2]
			default:
				return
			}
			if bm == nil {
				return
			}
			if bskin == nil {
				bskin = fallbackTex
			}
			ent := render.AliasEntityInterp{
				AliasEntity: render.AliasEntity{
					Origin:     seg.Origin,
					AnglePitch: seg.Pitch,
					AngleYaw:   seg.Yaw,
					AngleRoll:  0,
					FrameIdx:   0,
					SkinIdx:    0,
				},
				FrameIdxNext: 0,
				Lerp:         0,
			}
			if err := render.DrawAliasInterpLit(fb, rd, &cm, aliasShade, bm, bskin, ent); err != nil {
				if frame%60 == 0 {
					fmt.Printf("QUAKE: DrawAliasInterpLit(bolt kind=%d) err: %v\n", seg.Kind, err)
				}
				return
			}
			beamSegmentsDrawn++
			if seg.Index == 0 {
				beamsDrawn++
			}
		})
		if frame%60 == 0 {
			fmt.Printf("QUAKE: lightning beams active=%d segments=%d\n",
				beamsDrawn, beamSegmentsDrawn)
		}

		// Per-tic in-flight projectile census. Counts entities whose
		// model name (looked up through the alias precache the alias
		// loader walked) maps to a projectile trail kind -- rockets,
		// grenades, spikes, lasers, voorballs. The number is the
		// renderer-visible projectile population at the top of the
		// frame, the proof that the alias pipeline IS rendering the
		// in-flight missiles (the existing per-entity DrawAliasInterpLit
		// pass above already draws them; this counter surfaces the
		// census so the QEMU serial log proves the route).
		if frame%60 == 0 {
			missiles := 0
			for _, es := range runner.Client.Entities {
				if es.ModelIdx <= 0 || es.ModelIdx >= len(aliasPrecache) {
					continue
				}
				if _, ok := trailKindForModel(aliasPrecache[es.ModelIdx]); ok {
					missiles++
				}
			}
			fmt.Printf("QUAKE: tic %d missiles in flight: %d, explosion sprites: %d/%d (drawn/alive)\n",
				frame, missiles, spritesDrawn, tempSpritePool.NumAlive(now))
		}

		// HUD/sbar pass. Runs AFTER the alias entities (so monsters /
		// items don't paint over the status bar) and BEFORE Compose2D
		// (so the 2D notify + console layers still overlay on top of
		// the bar -- matches the upstream draw order in SCR_UpdateScreen).
		// DrawSBar early-returns on nil assets so the placeholder-pak
		// boot path (no real gfx/sbar.lmp) silently skips.
		if sbarAssets != nil {
			if err := render.DrawSBar(fb, runner.Client, sbarAssets); err != nil {
				if frame%60 == 0 {
					fmt.Printf("QUAKE: DrawSBar err: %v\n", err)
				}
			}
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
	pics, names, loaded, total, err := loadMiptexPicsNamed(file)
	_ = names // kept for the dispatch path below
	return pics, loaded, total, err
}

// loadMiptexPicsNamed is the named-bearing variant. The parallel
// `names` slice exposes the raw miptex name (16-byte slot, NUL-
// trimmed) at each slot so the per-face dispatch can branch on the
// upstream texture-name conventions:
//
//   - leading "sky"  -> two-layer sky composite (FillSkyPolygon)
//   - leading "*"    -> sinusoidal water/lava warp (FillTurbulentPolygon)
//   - everything else -> the stock affine FillTexturedPolygon path
//
// Empty string for missing / null slots (so the dispatch fall-through
// to fallbackTex still works cleanly).
func loadMiptexPicsNamed(file *bspfile.File) ([]*render.Pic, []string, int, int, error) {
	mtl, err := file.Textures()
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("file.Textures: %w", err)
	}
	total := int(mtl.NumMipTex)
	pics := make([]*render.Pic, total)
	names := make([]string, total)
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
		pics[i] = &render.Pic{
			Width:  int(mt.Width),
			Height: int(mt.Height),
			Pixels: buf,
		}
		names[i] = mt.Name
		loaded++
	}
	return pics, names, loaded, total, nil
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

	// 3a. Sound loader: closure over the embedded pakFS so the QC
	//     precache_sound + ambientsound builtins can resolve WAV blobs
	//     by canonical "sound/<n>.wav" path. Without this hook, every
	//     spawn-time precache_sound call surfaces ErrSoundLoadFailed
	//     (the host's PrecacheSound logs + continues; the wire-side
	//     precache slot is still filled so the missing-mixer-sample
	//     path is the only fallout -- the slot just won't audibly fire
	//     later).
	h.SetSoundLoader(func(name string) ([]byte, bool) {
		return tryReadPakFile(pakFS, name)
	})

	// 3b. Mixer pool: pre-allocate BEFORE SpawnServer runs so the
	//     spawn-time QC's sound() + ambientsound() builtins land
	//     channels on a real pool. Without this the spawn pass logs
	//     "no sound pool wired" for every static ambient sound the
	//     map's worldspawn function fires (water1, wind, ...). The
	//     SAME pointer is later handed to the runner (in run()) so
	//     the runloop's per-tic Paint walks the same channel bank.
	//     The 8-static-slot count matches the runloop's existing
	//     default for ambient (level-ambience) sources.
	pool, perr := enginesound.NewPool(8)
	if perr != nil {
		return nil, fmt.Errorf("buildHost: NewPool: %w", perr)
	}
	h.SetSoundPool(pool)

	// 4. Builtin table. RegisterMathBuiltins wires the 10 pure-math
	//    builtins (makevectors / normalize / vlen / vectoangles /
	//    random / ...); registerSpawnTimeBuiltins layers no-op stubs
	//    on top of every
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
	if err := registerSpawnTimeBuiltins(vm, h); err != nil {
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
// EXCEPTION: precache_model + setmodel ship REAL implementations
// (see [builtinPrecacheModel] / [builtinSetModel]). Without those
// two the Server.ModelPrecache slice stays empty + every entity's
// .modelindex stays zero, which means the post-spawn alias-render
// pass [setupRenderer]'s Pre2DDraw closure sees ModelIdx == 0 for
// every entity + draws nothing. The two real impls give the
// renderer real .mdl names to walk + real per-edict indices to
// dispatch by.
//
// EXCEPTION 2: traceline + findradius ship REAL implementations
// (see [builtinTraceLine] / [builtinFindRadius]). Monster QC's
// FindTarget loop calls findradius(self.origin, 1000) every think
// and traceline(self.origin, target.origin, false, self) to check
// the sightline -- both as no-ops returned an empty chain + an
// inside-solid trace, so every monster's FindTarget short-circuited
// + the idle->stand->walk transition never fired. Wiring the real
// impls against the world brushmodel + the area-tree unblocks
// monster AI's first wake-up cycle.
//
// Functional builtins (the math 9 from RegisterMathBuiltins) stay
// real; the rest of the spawn-time side-effect builtins are stubbed
// here. A real implementation would precache sounds, link entities
// into the world tree, etc.; for "prove the SpawnFn dispatch works"
// the no-op shape is sufficient + safer than a half-port that crashes.
func registerSpawnTimeBuiltins(vm *progs.VM, h *enginehost.Host) error {
	noop := func(_ *progs.VM) error { return nil }
	// makevectors is NOT registered here: RegisterMathBuiltins
	// (the prior call in buildHost) wires the real
	// [progs.BuiltinFnMakeVectors] against v_forward / v_right /
	// v_up. Overwriting it with a no-op here would silently break
	// W_FireShotgun's aim basis -- every traceline (src, src +
	// v_forward * 2048, ...) would collapse to a zero-length ray.
	vm.RegisterBuiltin(progs.BuiltinSetOrigin, builtinSetOrigin(h))
	vm.RegisterBuiltin(progs.BuiltinSetModel, builtinSetModel(h))
	vm.RegisterBuiltin(progs.BuiltinSetSize, noop)
	vm.RegisterBuiltin(progs.BuiltinBreak, noop)
	vm.RegisterBuiltin(progs.BuiltinSound, builtinSound(h))
	vm.RegisterBuiltin(progs.BuiltinError, noop)
	vm.RegisterBuiltin(progs.BuiltinObjError, noop)
	vm.RegisterBuiltin(progs.BuiltinSpawn, noop)
	vm.RegisterBuiltin(progs.BuiltinRemove, noop)
	vm.RegisterBuiltin(progs.BuiltinTraceLine, builtinTraceLine(h))
	vm.RegisterBuiltin(progs.BuiltinCheckClient, noop)
	vm.RegisterBuiltin(progs.BuiltinFind, noop)
	vm.RegisterBuiltin(progs.BuiltinPrecacheSound, builtinPrecacheSound(h))
	vm.RegisterBuiltin(progs.BuiltinPrecacheModel, builtinPrecacheModel(h))
	vm.RegisterBuiltin(progs.BuiltinStuffCmd, noop)
	vm.RegisterBuiltin(progs.BuiltinFindRadius, builtinFindRadius(h))
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
	for _, idx := range []int{68, 69, 71, 72, 73, 75, 76, 77, 78, 79} {
		vm.RegisterBuiltin(idx, noop)
	}
	// changelevel(string mapname) -- builtin #70. The QC `trigger_changelevel`
	// touch fires this when the player walks through the level-exit
	// volume. Wired against the host's PendingChangelevel/NextMap
	// fields so the embedder's main loop can poll the request +
	// re-spawn into the new map. See [enginehost.BuiltinChangeLevel].
	vm.RegisterBuiltin(enginehost.BuiltinChangeLevelIdx, enginehost.BuiltinChangeLevel(h))
	// ambientsound(pos, samp, vol, atten) -- builtin #74. The C upstream
	// PF_ambientsound (pr_cmds.c) calls SV_StartSound at the supplied
	// world position with a static channel. Wired here against the
	// host's mixer pool via [enginehost.Host.AmbientSound]; see
	// [builtinAmbientSound] for the per-arg shape.
	vm.RegisterBuiltin(74, builtinAmbientSound(h))
	// WriteByte / WriteChar / WriteShort / WriteLong / WriteCoord /
	// WriteAngle / WriteString / WriteEntity occupy slots 52..60 in
	// tyrquake's table. Server-side QC emits client-message bytes
	// through these; no client is reading, so swallowing them is
	// safe for the spawn-time + early-tic phase.
	for _, idx := range []int{52, 53, 54, 55, 56, 57, 58, 59, 60} {
		vm.RegisterBuiltin(idx, noop)
	}
	// Monster-AI / per-tic builtin gap fill. tyrquake's pr_builtin[]
	// (reference/common/pr_cmds.c) reserves the following slots that
	// the named-constant block above leaves uncovered:
	//
	//   33 = PF_Fixme (alt-walkmove slot; defs.qc unused)
	//   39 = PF_Fixme (between ceil + checkbottom)
	//   42 = PF_Fixme (between pointcontents + fabs)
	//   50 = PF_Fixme (between changeyaw + vectoangles)
	//   61..66 = PF_Fixme (gap between WriteEntity + SV_MoveToGoal)
	//   67 = SV_MoveToGoal (monster nav helper -- ai_walk's "step
	//        toward goalentity" core; called by ai_walk/ai_run after
	//        the deadline gate)
	//
	// Stubbing them as no-ops protects per-tic SV_RunThink dispatch
	// from "OP_CALLn target builtin index not registered: N" errors
	// when monster QC reaches ai_walk / ai_run / etc. The semantic
	// gap (monsters won't actually navigate) is the same as walkmove
	// being a no-op upstream of here -- DEFERRED to a real-AI batch
	// that wires SV_MoveToGoal + traceline + walkmove against the
	// world tree. The 80..89 tail covers anything mod-progs (Quake
	// Mission Pack 1/2) may have appended past the shareware table.
	for _, idx := range []int{
		33, 39, 42, 50,
		61, 62, 63, 64, 65, 66, 67,
		80, 81, 82, 83, 84, 85, 86, 87, 88, 89,
	} {
		vm.RegisterBuiltin(idx, noop)
	}
	return nil
}

// builtinPrecacheModel returns a Builtin closure that implements
// the QuakeC precache_model(name) built-in (tyrquake's PF_precache_model
// at builtin slot 20). Reads the string_t name from OFS_PARM0, appends
// it to h.Server.ModelPrecache via server.PrecacheModel (first-empty-
// slot policy), and writes the SAME string_t offset to OFS_RETURN so
// the caller's `self.model = precache_model("progs/foo.mdl")` lands
// the real string offset on the edict's .model field (the QC compiler
// emits OP_CALL1 then OP_STOREP_S using OFS_RETURN as the source).
//
// nil host or nil Server is a tolerated no-op (matches the stub
// shape: spawn QC still proceeds to its field-assignment half). A
// precache-full server logs a one-line warning + returns nil; the
// upstream Host_Errors but the Go port's contract is "diagnose loudly,
// don't crash the bring-up".
func builtinPrecacheModel(h *enginehost.Host) progs.Builtin {
	return func(vm *progs.VM) error {
		if h == nil || h.Server == nil {
			return nil
		}
		off, _ := vm.GlobalInt(progs.OfsParm0)
		name := vm.String(off)
		if name == "" {
			return vm.SetGlobalInt(progs.OfsReturn, off)
		}
		if _, err := engineserver.PrecacheModel(h.Server.ModelPrecache, name); err != nil {
			fmt.Printf("QUAKE: precache_model(%q): %v\n", name, err)
		}
		return vm.SetGlobalInt(progs.OfsReturn, off)
	}
}

// builtinPrecacheSound returns a Builtin closure that implements the
// QuakeC precache_sound(name) built-in (tyrquake's PF_precache_sound
// at builtin slot 19). Reads the string_t name from OFS_PARM0, loads
// the WAV via the host's injected SoundLoader (the WAV body lives at
// "sound/<name>" inside pak0), parses it via [sound.LoadWav], and
// records both the wire-side precache slot (Server.SoundPrecache) and
// the mixer-side parsed *Sample (h.Sounds[idx]) via
// [enginehost.Host.PrecacheSound]. Writes the SAME string_t offset
// back to OFS_RETURN so `self.noise = precache_sound("doors/medtry.wav")`
// stores the original string onto the edict (matching precache_model).
//
// nil host / nil server is a tolerated no-op (matches the no-op stub
// shape: spawn QC still proceeds to its field-assignment half). A
// precache failure (loader miss, parse error, table full) is logged
// + the call still returns the input string_t so QC bytecode stays
// intact (the missing sound just won't audibly fire later).
func builtinPrecacheSound(h *enginehost.Host) progs.Builtin {
	return func(vm *progs.VM) error {
		if h == nil || h.Server == nil {
			return nil
		}
		off, _ := vm.GlobalInt(progs.OfsParm0)
		name := vm.String(off)
		if name == "" {
			return vm.SetGlobalInt(progs.OfsReturn, off)
		}
		if _, err := h.PrecacheSound(name); err != nil {
			fmt.Printf("QUAKE: precache_sound(%q): %v\n", name, err)
		}
		return vm.SetGlobalInt(progs.OfsReturn, off)
	}
}

// builtinSound returns a Builtin closure that implements the QuakeC
// sound(entity, channel, samplename, volume, attenuation) built-in
// (tyrquake's PF_sound at builtin slot 8). Reads:
//
//	OFS_PARM0 = entity pointer
//	OFS_PARM1 = channel (float, truncated to int 0..7)
//	OFS_PARM2 = samplename (string_t)
//	OFS_PARM3 = volume (float in [0, 1], scaled to 0..255)
//	OFS_PARM4 = attenuation (float; ATTN_NORM=1, ATTN_NONE=0, ...)
//
// Dispatches the play-event onto h's mixer pool via
// [enginehost.Host.StartSound]. The QC builtin returns void, so no
// OFS_RETURN write is needed.
//
// Entity resolution: when the arena is attached the entity-pointer is
// resolved to a slot via [progs.EdictArena.ResolvePointer]; on no-arena
// / unresolvable pointer / world (slot 0) we pass entIdx=0 (the world).
//
// Spatialization is DEFERRED: the bring-up shape ignores the entity's
// world position + attenuation and plays at full master volume (= the
// sound is audible regardless of listener position). The leftVol /
// rightVol arguments are -1 sentinels so [Host.StartSound] uses the
// caller-supplied master volume on both ears. A follow-up batch wires
// the per-tic [sound.Spatialize] pass once the camera origin + right
// axis are threaded into the call site.
//
// Tolerated no-ops: nil host / no sound pool / empty name / missing
// precache slot all return nil silently after logging. Surfacing the
// error would abort the per-tic dispatch and skip the rest of the
// frame's QC -- not what we want when one missing sound shouldn't
// crash the game.
func builtinSound(h *enginehost.Host) progs.Builtin {
	return func(vm *progs.VM) error {
		if h == nil {
			return nil
		}
		entPtr, _ := vm.GlobalInt(progs.OfsParm0)
		chanF, _ := vm.GlobalFloat(progs.OfsParm1)
		nameOff, _ := vm.GlobalInt(progs.OfsParm2)
		volF, _ := vm.GlobalFloat(progs.OfsParm3)
		attenF, _ := vm.GlobalFloat(progs.OfsParm4)

		name := vm.String(nameOff)
		if name == "" {
			return nil
		}

		// Resolve entity-pointer to a slot index. Slot 0 (world) is the
		// upstream's "no specific origin" sentinel; non-world slots are
		// for entity-attached sounds (weapon fire, monster grunt, ...).
		entIdx := 0
		var entEdict *progs.Edict
		if arena := vm.Arena(); arena != nil && entPtr != 0 {
			if ed, _, err := arena.ResolvePointer(entPtr); err == nil {
				entEdict = ed
				for i, e := range h.Server.Edicts {
					if e == ed {
						entIdx = i
						break
					}
				}
			}
		}

		channel := int(chanF)
		if channel < 0 {
			channel = 0
		}
		if channel > 7 {
			channel = 7
		}
		// QC's volume is [0, 1]; the mixer's range is [0, 255].
		vol := int(volF * 255)
		if vol < 0 {
			vol = 0
		}
		if vol > 255 {
			vol = 255
		}

		// Source origin: the owning entity's origin entvars field
		// (zero when the sound is world-owned or the lookup fails;
		// Spatialize treats a zero source with a non-zero listener as
		// "to the listener's left/right depending on the right axis").
		var sourceOrigin [3]float32
		if entEdict != nil {
			if p := h.Progs(); p != nil {
				if ev, err := progs.NewEntVars(p, entEdict); err == nil {
					sourceOrigin, _ = ev.ReadVec3("origin")
				}
			}
		}

		atten := enginesound.SoundAttenuation(attenF)
		if _, err := h.StartSoundAt(entIdx, channel, name, vol, atten, sourceOrigin); err != nil {
			// Log + continue. The most common failure here is "sample
			// not precached" -- a real bug in the asset path but not
			// a reason to abort the per-tic dispatch.
			fmt.Printf("QUAKE: sound(ent=%d ch=%d %q vol=%d): %v\n",
				entIdx, channel, name, vol, err)
		}
		return nil
	}
}

// ambientSlotCounter is the round-robin index the per-call ambientsound
// builtin advances so each ambient source in the map lands on its own
// reserved-static channel. Wraps at pool.ReservedStatic (the call below
// modulo-clamps). tyrquake's PF_ambientsound also allocates from a
// fixed bank; the wrap is benign (>= ReservedStatic ambient sources
// in one map is exceedingly rare in the shareware progs).
var ambientSlotCounter int

// builtinAmbientSound returns a Builtin closure that implements the
// QuakeC ambientsound(position, samplename, volume, attenuation)
// built-in (tyrquake's PF_ambientsound at builtin slot 74). Reads:
//
//	OFS_PARM0 = position (vec3, world-space anchor)
//	OFS_PARM1 = samplename (string_t)
//	OFS_PARM2 = volume (float in [0, 1])
//	OFS_PARM3 = attenuation (float; typically ATTN_STATIC=3)
//
// Parks the sample on the next reserved-static channel via
// [enginehost.Host.AmbientSound]. The position is logged but NOT yet
// fed into a spatial mix (the mixer's Spatialize wiring is the
// follow-up); for the audio-pipeline-validation goal what matters is
// that ambient sources spawned by the map (water1, wind, gurgle,
// generator hum) become non-silent on the mixer output the moment
// SpawnServer's QC pass calls this builtin.
//
// Returns void.
func builtinAmbientSound(h *enginehost.Host) progs.Builtin {
	return func(vm *progs.VM) error {
		if h == nil || h.SoundPool() == nil {
			return nil
		}
		// Read the world-space anchor; the C upstream stores it on the
		// static channel so SND_Spatialize computes per-frame L/R
		// falloff. The Go port spatializes at fire-time using the
		// listener basis the embedder publishes via Host.SetListener.
		position, _ := vm.GlobalVector(progs.OfsParm0)
		nameOff, _ := vm.GlobalInt(progs.OfsParm1)
		volF, _ := vm.GlobalFloat(progs.OfsParm2)
		attenF, _ := vm.GlobalFloat(progs.OfsParm3)

		name := vm.String(nameOff)
		if name == "" {
			return nil
		}

		// Auto-precache: the upstream PF_ambientsound calls SV_FindIndex
		// on the soundname + Sys_Error's if it's missing; the Go port
		// triggers a precache here so map-spawn ambient sources don't
		// need a separate precache_sound call (the QC startup pass for
		// many entity classes calls ambientsound directly without
		// pre-precaching).
		if _, err := h.PrecacheSound(name); err != nil {
			fmt.Printf("QUAKE: ambientsound precache(%q): %v\n", name, err)
			return nil
		}

		reserved := h.SoundPool().ReservedStatic
		if reserved <= 0 {
			return nil
		}
		slot := ambientSlotCounter % reserved
		ambientSlotCounter++

		vol := int(volF * 255)
		if vol < 0 {
			vol = 0
		}
		if vol > 255 {
			vol = 255
		}
		atten := enginesound.SoundAttenuation(attenF)
		if _, err := h.AmbientSoundAt(slot, 0, name, vol, position, atten); err != nil {
			fmt.Printf("QUAKE: ambientsound(%q vol=%d slot=%d): %v\n", name, vol, slot, err)
		}
		return nil
	}
}

// setModelCache caches the per-builtinSetModel state that needs to
// survive across calls: a memo of decoded *mdl.Model bbox pairs
// keyed by precache slot (avoids re-parsing the same .mdl byte blob
// every time setmodel hits a recurring monster classname), plus a
// counter that gates the per-call before/after trace to the first
// N invocations so the serial log surfaces a sample without
// drowning the channel under the ~80-entity start.bsp spawn pass.
type setModelCache struct {
	mdlBBox map[int][2][3]float32 // idx -> {mins, maxs} for already-loaded alias mdls
	traced  int                   // count of calls already logged (cap = setModelTraceCalls)
}

// setModelTraceCalls caps the per-call before/after trace emitted by
// builtinSetModel so the serial log gets a sample of real spawn-time
// invocations without the whole ~80-entity start.bsp spawn pass
// turning into a 80-line wall of mins/maxs/size dumps.
const setModelTraceCalls = 8

// builtinSetModel returns a Builtin closure that implements the
// QuakeC setmodel(entity, name) built-in (tyrquake's PF_setmodel at
// builtin slot 3). Reads the entity-pointer from OFS_PARM0 + the
// string_t name from OFS_PARM1, resolves the model index by walking
// Server.ModelPrecache (NOT add-if-missing -- upstream PF_setmodel
// errors when the model isn't already precached, so a precache pass
// must have run first), then writes:
//
//   - ent.model      = name (string_t offset, stored as int32 in field)
//   - ent.modelindex = idx  (stored as float per QC convention)
//   - ent.mins / ent.maxs / ent.size = the model's bbox + extent
//     (SetMinMaxSize equivalent -- without these the world-trace
//     collapses to a ray, monsters / triggers / movers don't clip)
//   - h.World.LinkBounds(edictIdx, absmin, absmax, kind) registers
//     the edict in the area tree (SV_LinkEdict equivalent), so
//     AreaQuery + the trigger/solid trace broadphase see the
//     entity. The world-space bounds are origin + mins/maxs;
//     kind is derived from the edict's `solid` entvars field:
//     SOLID_NOT  -> SolidKindSkip (no-link), SOLID_TRIGGER ->
//     SolidKindTrigger, anything else -> SolidKindSolid.
//
// Bbox source per name:
//
//   - name == ""              -> zero bbox, kind = SolidKindSkip
//   - "maps/<x>.bsp" (idx 1)  -> worldmodel bspfile.Models[0] bbox
//   - "*N" submodels (idx >1) -> worldmodel bspfile.Models[N] bbox
//   - "*.mdl" alias models    -> load via mdl.Load (resolver), take
//     frame 0's BBoxMin/Max + scale by
//     Header.Scale + ScaleOrigin to
//     decode the byte-packed TriVertx
//     into world coordinates. Cached on
//     first hit so the second setmodel
//     call for the same model is O(1).
//
// Tolerated no-ops (no host / no server / no arena / unresolvable
// edict-pointer / field-not-in-progs / no worldmodel yet / mdl
// resolver error) all log a one-line warning + return nil; same
// crash-safety contract as builtinPrecacheModel.
//
// SCOPE: alias-mdl resolution uses Frame 0's bbox (the rest pose).
// Upstream Mod_LoadAliasModel walks every frame + tracks the max
// extent across the whole animation; the rest-pose bbox is a tight
// underestimate that's good enough for the spawn-time collision
// broadphase (a few units off the per-frame max won't move the
// AreaQuery answer for typical Q1 monsters).
func builtinSetModel(h *enginehost.Host) progs.Builtin {
	cache := &setModelCache{mdlBBox: map[int][2][3]float32{}}
	return func(vm *progs.VM) error {
		if h == nil || h.Server == nil {
			return nil
		}
		entPtr, _ := vm.GlobalInt(progs.OfsParm0)
		nameOff, _ := vm.GlobalInt(progs.OfsParm1)
		name := vm.String(nameOff)
		arena := vm.Arena()
		if arena == nil {
			return nil
		}
		ent, edictIdx, err := arena.ResolvePointer(entPtr)
		if err != nil {
			fmt.Printf("QUAKE: setmodel(ptr=%d, %q): ResolvePointer: %v\n", entPtr, name, err)
			return nil
		}
		idx, idxErr := engineserver.ModelIndex(h.Server.ModelPrecache, name)
		if idxErr != nil {
			fmt.Printf("QUAKE: setmodel(%q): %v\n", name, idxErr)
			// Continue: still write .model so .model isn't half-set.
		}
		p := vm.Progs()
		if p == nil {
			return nil
		}
		if def := p.FindField("model"); def != nil {
			_ = ent.FieldSetInt(int(def.Ofs), nameOff)
		}
		if def := p.FindField("modelindex"); def != nil {
			_ = ent.FieldSetFloat(int(def.Ofs), float32(idx))
		}

		// Bbox half. Skip when the precache lookup failed OR the
		// name resolves to slot 0 (the empty-model sentinel) -- the
		// upstream PF_setmodel calls SetMinMaxSize unconditionally,
		// but with a zero-bbox model the absmin/absmax both
		// collapse to ent.origin so the area-tree link adds a
		// degenerate point. The Go port treats both as "no link";
		// the entity still has a model name + index for the
		// renderer's per-entity dispatch.
		ev, _ := progs.NewEntVars(p, ent)
		var beforeMins, beforeMaxs, beforeSize [3]float32
		traceThis := cache.traced < setModelTraceCalls
		if traceThis {
			beforeMins, _ = ev.ReadVec3("mins")
			beforeMaxs, _ = ev.ReadVec3("maxs")
			beforeSize, _ = ev.ReadVec3("size")
		}

		mins, maxs, bboxOK := resolveModelBBox(h, cache, name, idx)
		if bboxOK {
			size := [3]float32{
				maxs[0] - mins[0],
				maxs[1] - mins[1],
				maxs[2] - mins[2],
			}
			_ = ev.WriteVec3("mins", mins)
			_ = ev.WriteVec3("maxs", maxs)
			_ = ev.WriteVec3("size", size)

			// Area-tree link half (SV_LinkEdict equivalent). Read
			// the edict's solid field to pick the SolidKind, the
			// origin to build the world-space absmin/absmax, then
			// LinkBounds. World.Clear (already called by SpawnServer)
			// built the area tree; LinkBounds is a no-op without
			// a root, which keeps the call safe pre-SpawnServer.
			if h.World != nil {
				origin, _ := ev.ReadVec3("origin")
				absmin := [3]float32{
					origin[0] + mins[0],
					origin[1] + mins[1],
					origin[2] + mins[2],
				}
				absmax := [3]float32{
					origin[0] + maxs[0],
					origin[1] + maxs[1],
					origin[2] + maxs[2],
				}
				kind := solidKindFromEntvars(ev)
				h.World.LinkBounds(world.Key(edictIdx), absmin, absmax, kind)
			}

			if traceThis {
				cache.traced++
				fmt.Printf("QUAKE: setmodel(slot=%d, %q, idx=%d) -- mins/maxs/size BEFORE=%v/%v/%v AFTER=%v/%v/%v\n",
					edictIdx, name, idx,
					beforeMins, beforeMaxs, beforeSize,
					mins, maxs, size)
			}
		} else if traceThis {
			cache.traced++
			fmt.Printf("QUAKE: setmodel(slot=%d, %q, idx=%d) -- bbox unresolved (kept mins/maxs %v/%v)\n",
				edictIdx, name, idx, beforeMins, beforeMaxs)
		}
		return nil
	}
}

// resolveModelBBox returns the world-space (mins, maxs) bounding box
// for the model named `name` at precache slot `idx`. The "ok" return
// is false when the bbox can't be determined (empty name, slot 0,
// missing worldmodel for BSP submodels, mdl load failure) -- in
// those cases the caller skips both the SetMinMaxSize writes AND
// the LinkBounds call, matching upstream's "PF_setmodel does
// nothing without a real model" early-out.
//
// The cache memoizes alias .mdl bboxes by precache slot so a second
// setmodel for the same model is O(1) instead of re-parsing the
// blob through the resolver.
func resolveModelBBox(h *enginehost.Host, cache *setModelCache, name string, idx int) (mins, maxs [3]float32, ok bool) {
	if name == "" || idx == 0 {
		return mins, maxs, false
	}
	// BSP world or submodel. The precache layout (set up by
	// SpawnServer) puts the worldmodel at slot 1 under its
	// "maps/<n>.bsp" full path; slots 2..N hold submodels under
	// "*1", "*2", ... aliases. Both kinds read from the same
	// underlying bspfile.File via Server.WorldModel.File, so the
	// submodel index = idx - 1 mapping holds for both.
	if name[0] == '*' || (idx == 1 && len(name) >= 4 && name[:4] == "maps") {
		if h.Server.WorldModel == nil || h.Server.WorldModel.File == nil {
			return mins, maxs, false
		}
		models, err := h.Server.WorldModel.File.Models()
		if err != nil {
			return mins, maxs, false
		}
		// submodel index = idx - 1 (slot 1 -> bspfile.Models[0] =
		// the world; slot 2 -> bspfile.Models[1] = first *N
		// submodel; etc.)
		smIdx := idx - 1
		if smIdx < 0 || smIdx >= len(models) {
			return mins, maxs, false
		}
		return models[smIdx].Mins, models[smIdx].Maxs, true
	}
	// Alias .mdl path. Check the per-slot memo first; on a miss
	// the resolver pulls the file via the host's FileResolver
	// (the same chain SpawnServer used for the worldmodel + the
	// alias-render preload uses for per-entity .mdl loads), and
	// mdl.Load decodes it. Frame 0's BBoxMin/Max are TriVertx
	// (byte-packed) values; scale-decode them with Header.Scale +
	// Header.ScaleOrigin to recover world coords. Group frames
	// (animated frame 0) collapse to the group's first sub-frame.
	if bb, hit := cache.mdlBBox[idx]; hit {
		return bb[0], bb[1], true
	}
	if h.Resolver == nil {
		return mins, maxs, false
	}
	size, ra, err := h.Resolver(name)
	if err != nil {
		return mins, maxs, false
	}
	m, err := mdl.Load(ra, size)
	if err != nil {
		return mins, maxs, false
	}
	if len(m.Frames) == 0 {
		return mins, maxs, false
	}
	f := &m.Frames[0]
	var bbMin, bbMax mdl.TriVertx
	switch f.Type {
	case mdl.FrameSingle:
		bbMin, bbMax = f.Single.BBoxMin, f.Single.BBoxMax
	case mdl.FrameGroup:
		if f.Group == nil || len(f.Group.Frames) == 0 {
			return mins, maxs, false
		}
		bbMin, bbMax = f.Group.Frames[0].BBoxMin, f.Group.Frames[0].BBoxMax
	default:
		return mins, maxs, false
	}
	for i := 0; i < 3; i++ {
		mins[i] = m.Header.Scale[i]*float32(bbMin.V[i]) + m.Header.ScaleOrigin[i]
		maxs[i] = m.Header.Scale[i]*float32(bbMax.V[i]) + m.Header.ScaleOrigin[i]
	}
	cache.mdlBBox[idx] = [2][3]float32{mins, maxs}
	return mins, maxs, true
}

// builtinTraceLine returns a Builtin closure that implements the QC
// traceline(v1, v2, nomonsters, forent) built-in (tyrquake's
// PF_traceline at builtin slot 16). Runs a swept-line trace through
// the world brushmodel + every solid candidate via [enginehost.Host.TraceLine],
// then writes the result back into the QC-visible trace_* globals
// via [enginehost.WriteTraceGlobals].
//
// Reads OFS_PARM0..3 = (v1 vec3, v2 vec3, nomonsters float, forent entity).
// nomonsters != 0 selects [enginehost.MoveNoMonsters], which skips
// non-BSP solid candidates (so monsters don't block each other's
// sightlines through the world). The forent argument is the calling
// entity -- typically `self` for monster QC; it is excluded from
// the candidate list so the monster doesn't clip against its own
// bounding box.
//
// Tolerated no-ops: nil host / nil server / nil progs / nil arena
// all collapse to a clean trace (Fraction=1, EndPos=v2, InOpen=1,
// trace_ent=world). Real Q1 progs declares every trace_* global so
// the named-global write path lands the result; test stubs that omit
// a subset silently skip those writes.
func builtinTraceLine(h *enginehost.Host) progs.Builtin {
	return func(vm *progs.VM) error {
		v1, _ := vm.GlobalVector(progs.OfsParm0)
		v2, _ := vm.GlobalVector(progs.OfsParm1)
		nomon, _ := vm.GlobalFloat(progs.OfsParm2)
		entPtr, _ := vm.GlobalInt(progs.OfsParm3)

		var passEdict *progs.Edict
		if arena := vm.Arena(); arena != nil {
			if ed, _, err := arena.ResolvePointer(entPtr); err == nil {
				passEdict = ed
			}
		}
		mode := enginehost.MoveNormal
		if nomon != 0 {
			mode = enginehost.MoveNoMonsters
		}

		res, err := h.TraceLine(v1, v2, mode, passEdict)
		if err != nil {
			fmt.Printf("QUAKE: traceline: %v\n", err)
			return nil
		}

		// Resolve trace_ent into an arena pointer. EntIdx == 0 is the
		// world (always slot 0); EntIdx > 0 is a per-edict slot. -1
		// (clean miss) and 0 (world) both map to the world pointer
		// (the QC convention is "no clip = world is the trace ent").
		var trEntPtr int32
		if res.EntIdx > 0 {
			if arena := vm.Arena(); arena != nil {
				trEntPtr = arena.MakePointer(res.EntIdx, 0)
			}
		}
		return enginehost.WriteTraceGlobals(vm, vm.Progs(), res, trEntPtr)
	}
}

// builtinFindRadius returns a Builtin closure that implements the QC
// findradius(org, rad) built-in (tyrquake's PF_findradius at builtin
// slot 22). Reads OFS_PARM0 = org (vec3), OFS_PARM1 = rad (float);
// walks every non-free, non-world solid edict whose bbox centre
// falls within rad units of org; chains them via the `chain` ev_entity
// field; writes the head edict's pointer into OFS_RETURN.
//
// End-of-chain sentinel = world (pointer 0). Empty result returns
// a pointer-to-world (= 0) so the caller's `head = findradius(...)`
// + `while (head != world)` loop terminates immediately.
//
// Tolerated no-ops: nil host / nil server / nil progs / nil arena
// all return pointer 0 (= world). The `chain` field absent in progs
// (test stubs) short-circuits to "return world" too -- the chain
// can't be linked without somewhere to store the prev pointer.
func builtinFindRadius(h *enginehost.Host) progs.Builtin {
	return func(vm *progs.VM) error {
		org, _ := vm.GlobalVector(progs.OfsParm0)
		rad, _ := vm.GlobalFloat(progs.OfsParm1)

		slots := h.FindRadius(org, rad)

		arena := vm.Arena()
		var pointerFor func(int) int32
		if arena != nil {
			pointerFor = func(slot int) int32 { return arena.MakePointer(slot, 0) }
		}

		var edicts []*progs.Edict
		if h != nil && h.Server != nil {
			edicts = h.Server.Edicts
		}
		headSlot, err := enginehost.ChainEdicts(vm.Progs(), edicts, slots, pointerFor)
		if err != nil {
			fmt.Printf("QUAKE: findradius: %v\n", err)
			return vm.SetGlobalInt(progs.OfsReturn, 0)
		}
		var headPtr int32
		if headSlot > 0 && arena != nil {
			headPtr = arena.MakePointer(headSlot, 0)
		}
		return vm.SetGlobalInt(progs.OfsReturn, headPtr)
	}
}

// builtinSetOrigin returns a Builtin closure that implements the
// QuakeC setorigin(entity, vector) built-in (tyrquake's PF_setorigin
// at builtin slot 2). Reads the entity-pointer from OFS_PARM0 + the
// vector from OFS_PARM1, writes the new origin onto the edict's
// entvars, then re-links the area-tree entry so any subsequent
// AreaQuery sees the new bounds.
//
// Why a real impl instead of the historical no-op: the item-pickup
// chain depends on it. The QuakeC items.qc body of every
// item_*_touch handler ends with
//
//	setmodel(self, "");
//	setorigin(self, '-8000 -8000 -8000');
//
// Without re-linking on setorigin, the trigger's area-tree entry
// stays at its pickup position and the player's next-tic
// TouchTriggers walk re-fires the same item (= infinite ammo loop
// + spammed pickup sound).
//
// Tolerated no-ops (one-line warning + return nil; same crash-safety
// contract as the other host-bound builtins):
//
//   - h or h.Server nil                  -> no-op
//   - VM arena unwired                   -> no-op
//   - entity-pointer doesn't resolve     -> warn + return nil
//   - host.SetOrigin handles entvars /
//     area-tree absent cases silently    -> no extra branching here.
func builtinSetOrigin(h *enginehost.Host) progs.Builtin {
	return func(vm *progs.VM) error {
		if h == nil || h.Server == nil {
			return nil
		}
		arena := vm.Arena()
		if arena == nil {
			return nil
		}
		entPtr, _ := vm.GlobalInt(progs.OfsParm0)
		origin, _ := vm.GlobalVector(progs.OfsParm1)
		ent, _, err := arena.ResolvePointer(entPtr)
		if err != nil {
			fmt.Printf("QUAKE: setorigin(ptr=%d, %v): ResolvePointer: %v\n", entPtr, origin, err)
			return nil
		}
		h.SetOrigin(ent, origin)
		return nil
	}
}

// solidKindFromEntvars reads the QC `solid` field off ev + maps it
// to the world.SolidKind enum the area-tree link uses. SOLID_NOT
// (= 0) collapses to SolidKindSkip (no link); SOLID_TRIGGER to
// SolidKindTrigger; everything else (BBOX / SLIDEBOX / BSP) to
// SolidKindSolid. Mirrors the C SV_LinkEdict per-SOLID_* dispatch.
//
// A missing `solid` field (test stubs that strip it) is treated as
// SOLID_NOT -- the entity won't be linked, which is the safe
// default (the entity still gets its bbox + can be moved by hand).
func solidKindFromEntvars(ev *progs.EntVars) world.SolidKind {
	solid, err := ev.ReadFloat("solid")
	if err != nil {
		return world.SolidKindSkip
	}
	switch engineserver.Solid(int32(solid)) {
	case engineserver.SolidNot:
		return world.SolidKindSkip
	case engineserver.SolidTrigger:
		return world.SolidKindTrigger
	default:
		return world.SolidKindSolid
	}
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

// newLCGByteSource returns a uniform-byte callback for the particle
// pool's Emit / EmitTrail RNG arg. Uses the same Numerical-Recipes
// 32-bit LCG as newLCGRandom (a separate goroutine of state so
// gameplay random + particle random don't sample the same stream);
// callers feed it as `rng func() byte` -- each call rotates a byte
// out of the high half of the LCG state for a uniform 0..255
// distribution.
func newLCGByteSource(seed uint32) func() byte {
	state := seed
	return func() byte {
		state = state*1664525 + 1013904223
		return byte(state >> 24)
	}
}

// trailKindForModel maps a precache model name to the trail kind
// tyrquake's CL_LinkEntities dispatches based on the entity's
// per-model EF_* bits. The C upstream sets those bits at model-load
// time inside Mod_LoadModel; the Go port collapses the bit table
// down to a name-based lookup because the engine doesn't yet
// re-derive the per-model EF_* mask from the .mdl flags field.
//
// Returns (kind, true) when the model name is trail-bearing,
// (0, false) otherwise so the caller's tic loop skips the entity.
//
// Names mirror id1's canonical alias-model paths -- "progs/missile.mdl"
// is the rocket (id1 progs.dat) and "progs/grenade.mdl" the grenade;
// "progs/gib*.mdl" / "progs/zom_gib.mdl" emit blood drips; the
// scrag/wizard/knight tracer + Voor orb mirror the upstream's
// EF_TRACER / EF_TRACER2 / EF_TRACER3 bits which are model-pinned.
func trailKindForModel(name string) (render.TrailKind, bool) {
	switch name {
	case "progs/missile.mdl":
		return render.TrailRocket, true
	case "progs/grenade.mdl":
		return render.TrailGrenade, true
	case "progs/gib1.mdl", "progs/gib2.mdl", "progs/gib3.mdl",
		"progs/zom_gib.mdl":
		return render.TrailBlood, true
	case "progs/k_spike.mdl":
		return render.TrailSlightBlood, true
	case "progs/w_spike.mdl":
		return render.TrailTracer, true
	case "progs/laser.mdl":
		return render.TrailTracer2, true
	case "progs/v_spike.mdl":
		return render.TrailVoor, true
	}
	return 0, false
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

// buildDemoWaypoints returns a small set of in-map (PointInLeaf >= 1)
// view origins the demo-orbit override cycles through every
// demoWaypointPeriodSeconds seconds of sv.time. The set is seeded with anchor
// (the pickInMapCamera result, guaranteed in a leaf) + a handful of
// lattice probes biased toward the bbox extents at the same z so
// different captures expose different miptex sets / alias entities.
//
// Each candidate that fails PointInLeaf is silently skipped; the
// returned slice always contains anchor as its first entry so the
// per-tic override has something to fall back on even if every
// lattice probe lands in a solid leaf.
func buildDemoWaypoints(bm *model.BrushModel, file *bspfile.File, anchor [3]float32) [][3]float32 {
	out := [][3]float32{anchor}
	models, err := file.Models()
	if err != nil || len(models) == 0 {
		return out
	}
	m := &models[0]
	// Candidates: each lattice corner of the bbox at the anchor's z,
	// nudged inward by 1/8 of the extent so we stay clear of the
	// outer brushes. Z stays at the anchor's height (160 for the
	// start.bsp info_player_start) so we always look at things
	// roughly from the player's eye-line.
	const (
		nx = 4
		ny = 4
	)
	for ix := 0; ix < nx; ix++ {
		for iy := 0; iy < ny; iy++ {
			fx := float32(ix+1) / float32(nx+1)
			fy := float32(iy+1) / float32(ny+1)
			p := [3]float32{
				m.Mins[0] + (m.Maxs[0]-m.Mins[0])*fx,
				m.Mins[1] + (m.Maxs[1]-m.Mins[1])*fy,
				anchor[2],
			}
			if leaf := bm.PointInLeaf(p); leaf > 0 {
				out = append(out, p)
			}
		}
	}
	// Cap the set so the cycle stays short enough that a 30 s
	// headless capture visits each waypoint at least once. Four
	// distinct waypoints at 600 frames each = 2400 frames per cycle
	// which is well inside a 30 s @ ~60 Hz Pre2DDraw cadence.
	const maxWaypoints = 4
	if len(out) > maxWaypoints {
		out = out[:maxWaypoints]
	}
	return out
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

// makeConcharsLump returns a 16384-byte 128x128 char sheet built from
// the inlined 8x8 ASCII bitmap font in
// quake-tamago/concharsfont. Glyph cells contain real ASCII letter
// shapes (palette index 0xDC on, 0 off in the lower bank; 0x67 on, 0
// off in the upper "yellow" bank). DrawCharacter treats 0 as
// transparent, so blank cells (space + non-printable codes) paint
// nothing. This replaces the earlier synthetic fallback that filled
// each cell with a single palette byte (= colored squares), which made
// menu titles, console text, centerprint, and the intermission
// scoreboard unreadable.
//
// The conchars sheet length is asserted against assets.ConCharsLumpSize
// to keep the two constants in lock-step.
func makeConcharsLump() []byte {
	buf := concharsfont.Build(0xDC, 0x67)
	if len(buf) != assets.ConCharsLumpSize {
		// Build returns concharsfont.SheetSize (= 128*128 = 16384),
		// matching ConCharsLumpSize by construction. The runtime
		// guard is a paranoid sanity check so a mismatched constant
		// surfaces as an obvious panic at boot rather than as silent
		// downstream rendering corruption.
		panic("concharsfont.Build size mismatch vs assets.ConCharsLumpSize")
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

// loadExplosionSprite opens the canonical s_explod.spr asset from the
// embedded pak so the TE_EXPLOSION client arm can spawn billboarded
// flashes on top of the particle shower. Tries both upstream paths in
// order (early shareware paks shipped it under progs/; the retail
// release moved it to sprites/), returning (sprite, resolvedPath) on
// the first hit. (nil, "") when neither path is present -- callers
// silently degrade to particles-only.
//
// A malformed .spr (truncated, bad magic, unsupported version) is
// treated the same as a missing file: log the parse error so the
// QEMU serial stream surfaces the cause, then return (nil, "") so
// the bring-up keeps booting.
//
// nil pakFS returns (nil, "") -- placeholder-pak boots never have
// real sprite assets.
func loadExplosionSprite(pakFS fs.FS) (*enginespr.Sprite, string) {
	if pakFS == nil {
		return nil, ""
	}
	candidates := []string{
		"progs/s_explod.spr",
		"sprites/s_explod.spr",
	}
	for _, path := range candidates {
		blob, ok := tryReadPakFile(pakFS, path)
		if !ok {
			continue
		}
		sp, err := enginespr.Load(bytes.NewReader(blob), int64(len(blob)))
		if err != nil {
			fmt.Printf("QUAKE: spr.Load(%s) err: %v\n", path, err)
			continue
		}
		return sp, path
	}
	return nil, ""
}

// loadAliasModels walks the model precache and opens every entry that
// names an alias model (".mdl" suffix), returning two parallel slices
// the Pre2DDraw alias pass indexes by EntityState.ModelIdx:
//
//   - models[i] = *mdl.Model decoded out of pakFS, or nil for slots
//     that name a non-.mdl asset (BSP world/submodels like
//     "*1"/"*2", sprites, missing files) -- a single source of "skip
//     this entity" the per-tic loop nil-checks.
//   - skins[i] = *render.Pic built from the first single-skin of the
//     model, or nil when the model has no usable single skin (the
//     per-tic loop falls back to the checker texture). Single-skin
//     models (the common case) expose Skins[0].Single.Pixels at
//     SkinWidth*SkinHeight bytes of palette indices; group skins are
//     skipped in this commit (DrawAliasInterp wires the per-tic skin
//     picker in a follow-up).
//
// Returns the two slices + the count of non-nil models loaded + the
// count of names in the precache that ended in ".mdl" (lets the caller
// log "loaded N / M names" so the QEMU serial log says how many of the
// precache's alias slots actually decoded).
//
// The function is tolerant: any per-slot error (missing pak entry,
// malformed .mdl) leaves models[i] = nil + continues to the next slot.
// A nil pakFS returns ([], [], 0, 0) -- the placeholder-pak boot path
// has no .mdl files to load anyway.
func loadAliasModels(pakFS fs.FS, precache []string) ([]*mdl.Model, []*render.Pic, int, int) {
	n := len(precache)
	models := make([]*mdl.Model, n)
	skins := make([]*render.Pic, n)
	if pakFS == nil || n == 0 {
		return models, skins, 0, 0
	}
	loaded := 0
	names := 0
	for i := 0; i < n; i++ {
		name := precache[i]
		if !hasSuffix(name, ".mdl") {
			continue
		}
		names++
		blob, ok := tryReadPakFile(pakFS, name)
		if !ok {
			continue
		}
		m, err := mdl.Load(bytes.NewReader(blob), int64(len(blob)))
		if err != nil {
			fmt.Printf("QUAKE: mdl.Load(%s) err: %v\n", name, err)
			continue
		}
		models[i] = m
		skins[i] = firstSkinAsPic(m)
		loaded++
	}
	return models, skins, loaded, names
}

// loadBoltModels opens the three canonical lightning-bolt alias models
// out of pak0: progs/bolt1.mdl, progs/bolt2.mdl, progs/bolt3.mdl
// (TE_LIGHTNING1 / 2 / 3 respectively; TE_BEAM re-uses bolt2). Returns
// parallel three-slot arrays plus the count of slots that resolved --
// missing files / parse failures leave the slot nil so the per-tic
// beam Walk in setupRenderer can silently skip them.
//
// A nil pakFS returns three nil slots + 0 (placeholder-pak boot path,
// no .mdl files anywhere).
func loadBoltModels(pakFS fs.FS) (models [3]*mdl.Model, skins [3]*render.Pic, loaded int) {
	if pakFS == nil {
		return
	}
	paths := [3]string{
		"progs/bolt.mdl",  // some shareware builds shipped bolt.mdl
		"progs/bolt2.mdl", // TE_LIGHTNING2 + TE_BEAM (the thunderbolt)
		"progs/bolt3.mdl", // TE_LIGHTNING3 (boss beam)
	}
	// TE_LIGHTNING1 (low-end boss bolt) is conventionally progs/bolt.mdl
	// in the shareware paks; later retail variants ship progs/bolt1.mdl.
	// Try the bolt1 alias first, fall back to bolt.
	alt1 := "progs/bolt1.mdl"
	if blob, ok := tryReadPakFile(pakFS, alt1); ok {
		if m, err := mdl.Load(bytes.NewReader(blob), int64(len(blob))); err == nil {
			models[0] = m
			skins[0] = firstSkinAsPic(m)
			loaded++
		} else {
			fmt.Printf("QUAKE: mdl.Load(%s) err: %v\n", alt1, err)
		}
	}
	startIdx := 0
	if models[0] != nil {
		startIdx = 1 // skip slot 0; already loaded via bolt1.mdl alias
	}
	for i := startIdx; i < 3; i++ {
		blob, ok := tryReadPakFile(pakFS, paths[i])
		if !ok {
			continue
		}
		m, err := mdl.Load(bytes.NewReader(blob), int64(len(blob)))
		if err != nil {
			fmt.Printf("QUAKE: mdl.Load(%s) err: %v\n", paths[i], err)
			continue
		}
		models[i] = m
		skins[i] = firstSkinAsPic(m)
		loaded++
	}
	return
}

// firstSkinAsPic returns the model's first single-skin as a *render.Pic
// (palette-indexed, SkinWidth x SkinHeight, byte-per-pixel). Group
// skins (animated) collapse to the first sub-skin of the group; a
// model with zero skins or a malformed first skin returns nil so the
// caller can fall back to the checker texture.
//
// The pixels are copied (not aliased) so the Pic owns a stable buffer
// independent of any future mutation of m.Skins[0].
func firstSkinAsPic(m *mdl.Model) *render.Pic {
	if m == nil || len(m.Skins) == 0 {
		return nil
	}
	w := int(m.Header.SkinWidth)
	h := int(m.Header.SkinHeight)
	if w <= 0 || h <= 0 {
		return nil
	}
	var src []byte
	sk := m.Skins[0]
	switch sk.Type {
	case mdl.SkinSingle:
		src = sk.Single.Pixels
	case mdl.SkinGroup:
		if sk.Group == nil || len(sk.Group.Skins) == 0 {
			return nil
		}
		src = sk.Group.Skins[0].Pixels
	default:
		return nil
	}
	if len(src) != w*h {
		return nil
	}
	pix := make([]byte, len(src))
	copy(pix, src)
	return &render.Pic{Width: w, Height: h, Pixels: pix}
}

// hasSuffix is a local strings.HasSuffix (the tamago std-lib subset
// links strings cleanly, but keeping the dependency surface narrow is
// the convention everywhere else in this binary -- see the bytes /
// io.ReadAll usage above for the same call-it-yourself pattern).
func hasSuffix(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

// wadOverlay wraps an [fs.FS] so that an Open miss on a `gfx/<name>.lmp`
// path transparently falls through to a lazily-parsed WAD2 archive
// (typically pak0:gfx/gfx.wad). The Quake Remastered pak0.pak does not
// ship the canonical HUD pic lumps (sbar / ibar / num_0..9 / face*...)
// as standalone entries -- they live inside gfx/gfx.wad. The overlay
// matches the lump name case-insensitively, with the leading directory
// + trailing .lmp stripped (so `gfx/sbar.lmp` resolves to lump `sbar`).
//
// The WAD payload returned by the underlying [wad.FS] for a qpic lump
// is the full dpic8_t blob (width:int32 + height:int32 + pixels), which
// is exactly what [render.ParsePic] consumes -- no transformation
// needed.
//
// The WAD is parsed at most once. If the WAD entry itself is missing
// (or fails to parse), the overlay degrades back to a plain pakFS view:
// every Open is forwarded to the wrapped FS, returning whatever error
// it produced.
type wadOverlay struct {
	base    fs.FS
	wadPath string
	parsed  bool
	w       *wad.FS
	wadBlob []byte // retained: wad.FS holds an io.ReaderAt into it
}

// newWADOverlay returns an overlay rooted at base. The WAD itself is
// not opened until the first miss requires it (lazy init keeps the
// fast path -- direct pakFS hits -- a single map lookup).
func newWADOverlay(base fs.FS, wadPath string) *wadOverlay {
	return &wadOverlay{base: base, wadPath: wadPath}
}

// Open implements [fs.FS]. Order of resolution:
//
//  1. Try the direct path in the underlying FS.
//  2. On miss (any error), if name has the `gfx/...lmp` shape,
//     lazy-parse the WAD + look up the bare lump name.
//  3. If the WAD also misses, return the ORIGINAL underlying error so
//     callers continue to see a sensible fs.PathError chain.
func (o *wadOverlay) Open(name string) (fs.File, error) {
	if o == nil || o.base == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	f, err := o.base.Open(name)
	if err == nil {
		return f, nil
	}
	lump, ok := wadLumpName(name)
	if !ok {
		return nil, err
	}
	w := o.openWAD()
	if w == nil {
		return nil, err
	}
	wf, werr := w.Open(lump)
	if werr != nil {
		return nil, err
	}
	return wf, nil
}

// openWAD lazily loads + parses the configured WAD path. Returns nil
// if the WAD is missing or malformed (the overlay then degrades to
// pakFS-only behaviour).
func (o *wadOverlay) openWAD() *wad.FS {
	if o.parsed {
		return o.w
	}
	o.parsed = true
	blob, ok := tryReadPakFile(o.base, o.wadPath)
	if !ok {
		return nil
	}
	o.wadBlob = blob
	w, err := wad.Open(bytes.NewReader(blob))
	if err != nil {
		return nil
	}
	o.w = w
	return o.w
}

// wadLumpName converts a pak-style `gfx/<name>.lmp` path into the bare
// WAD lump name (lowercase, case-insensitive). Returns ok=false for
// anything else so the overlay never tries to satisfy non-gfx requests
// out of the WAD.
func wadLumpName(name string) (string, bool) {
	const prefix = "gfx/"
	const suffix = ".lmp"
	if len(name) <= len(prefix)+len(suffix) {
		return "", false
	}
	if name[:len(prefix)] != prefix {
		return "", false
	}
	if name[len(name)-len(suffix):] != suffix {
		return "", false
	}
	return name[len(prefix) : len(name)-len(suffix)], true
}

// loadMenuAssets opens the menu pic lumps the [menu.Menu] overlay
// paints with. Mirrors the loadSBarAssets shape: WAD-overlay-fronted
// best-effort probe + verbose missing-asset logging so the QEMU serial
// trace surfaces gaps without killing the boot.
//
// The lump set: qplaque (left-edge "QUAKE" plaque), ttl_main +
// ttl_sgl (menu title banners), p_load / p_save / p_option (sub-menu
// titles), mainmenu (top-level body pic), sp_menu (single-player
// body pic), menudot1..6 (animated cursor strip). All live in
// gfx.wad on the Quake Remastered pak; the overlay resolves
// gfx/<name>.lmp transparently either way.
//
// Returns (assets, loaded, total). A nil pakFS skips the probe and
// returns a zero bundle so the menu's text-fallback path keeps it
// navigable.
func loadMenuAssets(pakFS fs.FS) (*menu.Assets, int, int) {
	if pakFS == nil {
		return &menu.Assets{}, 0, 0
	}
	a := &menu.Assets{}
	loaded, total := 0, 0
	overlay := newWADOverlay(pakFS, "gfx.wad")

	load := func(name string, dst **render.Pic) {
		total++
		blob, ok := tryReadPakFile(overlay, name)
		if !ok {
			fmt.Printf("QUAKE: menu asset %s missing -- text fallback\n", name)
			return
		}
		pic, err := render.ParsePic(blob)
		if err != nil {
			fmt.Printf("QUAKE: menu asset %s ParsePic err: %v -- text fallback\n", name, err)
			return
		}
		*dst = pic
		loaded++
	}

	load("gfx/qplaque.lmp", &a.QPlaque)
	load("gfx/ttl_main.lmp", &a.TitleMain)
	load("gfx/ttl_sgl.lmp", &a.TitleSinglePlayer)
	load("gfx/p_load.lmp", &a.TitleLoad)
	load("gfx/p_save.lmp", &a.TitleSave)
	load("gfx/p_option.lmp", &a.TitleOptions)
	load("gfx/mainmenu.lmp", &a.MainMenu)
	load("gfx/sp_menu.lmp", &a.SinglePlayerMenu)

	// Animated cursor strip (6 frames).
	a.MenuDots = make([]*render.Pic, 6)
	for i := 0; i < 6; i++ {
		load(fmt.Sprintf("gfx/menudot%d.lmp", i+1), &a.MenuDots[i])
	}
	// Drop trailing nils so menu.Draw's animation index stays valid.
	end := len(a.MenuDots)
	for end > 0 && a.MenuDots[end-1] == nil {
		end--
	}
	a.MenuDots = a.MenuDots[:end]

	return a, loaded, total
}

// loadSBarAssets opens the canonical sbar pic lumps out of pakFS via
// render.ParsePic and returns a populated *render.SBarAssets bundle +
// the count of slots resolved + the total attempted + a list of
// missing names (one entry per attempted lump that pakFS doesn't
// ship, e.g. the deathmatch ranking faces the single-player layout
// doesn't need but the upstream init pass still asks for).
//
// The list mirrors tyrquake's Sbar_Init pic table in NQ/sbar.c: the
// 320x24 main bar + the inventory strip + the 7 single-player weapon
// icons + 4 ammo icons + 5 health face pairs (rest+pained) + 3 armor
// tiers + 2 keys + 4 powerups + 4 sigils + 10 white digits + 10 red
// digits + the minus variants. Every lookup is best-effort; a missing
// lump logs once + leaves the corresponding slot nil so DrawSBar's
// drawIfNotNil + early-return-on-nil-BG path skips it silently.
//
// nil pakFS returns (nil, 0, 0, nil) -- the placeholder-pak boot path
// has no sbar lumps to load, and DrawSBar's nil-assets contract
// (ErrSbarNilAssets) is honoured by the caller's nil-guard.
func loadSBarAssets(pakFS fs.FS) (*render.SBarAssets, int, int, []string) {
	if pakFS == nil {
		return nil, 0, 0, nil
	}
	a := &render.SBarAssets{}
	loaded, total := 0, 0
	var missing []string

	// Quake Remastered's pak0.pak does NOT ship gfx/sbar.lmp etc as
	// standalone entries -- the canonical HUD pics live inside the WAD2
	// archive gfx.wad (at the pak root, NOT under gfx/). Wrap pakFS in
	// an overlay that falls through to a lazy-parsed gfx.wad on miss so
	// the same gfx/<name>.lmp probe resolves either layout transparently.
	overlay := newWADOverlay(pakFS, "gfx.wad")

	// load names a single lump + slots its parsed Pic into `dst`. The
	// nested closure shares the running counters; a missing lump
	// appends to `missing` + logs once so the QEMU serial channel
	// surfaces the gap without drowning the log.
	load := func(name string, dst **render.Pic) {
		total++
		blob, ok := tryReadPakFile(overlay, name)
		if !ok {
			missing = append(missing, name)
			fmt.Printf("QUAKE: sbar asset %s missing -- skipping\n", name)
			return
		}
		pic, err := render.ParsePic(blob)
		if err != nil {
			missing = append(missing, name)
			fmt.Printf("QUAKE: sbar asset %s ParsePic err: %v -- skipping\n", name, err)
			return
		}
		*dst = pic
		loaded++
	}

	// Main bar + inventory strip.
	load("gfx/sbar.lmp", &a.BG)
	load("gfx/ibar.lmp", &a.IBar)

	// White + red digit sets (10 each) + the minus variants. The minus
	// pics aren't part of SBarAssets's typed slots (DrawNumber never
	// renders a negative), so the loader probes them for log fidelity
	// only and discards the parsed Pic afterwards.
	for i := 0; i < 10; i++ {
		load(fmt.Sprintf("gfx/num_%d.lmp", i), &a.Nums[i])
	}
	for i := 0; i < 10; i++ {
		load(fmt.Sprintf("gfx/anum_%d.lmp", i), &a.AltNums[i])
	}
	var scratch *render.Pic
	load("gfx/num_minus.lmp", &scratch)
	scratch = nil
	load("gfx/anum_minus.lmp", &scratch)
	scratch = nil

	// Ammo icons (shells / nails / rockets / cells -- the order matches
	// pickAmmoIcon's else-if chain in render/sbar.go).
	load("gfx/sb_shells.lmp", &a.Ammo[0])
	load("gfx/sb_nails.lmp", &a.Ammo[1])
	load("gfx/sb_rocket.lmp", &a.Ammo[2])
	load("gfx/sb_cells.lmp", &a.Ammo[3])

	// Face pairs (5 health bands x {rest, pained}). The pak names are
	// 1-indexed; map face1..face5 -> Faces[0..4][0] (rest) and
	// face_p1..face_p5 -> Faces[0..4][1] (pained), mirroring the
	// (row, col) shape PickFaceFrame returns. Upstream order matches:
	// face1 = gibbed/near-death, face5 = healthy.
	for i := 0; i < 5; i++ {
		load(fmt.Sprintf("gfx/face%d.lmp", i+1), &a.Faces[i][0])
	}
	for i := 0; i < 5; i++ {
		load(fmt.Sprintf("gfx/face_p%d.lmp", i+1), &a.Faces[i][1])
	}

	// Armor (green / yellow / red).
	load("gfx/sb_armor1.lmp", &a.Armor[0])
	load("gfx/sb_armor2.lmp", &a.Armor[1])
	load("gfx/sb_armor3.lmp", &a.Armor[2])

	// Weapon icons (single-player set, 7 slots). Names per tyrquake's
	// sb_weapons[0][0..6] in NQ/sbar.c (Sbar_Init): shotgun, super-
	// shotgun, nailgun, super-nailgun, rocket-launcher, super-rocket-
	// launcher, lightning. There is NO axe slot -- the axe is rendered
	// via the player model, not the inventory strip -- so the table
	// has exactly 7 entries, matching SBarAssets.Weapons[7].
	//
	// The lumps live inside the WAD2 archive gfx.wad under the
	// upstream "inv_*" names (the overlay strips the gfx/ prefix +
	// .lmp suffix). The "selected" variant inv2_lightng is probed as
	// scratch so the log records the pak shipped the full active /
	// owned pair the upstream port toggles between -- consuming it
	// here would require expanding SBarAssets.Weapons to [7][8] to
	// match tyrquake's sb_weapons table, which the static-layout port
	// deliberately does not do.
	weaponBase := []string{
		"gfx/inv_shotgun.lmp",
		"gfx/inv_sshotgun.lmp",
		"gfx/inv_nailgun.lmp",
		"gfx/inv_snailgun.lmp",
		"gfx/inv_rlaunch.lmp",
		"gfx/inv_srlaunch.lmp",
		"gfx/inv_lightng.lmp",
	}
	for i, name := range weaponBase {
		load(name, &a.Weapons[i])
	}
	for _, name := range []string{"gfx/inv2_lightng.lmp"} {
		load(name, &scratch)
		scratch = nil
	}

	// Keys + powerups + sigils.
	load("gfx/sb_key1.lmp", &a.Key[0])
	load("gfx/sb_key2.lmp", &a.Key[1])
	load("gfx/sb_invis.lmp", &a.Invis)
	load("gfx/sb_invuln.lmp", &a.Invuln)
	load("gfx/sb_quad.lmp", &a.Quad)
	load("gfx/sb_suit.lmp", &a.Suit)
	for i := 0; i < 4; i++ {
		load(fmt.Sprintf("gfx/sb_sigil%d.lmp", i+1), &a.Sigil[i])
	}

	return a, loaded, total, missing
}

// halt is the tamago "spin forever after a fatal error" primitive.
// The board package's Exit handler also halts; this one covers the
// pre-board failure window + the in-engine return path.
func halt() {
	for {
	}
}

// observedAnyInput returns true iff the runloop has seen any movement
// key held / pressed this frame OR any trigger key (mouse-fire, jump)
// held. The Pre2DDraw closure uses it to auto-disable the demo-orbit
// override on the first observed user input (so a human pressing W on
// a virtio-keyboard session takes over from the headless panorama
// without restarting).
//
// "Held" is the bit-0 sample on each [client.ButtonState]; this is
// the bit [client.KeyState] preserves across frames (impulse bits
// 1+2 get cleared every sample). We test it directly so the helper
// is non-destructive: probing input state shouldn't consume the
// per-frame impulse the next BaseMove call needs to see.
func observedAnyInput(r *runloop.Runner) bool {
	if r == nil {
		return false
	}
	b := r.Buttons
	if b.Forward.Pressed != 0 || b.Back.Pressed != 0 ||
		b.MoveLeft.Pressed != 0 || b.MoveRight.Pressed != 0 ||
		b.Left.Pressed != 0 || b.Right.Pressed != 0 ||
		b.Up.Pressed != 0 || b.Down.Pressed != 0 ||
		b.Lookup.Pressed != 0 || b.Lookdown.Pressed != 0 ||
		b.SpeedHeld {
		return true
	}
	if r.Triggers.Attack || r.Triggers.Jump {
		return true
	}
	return false
}

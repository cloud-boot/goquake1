// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package virtio is a Backend implementation that drives the engine
// over the go-virtio family of pure-Go device drivers:
//
//   - go-virtio/gpu.Framebuffer  → backend.Display.PresentFrame
//   - go-virtio/input.VirtioInput → backend.Input.PollInput
//   - go-virtio/sound.VirtioSound → backend.Audio.QueueAudio
//
// The Quake-engine port exists specifically to stress-test go-virtio's
// gpu / input / sound paths inside a Tart (Apple HVF) micro-VM: every
// frame round-trips through the 2D-framebuffer control queue; every
// key + mouse motion comes off the event virtqueue; every mixed audio
// frame becomes a PCM transfer over the tx virtqueue.
//
// Design choices:
//
//   - The backend takes ALREADY-OPENED devices as constructor args.
//     Transport setup + PCI/MMIO probing is the calling app's job —
//     keeps this package free of platform-specific bring-up.
//   - The devices are accepted through narrow interfaces (Framebuffer,
//     InputDevice, AudioDevice) instead of the concrete go-virtio
//     types. Tests can plug in mocks; an adapter package
//     (backend/virtio/realdev/, follow-up batch) wraps the real
//     *gpu.VirtioGPU / *input.VirtioInput / *sound.VirtioSound in
//     these interfaces.
//   - go-virtio's Framebuffer expects BGRA. The engine produces RGBA.
//     PresentFrame swaps R↔B per pixel on the way through.
//
// tyrquake: the role of vid_uefi.c / snd_alsa.c / in_evdev.c
// collapsed onto the abstract backend.Backend surface.
package virtio

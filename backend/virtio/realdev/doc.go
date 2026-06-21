// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package realdev wires the abstract device interfaces declared in
// backend/virtio to the concrete pure-Go drivers shipped by
// github.com/go-virtio/{gpu,input,sound}.
//
// backend/virtio is deliberately decoupled from the real drivers — its
// Framebuffer / InputDevice / AudioDevice interfaces let tests inject
// in-memory fakes without dragging the virtio bring-up (PCI cap walks,
// MMIO transports, virtqueue allocators) into the unit-test binary.
// This package is the missing half: thin adapter shims that satisfy
// those interfaces by forwarding to the real driver objects a caller
// brings up against a Tart / QEMU virtio device.
//
// Wiring shape — the calling app (e.g. quake-tamago) opens the three
// devices through go-virtio's PCI transport, hands the resulting
// *gpu.Framebuffer / *input.VirtioInput / *sound.VirtioSound to the
// Wrap* constructors below, and feeds the returned interfaces into
// virtio.New:
//
//	gpuDev, _ := gpu.OpenVirtioGPU(t)
//	fb, _    := gpuDev.SetupFramebuffer(0, 640, 400)
//	inDev, _ := input.OpenVirtioInput(t2)
//	snd, _   := sound.OpenVirtioSound(t3)
//	// ... PCMInfo + PCMSetParams + PCMPrepare + PCMStart for streamID 0 ...
//	be, _ := virtio.New(
//	    realdev.WrapFramebuffer(fb),
//	    realdev.WrapInput(inDev),
//	    realdev.WrapAudio(snd, 0),
//	    nil,
//	)
//
// Test strategy — every wrapper is a one-line constructor over a struct
// whose methods either trivially read a field (Width / Height / Buffer
// / SampleRate) or forward to an injected function variable
// (Flush / pollEvents / write). The function-variable seam lets the
// unit tests cover every statement without standing up a real virtio
// transport; integration with the actual drivers is exercised by the
// quake-tamago end-to-end run, not here.
package realdev

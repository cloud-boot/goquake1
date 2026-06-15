// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package crc is the Go port of tyrquake's common/crc.c + include/crc.h.
// It implements the 16-bit, non-reflected CRC over polynomial 0x1021 with
// initial value 0xffff and final xor 0x0000 (the CCITT/XMODEM variant)
// that Quake uses to fingerprint progs.dat, server initialisation
// strings, and certain network message blocks.
//
// Upstream pin: github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
// Hand-port date: 2026-06-15 (Q-1a kickoff)
// Port conventions: see ../../CONVENTIONS.md
//
// Mapping notes:
//   - C CRC_Init(unsigned short *crc)            -> Init() uint16
//   - C CRC_ProcessByte(unsigned short *crc, b)  -> ProcessByte(crc, b) uint16
//   - C CRC_Value(unsigned short crc)            -> Value(crc) uint16
//   - C CRC_Block(const void *buf, int count)    -> Block(buf []byte) uint16
//   - The C API mutates a caller-supplied pointer; the Go port returns the
//     new value because Go has cheap value-return semantics and the
//     pointer-in pattern leaks mutability without buying anything for a
//     16-bit scalar.
//   - The XOR_VALUE is 0x0000 upstream, so [Value] is currently the
//     identity. It is kept as a function (not inlined out) so future
//     tyrquake updates that change the constant land as a one-line edit.
package crc

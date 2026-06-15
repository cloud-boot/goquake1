// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package msg is the Go port of tyrquake's MSG_Write* / MSG_Read*
// wire-format primitives from common/common.c lines 365-802.
//
// Upstream pin:
//   github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
//
// The C upstream has two distinct styles split across the same file:
//
//   - Write side: every MSG_WriteX(sizebuf_t *sb, ...) takes an
//     explicit sizebuf and appends to it. The Go port mirrors this
//     with free functions WriteX(b *sizebuf.Buffer, ...) -- the
//     package qualifier (msg.WriteByte) carries the intent without an
//     interface dance.
//
//   - Read side: every MSG_ReadX() reads from globals net_message,
//     msg_readcount, msg_badread. The Go port replaces that with a
//     [Reader] value that owns its data + position + bad-read flag.
//     This is a clean intent-preserving deviation from upstream; the
//     globals exist because C had no nicer way to thread state.
//
// QW_HACK functions (MSG_WriteDeltaUsercmd / MSG_ReadDeltaUsercmd /
// MSG_GetReadCount / MSG_ReadStringLine) are intentionally skipped:
// Phase Q-1a is single-player (NetQuake) only and the QW usercmd_t
// shape differs from the NQ one. They land when (if) Q-MP scope opens.
//
// NETFLAG_* constants are hoisted from NQ/net.h so this package stays
// self-contained -- the eventual engine/net package will re-export
// them rather than duplicate.
package msg

// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package cmd is the Go port of tyrquake's common/cmd.c +
// include/cmd.h: the console-command subsystem. It owns the deferred-
// execution Buffer (Cbuf_*), the named-Handler Registry (Cmd_Add /
// Cmd_Execute), the parsed-argv accessor that handlers read, and the
// alias macro table (Cmd_Alias_f).
//
// Upstream pin: github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
// Hand-port date: 2026-06-15 (Q-1a kickoff)
// Port conventions: see ../../CONVENTIONS.md
//
// Mapping notes:
//   - cmd_text (static sizebuf_t) -> Buffer (per-instance, not global,
//     so callers can run multiple buffers in tests).
//   - cmd_tree + cmdalias_tree (two global stree_t) -> Registry, which
//     bundles both maps so they share a single lookup site.
//   - Cmd_ExecuteString tokenises into cmd_argv (global) then dispatches
//     -> Registry.Execute tokenises into a local slice that it passes
//     into Handler. No global argv; the Handler signature carries it.
//   - COM_Parse (in common.c, called by Cmd_TokenizeString) is inlined
//     into the Tokenize implementation here -- it's the only consumer
//     in this package, and porting it standalone would force a circular
//     dependency once the common package lands.
//   - The C alias path Cbuf_InsertText(a->value) recurses without a
//     guard; we add a fixed-depth guard (maxAliasDepth) so a
//     self-referential alias errors out instead of hanging the test
//     harness. Real tyrquake-on-bare-metal would loop forever; the
//     guard is a port-level safety net.
//   - "//" line comments inside a line are honoured by Tokenize (mirrors
//     COM_Parse), but the upstream Cbuf_Execute split-by-';' does NOT
//     consult them -- we keep that asymmetry.
package cmd

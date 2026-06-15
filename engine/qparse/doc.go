// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later
//
// Package qparse is the Go port of tyrquake's streaming token parser
// (common/common.c: COM_Parse_, COM_Parse), the lower-level cousin of
// the console-line Tokenize already in engine/cmd.
//
// COM_Parse is the engine-wide single-token-at-a-time scanner: it is
// called repeatedly over config files, demo headers, BSP entity
// strings, and progs string tables, each call returning the next token
// plus the unconsumed tail. Demo replay treats its output as part of
// the GATE A byte-exact contract, so we reproduce the C control flow
// (the `while (*data)` loop, the `<= ' '` whitespace test, the EOF-
// inside-quoted-string behaviour) verbatim instead of leaning on
// bufio.Scanner or strings.Fields.
//
// Upstream pin: github.com/sezero/tyrquake@653157915975b196e36980a1ef7146485509b69a
// Hand-port date: 2026-06-15
// Port conventions: see ../../CONVENTIONS.md
//
// Mapping notes:
//   - C `const char *COM_Parse(const char *data)` returns the
//     post-token cursor and stashes the token in the global
//     `com_tokenbuf`. The Go port returns both as values: `Token`
//     yields `(token, rest)` so callers thread the cursor explicitly
//     and the package stays goroutine-safe (no mutable globals).
//   - The C entry point picks split_single_chars based on the
//     NQ_HACK/QW_HACK build flag; we expose the two behaviours as
//     two named functions (`Token`, `TokenSplitSingleChars`) instead
//     of a runtime flag or an options struct, since the choice is
//     per-call-site and resolved at compile time in upstream too.
//     The BSP entity-string parser wants the split-single-chars
//     behaviour so braces tokenise on their own; the config-file
//     and demo readers do not.
//   - EOF returns `("", "")` (the C version returns NULL).
package qparse

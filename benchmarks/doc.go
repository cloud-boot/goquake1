// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package benchmarks holds performance-parity micro-benchmarks comparing
// go-quake1's software-render hot loops against tyrquake@6531579 (the C
// engine this was hand-ported from). It contains no production code --
// only Benchmark* functions -- so it carries no coverage obligation.
// See BENCHMARKS.md for methodology and the C harness in benchmarks/c.
package benchmarks

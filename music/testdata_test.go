// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package music

import (
	_ "embed"
	"testing"
)

// testOGG is a small valid OGG/Vorbis fixture (~7 KB) the
// integration tests in music_test.go decode end-to-end. Sourced from
// the upstream github.com/jfreymuth/oggvorbis test corpus (MIT
// licence) so the fixture is reproducible by any reader who fetches
// that module.
//
//go:embed testdata/test.ogg
var testOGG []byte

// loadTestOGG returns the embedded test fixture. Exposed as a helper
// so the test file's call sites stay readable and so a future
// "fixture missing" guard is centralised.
func loadTestOGG(t *testing.T) []byte {
	t.Helper()
	if len(testOGG) == 0 {
		t.Fatal("testdata/test.ogg empty -- embed did not populate")
	}
	return testOGG
}

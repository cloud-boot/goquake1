// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// oci-pack-quake packs the engine's embedpak/empty.pak +
// embedmusic/track02..11.ogg into an OCI image-layout directory that
// `oras push` can publish to any OCI Distribution v2 registry. The
// resulting reference is what cmd/quake-wasm hands to
// ociassets.NewFSFromManifest at boot so the browser streams the pak
// + music tracks instead of carrying them as 264 MB of go:embed
// payload.
//
// Usage:
//
//	go run ./cmd/oci-pack-quake --out _oci --ref quake-assets:latest
//	oras push localhost:5000/quake-assets:latest _oci/...
//
// The tool is host-only (not GOOS=js): it reads from disk, hashes the
// content, and writes the OCI directory tree. When the input files
// are still the 12-byte git placeholders ("no real assets shipped
// yet"), the tool logs a skip notice + exits 0 -- useful for CI
// loops that want to run the binary but don't have real game data.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-quake1/engine/ociassets"
)

// osExit is the testability seam (mirrors cmd/serve-wasm/main.go's
// pattern). Tests swap it for a recording stub so they can drive the
// failure paths without halting the test binary.
var osExit = os.Exit

func main() {
	if code := run(os.Args[1:], os.Stdout, os.Stderr); code != 0 {
		osExit(code)
	}
}

// run is the testability seam: returns an exit code instead of
// calling os.Exit so tests can drive every branch (placeholder
// skip, missing input, success, output dir error). The two writers
// let tests intercept the logging output without re-routing stdout.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("oci-pack-quake", flag.ContinueOnError)
	fs.SetOutput(stderr)
	outDir := fs.String("out", "_oci", "OCI image-layout output directory")
	ref := fs.String("ref", "quake-assets:latest", "OCI reference annotation (org.opencontainers.image.ref.name)")
	pakPath := fs.String("pak", "embedpak/empty.pak", "Path to pak0.pak (or the empty placeholder)")
	musicDir := fs.String("music-dir", "embedmusic", "Directory holding track02.ogg .. track11.ogg")
	skipPlaceholders := fs.Bool("skip-placeholders", true, "Exit 0 (instead of pack) when all inputs are git placeholders (<=12 bytes)")
	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError already printed the error + usage.
		return 2
	}

	entries, err := collectEntries(*pakPath, *musicDir)
	if err != nil {
		fmt.Fprintln(stderr, "oci-pack-quake:", err)
		return 1
	}
	if *skipPlaceholders && allPlaceholders(entries) {
		fmt.Fprintln(stdout, "oci-pack-quake: all inputs are 12-byte placeholders; nothing to pack")
		return 0
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(stderr, "oci-pack-quake: mkdir out:", err)
		return 1
	}
	digest, size, err := packLayoutSeam(*outDir, *ref, entries)
	if err != nil {
		fmt.Fprintln(stderr, "oci-pack-quake:", err)
		return 1
	}
	fmt.Fprintf(stdout, "oci-pack-quake: wrote %d layers + manifest %s (%d bytes) to %s\n",
		len(entries), digest, size, *outDir)
	fmt.Fprintf(stdout, "oci-pack-quake: ref annotation: %s\n", *ref)
	return 0
}

// packLayoutSeam is the swappable wrapper for [ociassets.PackLayout]
// so tests can drive the unhappy path without going through a real
// filesystem.
var packLayoutSeam = ociassets.PackLayout

// collectEntries gathers the file list the packer feeds into
// PackLayout. Returns one entry per existing file: the pak (if it
// exists) plus every track02..11.ogg that's present on disk.
//
// Missing inputs are tolerated -- a partial install (only pak, no
// music) still produces a valid manifest with the pak layer.
func collectEntries(pakPath, musicDir string) ([]ociassets.FileEntry, error) {
	var out []ociassets.FileEntry
	if st, err := os.Stat(pakPath); err == nil && !st.IsDir() {
		out = append(out, ociassets.FileEntry{
			Name:      "pak0.pak",
			Path:      pakPath,
			MediaType: ociassets.MediaTypeLayerPak,
		})
	}
	for n := 2; n <= 11; n++ {
		name := fmt.Sprintf("track%02d.ogg", n)
		full := filepath.Join(musicDir, name)
		if st, err := os.Stat(full); err == nil && !st.IsDir() {
			out = append(out, ociassets.FileEntry{
				Name:      "music/" + name,
				Path:      full,
				MediaType: ociassets.MediaTypeLayerMusic,
			})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no input files found (pak=%s, music-dir=%s)", pakPath, musicDir)
	}
	return out, nil
}

// allPlaceholders returns true when every entry's on-disk size is
// <= the 12-byte git placeholder threshold (matching
// embedpak.emptyPakSize). Used by --skip-placeholders.
func allPlaceholders(entries []ociassets.FileEntry) bool {
	const placeholderMax = 12
	for _, e := range entries {
		st, err := os.Stat(e.Path)
		if err != nil {
			return false
		}
		if st.Size() > placeholderMax {
			return false
		}
	}
	return true
}

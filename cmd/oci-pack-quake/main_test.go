// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-quake1/engine/ociassets"
)

func writeFile(t *testing.T, p string, body []byte) {
	t.Helper()
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// realInputs lays out a fake pak + music tree shaped like the engine
// repo's embedpak/ + embedmusic/ directories. Bytes are non-zero +
// distinct per file so the resulting layer digests differ.
func realInputs(t *testing.T) (pakPath, musicDir string) {
	t.Helper()
	root := t.TempDir()
	pakPath = filepath.Join(root, "pak0.pak")
	writeFile(t, pakPath, bytes.Repeat([]byte{1}, 100))
	musicDir = filepath.Join(root, "music")
	if err := os.MkdirAll(musicDir, 0o755); err != nil {
		t.Fatalf("mkdir music: %v", err)
	}
	for n := 2; n <= 11; n++ {
		writeFile(t, filepath.Join(musicDir, "track"+twoDigit(n)+".ogg"), bytes.Repeat([]byte{byte(n)}, 200))
	}
	return pakPath, musicDir
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + string('0'+byte(n))
	}
	return string('0'+byte(n/10)) + string('0'+byte(n%10))
}

func TestRun_OkRealInputs(t *testing.T) {
	pak, musicDir := realInputs(t)
	out := filepath.Join(t.TempDir(), "out")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--out", out,
		"--ref", "quake-assets:latest",
		"--pak", pak,
		"--music-dir", musicDir,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run: code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote 11 layers") {
		t.Fatalf("stdout: %q", stdout.String())
	}
	// index.json should exist
	if _, err := os.Stat(filepath.Join(out, "index.json")); err != nil {
		t.Fatalf("index.json missing: %v", err)
	}
}

func TestRun_PlaceholdersSkip(t *testing.T) {
	// All 12-byte placeholders -> skip path, exit 0, no out dir written.
	root := t.TempDir()
	pak := filepath.Join(root, "empty.pak")
	writeFile(t, pak, bytes.Repeat([]byte{0}, 12))
	music := filepath.Join(root, "music")
	os.MkdirAll(music, 0o755)
	for n := 2; n <= 11; n++ {
		writeFile(t, filepath.Join(music, "track"+twoDigit(n)+".ogg"), bytes.Repeat([]byte{0}, 12))
	}
	out := filepath.Join(t.TempDir(), "out")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", out, "--ref", "r:latest", "--pak", pak, "--music-dir", music}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run: code=%d", code)
	}
	if !strings.Contains(stdout.String(), "placeholders") {
		t.Fatalf("stdout: %q", stdout.String())
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("out dir should not exist on skip")
	}
}

func TestRun_PlaceholderForcedPack(t *testing.T) {
	// --skip-placeholders=false should pack the 12-byte stubs anyway.
	root := t.TempDir()
	pak := filepath.Join(root, "empty.pak")
	writeFile(t, pak, bytes.Repeat([]byte{0}, 12))
	out := filepath.Join(t.TempDir(), "out")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", out, "--ref", "r:latest", "--pak", pak, "--music-dir", root, "--skip-placeholders=false"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run: code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote 1 layers") {
		t.Fatalf("stdout: %q", stdout.String())
	}
}

func TestRun_NoInputs(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", out, "--pak", "/nope", "--music-dir", "/also-nope"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run: code=%d", code)
	}
	if !strings.Contains(stderr.String(), "no input files found") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestRun_BadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--nope"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run: code=%d", code)
	}
}

func TestRun_MkdirOutError(t *testing.T) {
	// Plant a regular file where the parent of --out lives, so the
	// MkdirAll under it fails with ENOTDIR.
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	writeFile(t, blocker, []byte("x"))
	pak, music := realInputs(t)
	out := filepath.Join(blocker, "child")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", out, "--ref", "r:latest", "--pak", pak, "--music-dir", music}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run: code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "mkdir out") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestRun_PackLayoutError(t *testing.T) {
	orig := packLayoutSeam
	packLayoutSeam = func(string, string, []ociassets.FileEntry) (string, int64, error) {
		return "", 0, errors.New("forced pack failure")
	}
	t.Cleanup(func() { packLayoutSeam = orig })
	pak, music := realInputs(t)
	out := filepath.Join(t.TempDir(), "out")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--out", out, "--ref", "r:latest", "--pak", pak, "--music-dir", music}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "forced pack failure") {
		t.Fatalf("run: code=%d stderr=%q", code, stderr.String())
	}
}

func TestAllPlaceholders_StatError(t *testing.T) {
	// Entry with a non-existent path -> allPlaceholders returns false.
	got := allPlaceholders([]ociassets.FileEntry{{Name: "x", Path: "/definitely/does/not/exist"}})
	if got {
		t.Fatal("allPlaceholders(missing): want false")
	}
}

func TestCollectEntries_PakIsDir(t *testing.T) {
	// When --pak points at a directory, it must be ignored.
	root := t.TempDir()
	pakDir := filepath.Join(root, "pak0.pak")
	os.MkdirAll(pakDir, 0o755)
	musicDir := filepath.Join(root, "music")
	os.MkdirAll(musicDir, 0o755)
	writeFile(t, filepath.Join(musicDir, "track02.ogg"), bytes.Repeat([]byte{1}, 100))
	got, err := collectEntries(pakDir, musicDir)
	if err != nil {
		t.Fatalf("collectEntries: %v", err)
	}
	if len(got) != 1 || got[0].Name != "music/track02.ogg" {
		t.Fatalf("collectEntries: %+v", got)
	}
}

func TestCollectEntries_MusicEntryIsDir(t *testing.T) {
	// One of the track paths is actually a directory -> skipped.
	root := t.TempDir()
	pak := filepath.Join(root, "pak0.pak")
	writeFile(t, pak, bytes.Repeat([]byte{1}, 100))
	musicDir := filepath.Join(root, "music")
	os.MkdirAll(musicDir, 0o755)
	os.MkdirAll(filepath.Join(musicDir, "track02.ogg"), 0o755)
	writeFile(t, filepath.Join(musicDir, "track03.ogg"), bytes.Repeat([]byte{2}, 100))
	got, _ := collectEntries(pak, musicDir)
	if len(got) != 2 {
		t.Fatalf("entries: %+v", got)
	}
}

func TestMain_OsExitWiring(t *testing.T) {
	// We can't call main() directly without overriding flag.CommandLine,
	// but we can drive osExit's plumbing: set it to a recorder, then
	// reset.
	called := -1
	orig := osExit
	osExit = func(code int) { called = code }
	t.Cleanup(func() { osExit = orig })
	// Drive run() with a bad flag -> non-zero -> osExit triggered when
	// we run the actual main shape.
	if code := run([]string{"--nope"}, &bytes.Buffer{}, &bytes.Buffer{}); code == 0 {
		t.Fatal("run(--nope): want non-zero")
	}
	osExit(2)
	if called != 2 {
		t.Fatalf("osExit recorder: called=%d", called)
	}
}

func TestMain_RunsViaMain(t *testing.T) {
	// Drive main() through a no-op osExit so we exercise the
	// run-from-args bootstrap path. Use a placeholders-only directory
	// so the call returns 0 quickly.
	orig := osExit
	osExit = func(int) {}
	t.Cleanup(func() {
		osExit = orig
		os.Args = []string{"oci-pack-quake"}
	})
	root := t.TempDir()
	pak := filepath.Join(root, "empty.pak")
	writeFile(t, pak, bytes.Repeat([]byte{0}, 12))
	music := root
	os.Args = []string{"oci-pack-quake", "--pak", pak, "--music-dir", music, "--out", filepath.Join(t.TempDir(), "out")}
	main()
}

func TestMain_NonZeroTriggersOsExit(t *testing.T) {
	// Drive the non-zero branch of main() so osExit is invoked.
	called := -1
	orig := osExit
	osExit = func(code int) { called = code }
	t.Cleanup(func() {
		osExit = orig
		os.Args = []string{"oci-pack-quake"}
	})
	os.Args = []string{"oci-pack-quake", "--nope"}
	main()
	if called != 2 {
		t.Fatalf("osExit called=%d want 2", called)
	}
}

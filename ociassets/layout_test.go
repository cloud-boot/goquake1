// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package ociassets

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, dir, name string, body []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestPackLayout_RoundTrip(t *testing.T) {
	src := t.TempDir()
	pak := writeTempFile(t, src, "pak0.pak", []byte("PACKfake"))
	ogg := writeTempFile(t, src, "track02.ogg", []byte("OggS-pretend"))

	out := t.TempDir()
	files := []FileEntry{
		{Name: "pak0.pak", Path: pak, MediaType: MediaTypeLayerPak},
		{Name: "music/track02.ogg", Path: ogg, MediaType: MediaTypeLayerMusic},
	}
	digest, size, err := PackLayout(out, "quake-assets:latest", files)
	if err != nil {
		t.Fatalf("PackLayout: %v", err)
	}
	if !strings.HasPrefix(digest, "sha256:") || size == 0 {
		t.Fatalf("PackLayout: digest=%q size=%d", digest, size)
	}

	// oci-layout marker
	if b, err := os.ReadFile(filepath.Join(out, "oci-layout")); err != nil || !strings.Contains(string(b), "imageLayoutVersion") {
		t.Fatalf("oci-layout marker: %v %q", err, b)
	}
	// index.json should reference our manifest digest + tag
	idx, err := os.ReadFile(filepath.Join(out, "index.json"))
	if err != nil {
		t.Fatalf("index.json: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(idx, &parsed); err != nil {
		t.Fatalf("index.json json: %v", err)
	}
	// Re-serve through ServeLayout to validate the wire mapping.
	body, ct, status, err := ServeLayout(out, "quake-assets", "/v2/quake-assets/manifests/latest")
	if err != nil || status != 200 || ct != MediaTypeManifest {
		t.Fatalf("ServeLayout(manifest): status=%d ct=%q err=%v", status, ct, err)
	}
	m, err := DecodeManifest(body)
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if len(m.Layers) != 2 {
		t.Fatalf("layers: %d", len(m.Layers))
	}
	if m.Annotations[AnnotationPathPrefix+"pak0.pak"] != Sha256Digest([]byte("PACKfake")) {
		t.Fatalf("pak0 annotation wrong")
	}
	// Blob fetch by digest
	body, _, status, err = ServeLayout(out, "quake-assets", "/v2/quake-assets/blobs/"+Sha256Digest([]byte("PACKfake")))
	if err != nil || status != 200 || string(body) != "PACKfake" {
		t.Fatalf("ServeLayout(blob): status=%d err=%v body=%q", status, err, body)
	}

	// Idempotent re-pack: same digest, no error.
	digest2, _, err := PackLayout(out, "quake-assets:latest", files)
	if err != nil {
		t.Fatalf("re-pack: %v", err)
	}
	if digest2 != digest {
		t.Fatalf("digest drift: %s vs %s", digest, digest2)
	}
}

func TestPackLayout_EmptyReference(t *testing.T) {
	if _, _, err := PackLayout(t.TempDir(), "", []FileEntry{{Name: "x", Path: "/nope"}}); err == nil {
		t.Fatal("PackLayout(empty ref): want error")
	}
}

func TestPackLayout_NoFiles(t *testing.T) {
	if _, _, err := PackLayout(t.TempDir(), "r:latest", nil); err == nil {
		t.Fatal("PackLayout(no files): want error")
	}
}

func TestPackLayout_MissingPath(t *testing.T) {
	out := t.TempDir()
	_, _, err := PackLayout(out, "r:latest", []FileEntry{{Name: "x", Path: filepath.Join(t.TempDir(), "nope")}})
	if err == nil {
		t.Fatal("PackLayout(missing file): want error")
	}
}

func TestPackLayout_BadOutDir(t *testing.T) {
	// Pass a path whose parent is a regular file -> mkdir fails.
	bad := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(bad, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, _, err := PackLayout(filepath.Join(bad, "child"), "r:latest", []FileEntry{{Name: "x", Path: bad}})
	if err == nil {
		t.Fatal("PackLayout(bad outdir): want error")
	}
}

func TestPackLayout_DefaultMediaType(t *testing.T) {
	src := t.TempDir()
	p := writeTempFile(t, src, "x", []byte("hi"))
	out := t.TempDir()
	_, _, err := PackLayout(out, "r:latest", []FileEntry{{Name: "x", Path: p}})
	if err != nil {
		t.Fatalf("PackLayout: %v", err)
	}
	// Re-read manifest, assert default media type stamped on layer.
	body, _, _, err := ServeLayout(out, "r", "/v2/r/manifests/latest")
	if err != nil {
		t.Fatalf("ServeLayout: %v", err)
	}
	m, _ := DecodeManifest(body)
	if m.Layers[0].MediaType != "application/octet-stream" {
		t.Fatalf("default mediaType: %q", m.Layers[0].MediaType)
	}
}

func TestServeLayout_Unknown(t *testing.T) {
	out := t.TempDir()
	// Seed a tiny layout so ServeLayout has an index to load.
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	if _, _, err := PackLayout(out, "r:latest", []FileEntry{{Name: "x", Path: src}}); err != nil {
		t.Fatalf("pack: %v", err)
	}
	// 404 on unknown manifest
	_, _, st, _ := ServeLayout(out, "r", "/v2/r/manifests/nope")
	if st != 404 {
		t.Fatalf("status = %d want 404", st)
	}
	// 404 on unknown blob
	_, _, st, _ = ServeLayout(out, "r", "/v2/r/blobs/sha256:0000")
	if st != 404 {
		t.Fatalf("unknown blob status = %d want 404", st)
	}
	// 400 on non-sha256 blob
	_, _, st, _ = ServeLayout(out, "r", "/v2/r/blobs/md5:abc")
	if st != 400 {
		t.Fatalf("bad-digest status = %d want 400", st)
	}
	// 404 on unknown route
	_, _, st, _ = ServeLayout(out, "r", "/v3/other")
	if st != 404 {
		t.Fatalf("unknown route status = %d want 404", st)
	}
}

func TestServeLayout_ManifestByDigest(t *testing.T) {
	out := t.TempDir()
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	digest, _, err := PackLayout(out, "r:latest", []FileEntry{{Name: "x", Path: src}})
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	body, _, st, err := ServeLayout(out, "r", "/v2/r/manifests/"+digest)
	if err != nil || st != 200 || len(body) == 0 {
		t.Fatalf("ServeLayout(digest): st=%d err=%v body=%d", st, err, len(body))
	}
}

func TestServeLayout_MissingIndex(t *testing.T) {
	if _, _, _, err := ServeLayout(t.TempDir(), "r", "/v2/r/manifests/latest"); err == nil {
		t.Fatal("ServeLayout(no index): want error")
	}
}

func TestServeLayout_BadIndex(t *testing.T) {
	d := t.TempDir()
	writeTempFile(t, d, "index.json", []byte("not json"))
	if _, _, _, err := ServeLayout(d, "r", "/v2/r/manifests/latest"); err == nil {
		t.Fatal("ServeLayout(bad json): want error")
	}
}

// swapWrite installs a hook on osWriteFile that errors when path
// matches matchSuffix; restores on cleanup. Drives the defensive
// "write blob/manifest/index/oci-layout" branches.
func swapWrite(t *testing.T, matchSuffix string) {
	t.Helper()
	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, matchSuffix) {
			return errors.New("forced write error: " + name)
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })
}

func TestPackLayout_WriteBlobError(t *testing.T) {
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	// Suffix is the sha256 hex of "hi"; force write of THAT blob to fail.
	hex := strings.TrimPrefix(Sha256Digest([]byte("hi")), "sha256:")
	swapWrite(t, hex)
	if _, _, err := PackLayout(t.TempDir(), "r:latest", []FileEntry{{Name: "x", Path: src}}); err == nil || !strings.Contains(err.Error(), "write blob") {
		t.Fatalf("PackLayout(write blob err): %v", err)
	}
}

func TestPackLayout_WriteConfigError(t *testing.T) {
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	// Force the config write to fail. We don't know the config digest
	// up-front, so we error on every write except the layer blob.
	hex := strings.TrimPrefix(Sha256Digest([]byte("hi")), "sha256:")
	origWrite := osWriteFile
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, hex) {
			return origWrite(name, data, perm)
		}
		return errors.New("forced write error: " + name)
	}
	t.Cleanup(func() { osWriteFile = origWrite })
	if _, _, err := PackLayout(t.TempDir(), "r:latest", []FileEntry{{Name: "x", Path: src}}); err == nil || !strings.Contains(err.Error(), "config blob") {
		t.Fatalf("PackLayout(write config err): %v", err)
	}
}

func TestPackLayout_MarshalConfigError(t *testing.T) {
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	orig := jsonMarshal
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("forced") }
	t.Cleanup(func() { jsonMarshal = orig })
	if _, _, err := PackLayout(t.TempDir(), "r:latest", []FileEntry{{Name: "x", Path: src}}); err == nil || !strings.Contains(err.Error(), "marshal config") {
		t.Fatalf("PackLayout(marshal config err): %v", err)
	}
}

func TestPackLayout_MarshalManifestError(t *testing.T) {
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	orig := encodeManifestSeam
	encodeManifestSeam = func(*Manifest) ([]byte, error) { return nil, errors.New("forced") }
	t.Cleanup(func() { encodeManifestSeam = orig })
	if _, _, err := PackLayout(t.TempDir(), "r:latest", []FileEntry{{Name: "x", Path: src}}); err == nil || !strings.Contains(err.Error(), "marshal manifest") {
		t.Fatalf("PackLayout(marshal manifest err): %v", err)
	}
}

func TestPackLayout_MarshalIndexError(t *testing.T) {
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	orig := jsonMarshalI
	jsonMarshalI = func(any, string, string) ([]byte, error) { return nil, errors.New("forced") }
	t.Cleanup(func() { jsonMarshalI = orig })
	if _, _, err := PackLayout(t.TempDir(), "r:latest", []FileEntry{{Name: "x", Path: src}}); err == nil || !strings.Contains(err.Error(), "marshal index") {
		t.Fatalf("PackLayout(marshal index err): %v", err)
	}
}

func TestPackLayout_WriteOCILayoutError(t *testing.T) {
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	swapWrite(t, "oci-layout")
	if _, _, err := PackLayout(t.TempDir(), "r:latest", []FileEntry{{Name: "x", Path: src}}); err == nil || !strings.Contains(err.Error(), "write oci-layout") {
		t.Fatalf("PackLayout(write oci-layout err): %v", err)
	}
}

func TestPackLayout_WriteIndexError(t *testing.T) {
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	swapWrite(t, "index.json")
	if _, _, err := PackLayout(t.TempDir(), "r:latest", []FileEntry{{Name: "x", Path: src}}); err == nil || !strings.Contains(err.Error(), "write index.json") {
		t.Fatalf("PackLayout(write index.json err): %v", err)
	}
}

func TestPackLayout_WriteManifestError(t *testing.T) {
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	// Allow layer blob write + config blob write; force ONLY the
	// manifest write to fail. We distinguish manifest writes from
	// the others by the size: manifestBody is the longest write.
	hexLayer := strings.TrimPrefix(Sha256Digest([]byte("hi")), "sha256:")
	origWrite := osWriteFile
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		// Always allow the layer blob. Fail any other blob write
		// (the next call is the config blob; the call after that is
		// the manifest blob). We let config succeed by only failing
		// the LARGEST-so-far blob path call.
		base := filepath.Base(name)
		if base == hexLayer || base == "index.json" || base == "oci-layout" {
			return origWrite(name, data, perm)
		}
		// First non-layer non-meta blob -> config (small JSON). Second -> manifest.
		// Heuristic: manifest is multi-line JSON with {\n; config is single-line {"created"...}.
		if strings.Contains(string(data), "\n  \"") {
			return errors.New("forced manifest blob error")
		}
		return origWrite(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = origWrite })
	if _, _, err := PackLayout(t.TempDir(), "r:latest", []FileEntry{{Name: "x", Path: src}}); err == nil || !strings.Contains(err.Error(), "write manifest blob") {
		t.Fatalf("PackLayout(write manifest err): %v", err)
	}
}

func TestServeLayout_ManifestBlobReadError(t *testing.T) {
	out := t.TempDir()
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	digest, _, err := PackLayout(out, "r:latest", []FileEntry{{Name: "x", Path: src}})
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	// Replace manifest blob with a directory -> ReadFile fails.
	hex := strings.TrimPrefix(digest, "sha256:")
	mPath := filepath.Join(out, "blobs", "sha256", hex)
	if err := os.Remove(mPath); err != nil {
		t.Fatalf("rm manifest: %v", err)
	}
	if err := os.Mkdir(mPath, 0o755); err != nil {
		t.Fatalf("mkdir manifest: %v", err)
	}
	if _, _, _, err := ServeLayout(out, "r", "/v2/r/manifests/latest"); err == nil {
		t.Fatal("ServeLayout(manifest EISDIR): want error")
	}
}

func TestServeLayout_BlobReadError(t *testing.T) {
	// Force the blob read path to fail with a non-NotExist error
	// by making the blob path a directory (read returns EISDIR).
	out := t.TempDir()
	src := writeTempFile(t, t.TempDir(), "x", []byte("hi"))
	digest, _, err := PackLayout(out, "r:latest", []FileEntry{{Name: "x", Path: src}})
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	// Replace the layer blob file with a directory at the same path.
	layerDigest := Sha256Digest([]byte("hi"))
	hex := strings.TrimPrefix(layerDigest, "sha256:")
	blobPath := filepath.Join(out, "blobs", "sha256", hex)
	if err := os.Remove(blobPath); err != nil {
		t.Fatalf("rm blob: %v", err)
	}
	if err := os.Mkdir(blobPath, 0o755); err != nil {
		t.Fatalf("mkdir blob: %v", err)
	}
	if _, _, _, err := ServeLayout(out, "r", "/v2/r/blobs/"+layerDigest); err == nil {
		t.Fatalf("ServeLayout(EISDIR): want error, manifest digest %s", digest)
	}
}

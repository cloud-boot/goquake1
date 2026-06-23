// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package ociassets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Test seams. Layout writes through os.WriteFile + json.Marshal; the
// few defensive error branches are unreachable from the happy real
// filesystem so tests swap these out to drive each one in turn.
var (
	osWriteFile  = os.WriteFile
	jsonMarshal  = json.Marshal
	jsonMarshalI = json.MarshalIndent
)

// FileEntry pairs a VFS-relative name with the on-disk path holding
// its bytes + a media type for the descriptor. The CLI packer
// consumes a slice of these to emit one OCI layer per entry.
type FileEntry struct {
	// Name is the VFS-relative file name (e.g. "pak0.pak",
	// "music/track02.ogg"). Stored verbatim in the manifest
	// annotation under [AnnotationPathPrefix].
	Name string

	// Path is the on-disk path the packer reads from.
	Path string

	// MediaType is recorded on the resulting layer descriptor.
	MediaType string
}

// PackLayout writes an OCI image-layout directory at outDir packing
// the given files as one layer each. Layout produced:
//
//	outDir/
//	  oci-layout
//	  index.json
//	  blobs/
//	    sha256/
//	      <hex of manifest digest>
//	      <hex of config digest>
//	      <hex of each layer digest>
//
// The reference (e.g. "quake-assets:latest") is recorded as the
// org.opencontainers.image.ref.name annotation on the index entry --
// that's the tag oras/podman/skopeo pull when the user pushes the
// directory to a registry.
//
// Returns the manifest's digest + size so the caller can echo them
// (handy for "what did I just produce" CLI banners).
func PackLayout(outDir, reference string, files []FileEntry) (manifestDigest string, manifestSize int64, err error) {
	if reference == "" {
		return "", 0, fmt.Errorf("ociassets: empty reference")
	}
	if len(files) == 0 {
		return "", 0, fmt.Errorf("ociassets: no files to pack")
	}
	// Stable layer ordering -> stable manifest digest across runs
	// with the same input set. Sort by VFS name (the tie-breaker
	// most humans expect when scanning the manifest).
	sorted := make([]FileEntry, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	blobsDir := filepath.Join(outDir, "blobs", "sha256")
	if err := os.MkdirAll(blobsDir, 0o755); err != nil {
		return "", 0, fmt.Errorf("ociassets: mkdir blobs: %w", err)
	}

	layers := make([]Descriptor, 0, len(sorted))
	annotations := make(map[string]string, len(sorted))
	for _, fe := range sorted {
		data, err := os.ReadFile(fe.Path)
		if err != nil {
			return "", 0, fmt.Errorf("ociassets: read %s: %w", fe.Path, err)
		}
		digest := Sha256Digest(data)
		hexPart := strings.TrimPrefix(digest, "sha256:")
		blobPath := filepath.Join(blobsDir, hexPart)
		if err := writeFileIfMissing(blobPath, data); err != nil {
			return "", 0, fmt.Errorf("ociassets: write blob %s: %w", blobPath, err)
		}
		mt := fe.MediaType
		if mt == "" {
			mt = "application/octet-stream"
		}
		layers = append(layers, Descriptor{MediaType: mt, Digest: digest, Size: int64(len(data))})
		annotations[AnnotationPathPrefix+fe.Name] = digest
	}

	// Minimal config blob: just record the layer count so registries
	// have something to attach the manifest to. (OCI requires a
	// config descriptor; the body can be any JSON we like as long as
	// the digest matches.)
	cfgBody, err := jsonMarshal(map[string]any{
		"created": "1970-01-01T00:00:00Z",
		"files":   len(layers),
	})
	if err != nil {
		return "", 0, fmt.Errorf("ociassets: marshal config: %w", err)
	}
	cfgDigest := Sha256Digest(cfgBody)
	cfgBlobPath := filepath.Join(blobsDir, strings.TrimPrefix(cfgDigest, "sha256:"))
	if err := writeFileIfMissing(cfgBlobPath, cfgBody); err != nil {
		return "", 0, fmt.Errorf("ociassets: write config blob: %w", err)
	}

	manifest := &Manifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeManifest,
		Config: Descriptor{
			MediaType: MediaTypeConfig,
			Digest:    cfgDigest,
			Size:      int64(len(cfgBody)),
		},
		Layers:      layers,
		Annotations: annotations,
	}
	manifestBody, err := encodeManifestSeam(manifest)
	if err != nil {
		return "", 0, fmt.Errorf("ociassets: marshal manifest: %w", err)
	}
	mDigest := Sha256Digest(manifestBody)
	mBlobPath := filepath.Join(blobsDir, strings.TrimPrefix(mDigest, "sha256:"))
	if err := writeFileIfMissing(mBlobPath, manifestBody); err != nil {
		return "", 0, fmt.Errorf("ociassets: write manifest blob: %w", err)
	}

	// oci-layout marker (required by the image-layout spec; readers
	// inspect it to confirm the directory shape they're parsing).
	layoutBody := []byte(`{"imageLayoutVersion": "1.0.0"}`)
	if err := osWriteFile(filepath.Join(outDir, "oci-layout"), layoutBody, 0o644); err != nil {
		return "", 0, fmt.Errorf("ociassets: write oci-layout: %w", err)
	}

	// index.json points at the manifest with the tag (= reference).
	indexBody, err := jsonMarshalI(map[string]any{
		"schemaVersion": 2,
		"manifests": []map[string]any{
			{
				"mediaType": MediaTypeManifest,
				"digest":    mDigest,
				"size":      len(manifestBody),
				"annotations": map[string]string{
					"org.opencontainers.image.ref.name": reference,
				},
			},
		},
	}, "", "  ")
	if err != nil {
		return "", 0, fmt.Errorf("ociassets: marshal index: %w", err)
	}
	if err := osWriteFile(filepath.Join(outDir, "index.json"), indexBody, 0o644); err != nil {
		return "", 0, fmt.Errorf("ociassets: write index.json: %w", err)
	}

	return mDigest, int64(len(manifestBody)), nil
}

// encodeManifestSeam is the swappable wrapper for [EncodeManifest]
// so tests can drive the defensive marshal-error branch (the real
// json.Marshal can't fail for our shape).
var encodeManifestSeam = EncodeManifest

// writeFileIfMissing writes data to path unless a file with the same
// digest already exists there (so re-running the packer with the
// same inputs is idempotent + cheap -- the manifest digest stays
// pinned).
func writeFileIfMissing(path string, data []byte) error {
	if st, err := os.Stat(path); err == nil && st.Size() == int64(len(data)) {
		return nil
	}
	return osWriteFile(path, data, 0o644)
}

// ServeLayout maps an OCI image-layout directory onto the path
// fragments the Distribution v2 API exposes. Used by the test fake
// registry + by the CLI's `--serve` mode for ad-hoc serving without
// a real registry.
//
// Returns the (manifestBytes, layerBytes, ok) triple for a given
// request path. The path forms recognised:
//
//	/v2/<repo>/manifests/<reference>  -> manifestBytes
//	/v2/<repo>/blobs/sha256:<hex>     -> layerBytes
//
// repo is fixed to "<reference-name-without-tag>" -- callers that
// pushed the layout under a different repo name pass that in.
func ServeLayout(layoutDir, repo, urlPath string) (body []byte, contentType string, status int, err error) {
	idxBody, err := os.ReadFile(filepath.Join(layoutDir, "index.json"))
	if err != nil {
		return nil, "", 0, err
	}
	var idx struct {
		Manifests []struct {
			MediaType   string            `json:"mediaType"`
			Digest      string            `json:"digest"`
			Size        int64             `json:"size"`
			Annotations map[string]string `json:"annotations"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(idxBody, &idx); err != nil {
		return nil, "", 0, fmt.Errorf("ociassets: decode index.json: %w", err)
	}

	// /v2/<repo>/manifests/<ref>
	mPrefix := "/v2/" + repo + "/manifests/"
	if strings.HasPrefix(urlPath, mPrefix) {
		ref := strings.TrimPrefix(urlPath, mPrefix)
		for _, m := range idx.Manifests {
			// ref can be either the tag or the digest.
			if m.Digest == ref || (m.Annotations["org.opencontainers.image.ref.name"] == ref || strings.HasSuffix(m.Annotations["org.opencontainers.image.ref.name"], ":"+ref)) {
				blob := filepath.Join(layoutDir, "blobs", "sha256", strings.TrimPrefix(m.Digest, "sha256:"))
				data, err := os.ReadFile(blob)
				if err != nil {
					return nil, "", 0, err
				}
				return data, m.MediaType, 200, nil
			}
		}
		return nil, "", 404, nil
	}
	// /v2/<repo>/blobs/<digest>
	bPrefix := "/v2/" + repo + "/blobs/"
	if strings.HasPrefix(urlPath, bPrefix) {
		digest := strings.TrimPrefix(urlPath, bPrefix)
		if !strings.HasPrefix(digest, "sha256:") {
			return nil, "", 400, nil
		}
		blob := filepath.Join(layoutDir, "blobs", "sha256", strings.TrimPrefix(digest, "sha256:"))
		data, err := os.ReadFile(blob)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, "", 404, nil
			}
			return nil, "", 0, err
		}
		return data, "application/octet-stream", 200, nil
	}
	return nil, "", 404, nil
}

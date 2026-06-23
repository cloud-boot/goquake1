// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package ociassets

import (
	"encoding/json"
	"errors"
	"strings"
)

// Media types used by this package. We keep them grouped in one place
// so the CLI packer + the runtime client agree on the wire vocabulary.
const (
	// MediaTypeManifest is what we set on the manifest body itself.
	// The Quake-specific suffix lets registries / browser dev-tools
	// differentiate a quake-assets manifest from a generic OCI image
	// manifest even though the schema is OCI v1-compatible.
	MediaTypeManifest = "application/vnd.go-quake1.assets.v1+json"

	// MediaTypeConfig is the descriptor.mediaType for the config blob
	// (a tiny JSON object that records architecture-independent
	// metadata; we mostly carry "created" + a "files" sidecar).
	MediaTypeConfig = "application/vnd.go-quake1.config.v1+json"

	// MediaTypeLayerPak is the descriptor.mediaType for the pak0
	// blob. application/octet-stream would work too but the explicit
	// vendor type makes the manifest self-describing.
	MediaTypeLayerPak = "application/vnd.go-quake1.pak.v1"

	// MediaTypeLayerMusic is the descriptor.mediaType for each
	// track*.ogg blob.
	MediaTypeLayerMusic = "audio/ogg"

	// AnnotationPathPrefix is prepended to each VFS-relative file name
	// in the manifest's annotation map ("quake.path/pak0.pak", ...).
	// The prefix lets us coexist with other annotation namespaces
	// (org.opencontainers.image.*) without collision.
	AnnotationPathPrefix = "quake.path/"
)

// Descriptor is the standard OCI content descriptor (mediaType + digest
// + size). We don't model the optional fields (URLs, annotations,
// platform) -- pak streaming doesn't need them.
type Descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// Manifest is the OCI v1 image-manifest JSON shape, narrowed to the
// fields this package reads or writes. Extra fields registries add on
// the wire are tolerated (json.Unmarshal drops unknown keys).
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType,omitempty"`
	Config        Descriptor        `json:"config"`
	Layers        []Descriptor      `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// ErrManifestNoLayers is returned when a manifest carries an empty
// layers array. Callers can't usefully build an FS over a manifest
// with no blobs so we surface this as a typed error rather than let
// later Open() calls fail one-by-one with "digest not found".
var ErrManifestNoLayers = errors.New("ociassets: manifest has no layers")

// ErrManifestNoAnnotations is returned when a manifest's annotations
// map carries no entries under [AnnotationPathPrefix]. Without the
// path->digest mapping the FS has no way to translate Open(name)
// requests into a blob fetch, so we fail fast at FS construction.
var ErrManifestNoAnnotations = errors.New("ociassets: manifest has no quake.path/* annotations")

// DecodeManifest parses raw JSON into a [Manifest]. The schemaVersion
// is required to be 2 (the only OCI image-manifest version we
// understand); any other value returns an error.
func DecodeManifest(body []byte) (*Manifest, error) {
	m := &Manifest{}
	if err := json.Unmarshal(body, m); err != nil {
		return nil, err
	}
	if m.SchemaVersion != 2 {
		return nil, errors.New("ociassets: manifest schemaVersion must be 2")
	}
	return m, nil
}

// BuildFileMap walks m.Annotations and returns a vfs-name -> digest
// map keyed without the [AnnotationPathPrefix]. The result is the
// argument [NewFS] takes for its fileMap parameter.
//
// Returns [ErrManifestNoAnnotations] when no annotations carry the
// quake.path/ prefix (likely a manifest from a different producer).
func BuildFileMap(m *Manifest) (map[string]string, error) {
	out := make(map[string]string, len(m.Annotations))
	for k, v := range m.Annotations {
		if !strings.HasPrefix(k, AnnotationPathPrefix) {
			continue
		}
		name := strings.TrimPrefix(k, AnnotationPathPrefix)
		if name == "" || v == "" {
			continue
		}
		out[name] = v
	}
	if len(out) == 0 {
		return nil, ErrManifestNoAnnotations
	}
	return out, nil
}

// EncodeManifest is the round-trip companion of [DecodeManifest]. It
// emits canonical JSON (no HTML escaping, stable two-space indent)
// suitable for writing to an OCI image-layout `blobs/sha256/<digest>`
// file.
func EncodeManifest(m *Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

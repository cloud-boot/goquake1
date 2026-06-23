// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package ociassets

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"sort"
	"sync"
	"time"
)

// FS implements [io/fs.FS] over an OCI registry. Open(name) looks up
// the file's layer digest in the manifest's annotation map, GETs the
// blob from the registry (the first call only -- subsequent calls
// hit the in-memory cache), and returns an [io/fs.File] over the
// bytes.
//
// FS is safe for concurrent use: per-file locks serialize the
// first-time fetch while still letting unrelated Open calls run in
// parallel.
type FS struct {
	client  *Client
	repo    string
	fileMap map[string]string // vfs-name -> "sha256:..."

	mu        sync.Mutex
	inflight  map[string]*pending
	cache     map[string][]byte
	persisted persistCache // wasm: IndexedDB; host: no-op

	// nowFn / ctxFn are injectable seams so tests can drive the
	// timestamp embedded in the returned fs.FileInfo + the context
	// the blob fetch is issued with.
	nowFn func() time.Time
	ctxFn func() context.Context
}

// pending is the single-flight handle that fans out a concurrent
// Open(name) burst to ONE blob fetch. Holders Wait on done; the
// initiator fills bytes (and err) before closing it.
type pending struct {
	done  chan struct{}
	bytes []byte
	err   error
}

// NewFS constructs an FS for the given manifest reference + file
// map. The fileMap is typically produced by [BuildFileMap] but the
// constructor accepts any map so the CLI / tests can supply a
// hand-built one.
//
// Returns ErrManifestNoAnnotations when fileMap is empty (FS would be
// useless and Open would always fail).
func NewFS(client *Client, repo string, fileMap map[string]string) (*FS, error) {
	if len(fileMap) == 0 {
		return nil, ErrManifestNoAnnotations
	}
	return &FS{
		client:    client,
		repo:      repo,
		fileMap:   fileMap,
		inflight:  make(map[string]*pending),
		cache:     make(map[string][]byte),
		persisted: defaultPersistCache(),
		nowFn:     time.Now,
		ctxFn:     context.Background,
	}, nil
}

// NewFSFromManifest is a convenience that fetches + decodes the
// manifest then constructs the FS. Equivalent to:
//
//	m, _ := client.Manifest(ctx, repo, ref)
//	fm, _ := BuildFileMap(m)
//	return NewFS(client, repo, fm)
//
// with the errors wired through.
func NewFSFromManifest(ctx context.Context, client *Client, repo, reference string) (*FS, error) {
	m, err := client.Manifest(ctx, repo, reference)
	if err != nil {
		return nil, err
	}
	if len(m.Layers) == 0 {
		return nil, ErrManifestNoLayers
	}
	fm, err := BuildFileMap(m)
	if err != nil {
		return nil, err
	}
	return NewFS(client, repo, fm)
}

// Open implements [io/fs.FS]. Name is the VFS-relative path under the
// quake.path/ annotation namespace (e.g. "pak0.pak",
// "music/track02.ogg"). Lookups are exact (no directory listing) --
// this matches what the engine's pak/vfs subsystem asks for.
func (f *FS) Open(name string) (fs.File, error) {
	name = path.Clean(name)
	digest, ok := f.fileMap[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	data, err := f.load(name, digest)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return &memFile{
		name:    name,
		data:    data,
		modTime: f.nowFn(),
	}, nil
}

// load returns the bytes for digest, fetching them from the registry
// on first call and caching the result for later. Concurrent calls
// for the same digest collapse to a single fetch (single-flight).
func (f *FS) load(name, digest string) ([]byte, error) {
	f.mu.Lock()
	if data, ok := f.cache[digest]; ok {
		f.mu.Unlock()
		return data, nil
	}
	if p, ok := f.inflight[digest]; ok {
		f.mu.Unlock()
		<-p.done
		return p.bytes, p.err
	}
	// Try the persistent cache (wasm IndexedDB on GOOS=js, no-op
	// elsewhere). A hit promotes to the in-memory cache + skips the
	// registry round-trip entirely.
	if data, ok := f.persisted.Get(digest); ok {
		f.cache[digest] = data
		f.mu.Unlock()
		return data, nil
	}
	p := &pending{done: make(chan struct{})}
	f.inflight[digest] = p
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		delete(f.inflight, digest)
		f.mu.Unlock()
		close(p.done)
	}()

	rc, err := f.client.Blob(f.ctxFn(), f.repo, digest, nil)
	if err != nil {
		p.err = err
		return nil, err
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		p.err = err
		return nil, err
	}
	if err := VerifyDigest(body, digest); err != nil {
		p.err = err
		return nil, err
	}
	f.mu.Lock()
	f.cache[digest] = body
	f.mu.Unlock()
	f.persisted.Put(digest, body)
	p.bytes = body
	_ = name // kept for future per-name metrics
	return body, nil
}

// Names returns the sorted VFS-relative names known to the FS. Used
// by the CLI tool's verify mode + by tests.
func (f *FS) Names() []string {
	out := make([]string, 0, len(f.fileMap))
	for k := range f.fileMap {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// memFile is the [io/fs.File] adapter wrapping the cached bytes for
// one logical entry. Reads consume an internal byte cursor; Seek lets
// the engine's pak parser jump around within a small file.
type memFile struct {
	name    string
	data    []byte
	pos     int64
	modTime time.Time
	closed  bool
}

func (f *memFile) Read(p []byte) (int, error) {
	if f.closed {
		return 0, errClosed
	}
	if f.pos >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += int64(n)
	return n, nil
}

func (f *memFile) ReadAt(p []byte, off int64) (int, error) {
	if f.closed {
		return 0, errClosed
	}
	if off < 0 || off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *memFile) Seek(offset int64, whence int) (int64, error) {
	if f.closed {
		return 0, errClosed
	}
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = f.pos + offset
	case io.SeekEnd:
		abs = int64(len(f.data)) + offset
	default:
		return 0, errInvalidWhence
	}
	if abs < 0 {
		return 0, errNegativeSeek
	}
	f.pos = abs
	return abs, nil
}

func (f *memFile) Close() error {
	f.closed = true
	return nil
}

func (f *memFile) Stat() (fs.FileInfo, error) {
	return &memFileInfo{name: f.name, size: int64(len(f.data)), modTime: f.modTime}, nil
}

// Bytes returns a reference to the underlying byte slice. Useful for
// callers that need bytes.NewReader-style random access without
// pulling in the io/fs adapter overhead.
func (f *memFile) Bytes() []byte { return f.data }

var (
	errClosed        = errors.New("ociassets: file closed")
	errInvalidWhence = errors.New("ociassets: invalid Seek whence")
	errNegativeSeek  = errors.New("ociassets: negative Seek offset")
)

type memFileInfo struct {
	name    string
	size    int64
	modTime time.Time
}

func (i *memFileInfo) Name() string       { return path.Base(i.name) }
func (i *memFileInfo) Size() int64        { return i.size }
func (i *memFileInfo) Mode() fs.FileMode  { return 0o444 }
func (i *memFileInfo) ModTime() time.Time { return i.modTime }
func (i *memFileInfo) IsDir() bool        { return false }
func (i *memFileInfo) Sys() any           { return nil }

// ensure memFile satisfies what callers expect.
var (
	_ fs.File     = (*memFile)(nil)
	_ io.ReaderAt = (*memFile)(nil)
	_ io.Seeker   = (*memFile)(nil)
)

// nopReadCloser wraps bytes for callers that want an io.ReadCloser
// over a precomputed buffer. Kept package-local so tests can use it
// when injecting a fake HTTP body.
type nopReadCloser struct{ *bytes.Reader }

func (nopReadCloser) Close() error { return nil }

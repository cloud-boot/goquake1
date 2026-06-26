// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// Package ociassets exposes an [io/fs.FS] backed by an OCI registry so
// the wasm Quake build can stream pak0.pak + the music tracks on
// demand instead of embedding them. The wasm payload stays ~10 MB; the
// ~180 MB pak + ~84 MB of OGG music ship as separate registry layers
// that the browser fetches the first time a file is Open'd.
//
// Wire shape:
//
//   - [Client] talks to an OCI Distribution v2 registry origin. The
//     two operations we use are GET /v2/<repo>/manifests/<reference>
//     (returns the JSON manifest body) and GET /v2/<repo>/blobs/<digest>
//     (returns the layer bytes; supports HTTP Range so future
//     incremental reads stay cheap).
//
//   - [Manifest] is the standard OCI v1 image-manifest shape with the
//     `quake.path/<vfs-name>` annotation namespace recording which
//     layer digest holds which file inside the Quake VFS. The
//     [BuildFileMap] helper extracts that map.
//
//   - [FS] turns the (Client + reference + fileMap) triple into an
//     [io/fs.FS] -- Open(name) looks up the digest, fetches the blob,
//     caches the bytes in memory, and returns an [io/fs.File] over
//     them. On GOOS=js GOARCH=wasm, a build-tagged write-through cache
//     in fs_wasm.go persists each successful fetch to IndexedDB so a
//     page reload skips the network round-trip.
//
// The package is pure-Go and CGO=0. On wasm Go's net/http already
// routes through the browser fetch() API, so [Client.Blob] works
// inside the wasm runtime without any JS glue.
package ociassets

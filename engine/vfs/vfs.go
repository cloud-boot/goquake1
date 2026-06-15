// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package vfs

import (
	"errors"
	"io/fs"
	"sort"
)

// SearchPath is the ordered overlay of one or more [fs.FS] sources.
// Open walks them in order, returning the first hit. tyrquake:
// searchpath_t linked list rooted at com_searchpaths.
type SearchPath struct {
	sources []fs.FS
}

// New returns an empty SearchPath. Calls to Add prepend, matching the
// tyrquake convention where later adds override earlier ones.
func New() *SearchPath { return &SearchPath{} }

// Add prepends src to the search order so subsequent Open calls
// resolve through src before falling back to existing sources. Nil
// src is silently ignored so callers can pass an optional override
// without a guard. tyrquake call shape: `search->next =
// com_searchpaths; com_searchpaths = search`.
func (s *SearchPath) Add(src fs.FS) {
	if src == nil {
		return
	}
	s.sources = append([]fs.FS{src}, s.sources...)
}

// Len returns the number of sources in the chain.
func (s *SearchPath) Len() int { return len(s.sources) }

// Open implements [fs.FS]. Walks sources in declared order
// (most-recently-added first) and returns the first one that yields
// a file. Returns fs.ErrNotExist when no source has the path; any
// non-NotExist error from a source short-circuits the walk so a real
// I/O failure is not silently masked by a fallback. tyrquake:
// COM_FindFile / COM_OpenFile.
func (s *SearchPath) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	for _, src := range s.sources {
		f, err := src.Open(name)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadDir implements [fs.ReadDirFS]. Returns the deduplicated union
// of all sources' entries at name. First-source-wins on name
// collisions (matches Open's override semantics). tyrquake's C form
// re-walks the search list per request; the Go port collapses that
// into one pass.
func (s *SearchPath) ReadDir(name string) ([]fs.DirEntry, error) {
	seen := make(map[string]fs.DirEntry)
	found := false
	for _, src := range s.sources {
		entries, err := fs.ReadDir(src, name)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		found = true
		for _, e := range entries {
			if _, ok := seen[e.Name()]; !ok {
				seen[e.Name()] = e
			}
		}
	}
	if !found {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]fs.DirEntry, 0, len(names))
	for _, n := range names {
		out = append(out, seen[n])
	}
	return out, nil
}

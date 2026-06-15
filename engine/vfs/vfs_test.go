// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package vfs

import (
	"errors"
	"io"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestNew_Empty(t *testing.T) {
	s := New()
	if s.Len() != 0 {
		t.Errorf("Len: %d", s.Len())
	}
	if _, err := s.Open("foo"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open empty: got %v", err)
	}
	if _, err := s.ReadDir("."); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadDir empty: got %v", err)
	}
}

func TestAdd_Nil(t *testing.T) {
	s := New()
	s.Add(nil)
	if s.Len() != 0 {
		t.Errorf("nil Add should be a no-op; Len=%d", s.Len())
	}
}

func TestOpen_FirstHitWins(t *testing.T) {
	// pak0: has "a" + "b"
	// pak1: has "a" (different content) + "c"
	// After s.Add(pak0); s.Add(pak1): pak1 sits in FRONT so "a"
	// resolves to pak1's version (override semantics).
	pak0 := fstest.MapFS{
		"a": &fstest.MapFile{Data: []byte("from-pak0")},
		"b": &fstest.MapFile{Data: []byte("only-in-pak0")},
	}
	pak1 := fstest.MapFS{
		"a": &fstest.MapFile{Data: []byte("from-pak1")},
		"c": &fstest.MapFile{Data: []byte("only-in-pak1")},
	}
	s := New()
	s.Add(pak0)
	s.Add(pak1)
	if s.Len() != 2 {
		t.Errorf("Len: %d", s.Len())
	}

	mustRead := func(name string) string {
		t.Helper()
		f, err := s.Open(name)
		if err != nil {
			t.Fatalf("Open(%q): %v", name, err)
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		return string(data)
	}
	if got := mustRead("a"); got != "from-pak1" {
		t.Errorf("a: got %q want from-pak1 (pak1 should override)", got)
	}
	if got := mustRead("b"); got != "only-in-pak0" {
		t.Errorf("b: got %q", got)
	}
	if got := mustRead("c"); got != "only-in-pak1" {
		t.Errorf("c: got %q", got)
	}
}

func TestOpen_NotExist(t *testing.T) {
	s := New()
	s.Add(fstest.MapFS{"a": &fstest.MapFile{}})
	_, err := s.Open("nope")
	var pe *fs.PathError
	if !errors.As(err, &pe) || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("got %v want fs.ErrNotExist (PathError-wrapped)", err)
	}
}

func TestOpen_InvalidPath(t *testing.T) {
	s := New()
	s.Add(fstest.MapFS{"a": &fstest.MapFile{}})
	if _, err := s.Open("../escape"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("got %v want fs.ErrInvalid", err)
	}
}

// failingFS returns a non-NotExist error from Open and ReadDir to
// exercise the short-circuit branches in SearchPath.
type failingFS struct{ err error }

func (f failingFS) Open(string) (fs.File, error) { return nil, f.err }
func (f failingFS) ReadDir(string) ([]fs.DirEntry, error) {
	return nil, f.err
}

func TestOpen_NonNotExistErrorShortCircuits(t *testing.T) {
	custom := errors.New("disk on fire")
	s := New()
	s.Add(fstest.MapFS{"a": &fstest.MapFile{Data: []byte("ok")}})
	s.Add(failingFS{err: custom}) // prepended -> walked first
	_, err := s.Open("a")
	if !errors.Is(err, custom) {
		t.Errorf("got %v want short-circuit on custom error", err)
	}
}

func TestReadDir_UnionAndOverride(t *testing.T) {
	pak0 := fstest.MapFS{
		"a.txt":          &fstest.MapFile{Data: []byte("pak0-a")},
		"b.txt":          &fstest.MapFile{Data: []byte("pak0-b")},
		"maps/start.bsp": &fstest.MapFile{Data: []byte("pak0-bsp")},
	}
	pak1 := fstest.MapFS{
		"a.txt":         &fstest.MapFile{Data: []byte("pak1-a")},
		"maps/e1m1.bsp": &fstest.MapFile{Data: []byte("pak1-e1m1")},
	}
	s := New()
	s.Add(pak0)
	s.Add(pak1)

	entries, err := s.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	want := []string{"a.txt", "b.txt", "maps"}
	for _, w := range want {
		if !got[w] {
			t.Errorf("ReadDir missing %q", w)
		}
	}
}

func TestReadDir_NotExist(t *testing.T) {
	s := New()
	s.Add(fstest.MapFS{"a.txt": &fstest.MapFile{}})
	_, err := s.ReadDir("missing-dir")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("got %v want fs.ErrNotExist", err)
	}
}

func TestReadDir_NonNotExistErrorShortCircuits(t *testing.T) {
	custom := errors.New("readdir blew up")
	s := New()
	s.Add(fstest.MapFS{"a.txt": &fstest.MapFile{Data: []byte("x")}})
	s.Add(failingFS{err: custom})
	_, err := s.ReadDir(".")
	if !errors.Is(err, custom) {
		t.Errorf("got %v want custom error", err)
	}
}

// fs.ReadFile across the search path is the production access path
// for engine/common.LoadFile -- verify it works end-to-end.
func TestSearchPath_FSReadFile(t *testing.T) {
	s := New()
	s.Add(fstest.MapFS{"x": &fstest.MapFile{Data: []byte("hello")}})
	data, err := fs.ReadFile(s, "x")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q", data)
	}
}

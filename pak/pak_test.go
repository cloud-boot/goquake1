// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pak

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"testing"
)

// buildPAK constructs an in-memory PAK archive from a name->payload
// map and returns a bytes.Reader suitable for Open.
func buildPAK(entries map[string][]byte) *bytes.Reader {
	// Layout: 12-byte header + payloads + directory.
	hdr := make([]byte, headerSize)
	copy(hdr[0:4], "PACK")

	type packed struct {
		name string
		data []byte
		off  int32
	}
	all := make([]packed, 0, len(entries))
	// Sort by insertion via a sorted key list so the test is
	// reproducible across map-iteration orders.
	names := make([]string, 0, len(entries))
	for k := range entries {
		names = append(names, k)
	}
	// Stable: just append in the iteration order we collected.
	for _, n := range names {
		all = append(all, packed{name: n, data: entries[n]})
	}

	buf := bytes.NewBuffer(hdr)
	// Reserve correct offsets relative to file start.
	for i := range all {
		all[i].off = int32(buf.Len())
		buf.Write(all[i].data)
	}
	dirOff := int32(buf.Len())
	for _, e := range all {
		nameBuf := make([]byte, nameLen)
		copy(nameBuf, e.name)
		buf.Write(nameBuf)
		_ = binary.Write(buf, binary.LittleEndian, e.off)
		_ = binary.Write(buf, binary.LittleEndian, int32(len(e.data)))
	}
	dirLen := int32(buf.Len()) - dirOff

	// Patch header dirofs/dirlen.
	out := buf.Bytes()
	binary.LittleEndian.PutUint32(out[4:8], uint32(dirOff))
	binary.LittleEndian.PutUint32(out[8:12], uint32(dirLen))
	return bytes.NewReader(out)
}

func TestOpen_ReadBack(t *testing.T) {
	src := buildPAK(map[string][]byte{
		"foo.txt":        []byte("hello"),
		"maps/start.bsp": []byte("BSP-bytes"),
	})
	p, err := Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if p.Len() != 2 {
		t.Errorf("Len: got %d want 2", p.Len())
	}
	f, err := p.Open("foo.txt")
	if err != nil {
		t.Fatalf("Open foo.txt: %v", err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("payload: got %q want hello", got)
	}
}

func TestOpen_NilSrc(t *testing.T) {
	if _, err := Open(nil); err == nil {
		t.Error("expected nil-src error")
	}
}

func TestOpen_BadMagic(t *testing.T) {
	src := bytes.NewReader(append([]byte("NOPE"), make([]byte, 8)...))
	if _, err := Open(src); !errors.Is(err, ErrBadMagic) {
		t.Errorf("got %v want ErrBadMagic", err)
	}
}

func TestOpen_BadDirectoryLength(t *testing.T) {
	// dirlen = 7 (not a multiple of 64), dirofs = 12.
	hdr := make([]byte, headerSize+7)
	copy(hdr[0:4], "PACK")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(headerSize))
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(7))
	if _, err := Open(bytes.NewReader(hdr)); !errors.Is(err, ErrBadDirectory) {
		t.Errorf("got %v want ErrBadDirectory", err)
	}
}

func TestOpen_HeaderShortRead(t *testing.T) {
	// Only 4 bytes -- header read fails before magic check.
	if _, err := Open(bytes.NewReader([]byte("PACK"))); err == nil {
		t.Error("expected short-read error")
	}
}

func TestOpen_DirShortRead(t *testing.T) {
	// Magic OK, dirofs points past EOF.
	hdr := make([]byte, headerSize)
	copy(hdr[0:4], "PACK")
	binary.LittleEndian.PutUint32(hdr[4:8], 1000) // way past EOF
	binary.LittleEndian.PutUint32(hdr[8:12], 64)
	if _, err := Open(bytes.NewReader(hdr)); err == nil {
		t.Error("expected dir short-read error")
	}
}

func TestOpen_MissingFile(t *testing.T) {
	src := buildPAK(map[string][]byte{"foo.txt": []byte("hello")})
	p, _ := Open(src)
	_, err := p.Open("nope.txt")
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("got %v want fs.ErrNotExist", err)
	}
}

func TestOpen_InvalidPath(t *testing.T) {
	src := buildPAK(map[string][]byte{"foo.txt": []byte("hello")})
	p, _ := Open(src)
	if _, err := p.Open("../foo.txt"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("got %v want fs.ErrInvalid", err)
	}
}

func TestFile_StatAndRead(t *testing.T) {
	src := buildPAK(map[string][]byte{"x": []byte("abcdef")})
	p, _ := Open(src)
	f, _ := p.Open("x")
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Name() != "x" || info.Size() != 6 || info.IsDir() {
		t.Errorf("Stat: %+v", info)
	}
	if info.Mode() != 0444 {
		t.Errorf("Mode: %v", info.Mode())
	}
	if !info.ModTime().IsZero() {
		t.Error("ModTime should be zero")
	}
	if info.Sys() != nil {
		t.Error("Sys should be nil")
	}

	// Partial reads.
	buf := make([]byte, 3)
	n, err := f.Read(buf)
	if n != 3 || err != nil || string(buf) != "abc" {
		t.Errorf("first read: n=%d err=%v buf=%q", n, err, buf)
	}
	n, err = f.Read(buf)
	if n != 3 || err != nil || string(buf) != "def" {
		t.Errorf("second read: n=%d err=%v buf=%q", n, err, buf)
	}
	n, err = f.Read(buf)
	if n != 0 || err != io.EOF {
		t.Errorf("EOF: n=%d err=%v", n, err)
	}
}

func TestRootDir_ReadDir(t *testing.T) {
	src := buildPAK(map[string][]byte{
		"b.txt":      []byte("b"),
		"a.txt":      []byte("a"),
		"maps/c.bsp": []byte("c"),
	})
	p, _ := Open(src)
	root, err := p.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat(); err != nil {
		t.Error(err)
	}
	if _, err := root.Read(make([]byte, 4)); err == nil {
		t.Error("root.Read should fail")
	}
	if err := root.Close(); err != nil {
		t.Error(err)
	}
	rd, _ := root.(fs.ReadDirFile)
	entries, err := rd.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("ReadDir count: %d want 3", len(entries))
	}
	// Sorted lexically.
	want := []string{"a.txt", "b.txt", "maps/c.bsp"}
	for i, e := range entries {
		if e.Name() != want[i] {
			t.Errorf("entry %d: got %q want %q", i, e.Name(), want[i])
		}
		if e.IsDir() {
			t.Errorf("entry %d: IsDir should be false", i)
		}
		if e.Type() != 0 {
			t.Errorf("entry %d: Type bits should be 0 for files, got %v", i, e.Type())
		}
		// Info wrapper roundtrips.
		info, err := e.Info()
		if err != nil || info.Name() != e.Name() {
			t.Errorf("entry %d: Info mismatch", i)
		}
	}
	// Second ReadDir returns EOF.
	if _, err := rd.ReadDir(-1); err != io.EOF {
		t.Errorf("second ReadDir: got %v want EOF", err)
	}
}

func TestReadDir_NonRoot(t *testing.T) {
	src := buildPAK(map[string][]byte{"foo.txt": []byte("hello")})
	p, _ := Open(src)
	if _, err := p.ReadDir("maps"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("got %v want fs.ErrNotExist", err)
	}
}

// Round-trip via the fs.FS interface (the production access path).
func TestFS_Compatible(t *testing.T) {
	src := buildPAK(map[string][]byte{"hello.txt": []byte("world")})
	p, _ := Open(src)
	data, err := fs.ReadFile(p, "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "world" {
		t.Errorf("got %q want world", data)
	}
}

// Directory entry with the full 56-byte name (no NUL terminator
// inside the slot) is read correctly.
func TestEntry_NameFillsSlot(t *testing.T) {
	longName := "" // build a 56-char name
	for i := 0; i < nameLen; i++ {
		longName += "a"
	}
	src := buildPAK(map[string][]byte{longName: []byte("x")})
	p, err := Open(src)
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(p, longName)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "x" {
		t.Errorf("payload: %q", data)
	}
}

// Directory mode bits report directory.
func TestFileInfo_DirMode(t *testing.T) {
	info := &fileInfo{name: "x", dir: true}
	if !info.IsDir() {
		t.Error("IsDir")
	}
	if info.Mode()&fs.ModeDir == 0 {
		t.Errorf("Mode missing ModeDir: %v", info.Mode())
	}
}

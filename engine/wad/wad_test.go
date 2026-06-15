// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package wad

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"testing"
)

// buildWAD synthesises a minimal-valid WAD2 archive from a name ->
// (type, payload) map. Names that are >16 chars are truncated to
// match upstream W_CleanupName behaviour.
type spec struct {
	name    string
	typ     byte
	payload []byte
}

func buildWAD(entries []spec) *bytes.Reader {
	buf := &bytes.Buffer{}
	// Header placeholder; patched after we know infotableofs.
	buf.Write(make([]byte, headerSize))

	type packed struct {
		spec
		filepos int32
	}
	all := make([]packed, len(entries))
	for i, e := range entries {
		all[i] = packed{spec: e, filepos: int32(buf.Len())}
		buf.Write(e.payload)
	}
	infotableofs := int32(buf.Len())
	for _, e := range all {
		entry := make([]byte, lumpEntrySize)
		binary.LittleEndian.PutUint32(entry[0:4], uint32(e.filepos))
		binary.LittleEndian.PutUint32(entry[4:8], uint32(len(e.payload)))
		binary.LittleEndian.PutUint32(entry[8:12], uint32(len(e.payload)))
		entry[12] = e.typ
		entry[13] = CmpNone
		// Name slot (16 bytes, NUL-padded).
		n := e.name
		if len(n) > nameLen {
			n = n[:nameLen]
		}
		copy(entry[16:32], n)
		buf.Write(entry)
	}
	out := buf.Bytes()
	copy(out[0:4], magic)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(all)))
	binary.LittleEndian.PutUint32(out[8:12], uint32(infotableofs))
	return bytes.NewReader(out)
}

func TestOpen_Roundtrip(t *testing.T) {
	src := buildWAD([]spec{
		{name: "PALETTE", typ: TypPalette, payload: bytes.Repeat([]byte{0xAB}, 768)},
		{name: "Hello", typ: TypQPic, payload: []byte("payload-data")},
	})
	w, err := Open(src)
	if err != nil {
		t.Fatal(err)
	}
	if w.Len() != 2 {
		t.Errorf("Len: %d", w.Len())
	}
	// Names are stored lowercased.
	for _, want := range []string{"palette", "hello"} {
		if _, ok := w.byName[want]; !ok {
			t.Errorf("missing canonical name %q", want)
		}
	}
}

func TestOpen_CaseInsensitive(t *testing.T) {
	src := buildWAD([]spec{{name: "MyLump", typ: TypQPic, payload: []byte("x")}})
	w, _ := Open(src)
	for _, q := range []string{"mylump", "MYLUMP", "MyLump"} {
		f, err := w.Open(q)
		if err != nil {
			t.Errorf("Open(%q): %v", q, err)
			continue
		}
		f.Close()
	}
}

func TestOpen_ReadPayload(t *testing.T) {
	src := buildWAD([]spec{{name: "data", typ: TypQPic, payload: []byte("hello")}})
	w, _ := Open(src)
	got, err := fs.ReadFile(w, "data")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q want hello", got)
	}
}

func TestOpen_NilSrc(t *testing.T) {
	if _, err := Open(nil); err == nil {
		t.Error("expected nil-src error")
	}
}

func TestOpen_BadMagic(t *testing.T) {
	bad := bytes.NewReader(append([]byte("NOPE"), make([]byte, 8)...))
	if _, err := Open(bad); !errors.Is(err, ErrBadMagic) {
		t.Errorf("got %v want ErrBadMagic", err)
	}
}

func TestOpen_HeaderShortRead(t *testing.T) {
	if _, err := Open(bytes.NewReader([]byte("WAD2"))); err == nil {
		t.Error("expected short-read error")
	}
}

func TestOpen_DirShortRead(t *testing.T) {
	// Valid magic + infotableofs that points past EOF.
	hdr := make([]byte, headerSize)
	copy(hdr[0:4], magic)
	binary.LittleEndian.PutUint32(hdr[4:8], 1)         // numlumps
	binary.LittleEndian.PutUint32(hdr[8:12], 1<<20)    // way past EOF
	if _, err := Open(bytes.NewReader(hdr)); err == nil {
		t.Error("expected dir short-read error")
	}
}

func TestOpen_NegativeFields(t *testing.T) {
	hdr := make([]byte, headerSize)
	copy(hdr[0:4], magic)
	binary.LittleEndian.PutUint32(hdr[4:8], 0xFFFFFFFF) // -1 as int32 bits
	if _, err := Open(bytes.NewReader(hdr)); !errors.Is(err, ErrShortRead) {
		t.Errorf("got %v want ErrShortRead on negative numlumps", err)
	}
}

func TestOpen_MissingFile(t *testing.T) {
	src := buildWAD([]spec{{name: "x", typ: TypQPic, payload: []byte("y")}})
	w, _ := Open(src)
	_, err := w.Open("nope")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("got %v want fs.ErrNotExist", err)
	}
}

func TestOpen_InvalidPath(t *testing.T) {
	src := buildWAD([]spec{{name: "x", typ: TypQPic, payload: []byte("y")}})
	w, _ := Open(src)
	if _, err := w.Open("../escape"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("got %v want fs.ErrInvalid", err)
	}
}

func TestLumps_Copy(t *testing.T) {
	src := buildWAD([]spec{{name: "a", typ: TypQPic, payload: []byte("p")}})
	w, _ := Open(src)
	lumps := w.Lumps()
	if len(lumps) != 1 {
		t.Fatal("Lumps count")
	}
	// Mutating the returned slice must not affect internal state.
	lumps[0].Name = "MUTATED"
	if w.lumps[0].Name == "MUTATED" {
		t.Error("Lumps() returned aliased slice; should return a copy")
	}
}

func TestFile_StatAndRead(t *testing.T) {
	src := buildWAD([]spec{{name: "x", typ: TypQPic, payload: []byte("abcdef")}})
	w, _ := Open(src)
	f, _ := w.Open("x")
	info, _ := f.Stat()
	if info.Name() != "x" || info.Size() != 6 || info.IsDir() {
		t.Errorf("Stat: %+v", info)
	}
	if info.Mode() != 0444 || info.Sys() != nil || !info.ModTime().IsZero() {
		t.Error("Stat metadata wrong")
	}
	// Partial reads.
	buf := make([]byte, 3)
	if n, err := f.Read(buf); n != 3 || err != nil || string(buf) != "abc" {
		t.Errorf("first read: n=%d err=%v buf=%q", n, err, buf)
	}
	if n, err := f.Read(buf); n != 3 || err != nil || string(buf) != "def" {
		t.Errorf("second read: n=%d err=%v buf=%q", n, err, buf)
	}
	if _, err := f.Read(buf); err != io.EOF {
		t.Errorf("EOF: got %v", err)
	}
	if err := f.Close(); err != nil {
		t.Error(err)
	}
}

func TestRootDir(t *testing.T) {
	src := buildWAD([]spec{
		{name: "z", typ: TypQPic, payload: []byte("z")},
		{name: "a", typ: TypQPic, payload: []byte("a")},
	})
	w, _ := Open(src)
	root, err := w.Open(".")
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
	rd := root.(fs.ReadDirFile)
	entries, err := rd.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "z"}
	if len(entries) != 2 || entries[0].Name() != want[0] || entries[1].Name() != want[1] {
		t.Errorf("ReadDir: got %v want %v", entries, want)
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Error("entry should not be a dir")
		}
		if e.Type() != 0 {
			t.Error("file entries should have zero Type bits")
		}
		info, err := e.Info()
		if err != nil || info.Name() != e.Name() {
			t.Errorf("Info: %v", err)
		}
	}
	// Second ReadDir returns EOF.
	if _, err := rd.ReadDir(-1); err != io.EOF {
		t.Errorf("second ReadDir: got %v want EOF", err)
	}
}

func TestReadDir_NonRoot(t *testing.T) {
	src := buildWAD([]spec{{name: "x", typ: TypQPic, payload: []byte("p")}})
	w, _ := Open(src)
	if _, err := w.ReadDir("subdir"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("got %v want fs.ErrNotExist", err)
	}
}

// Entry whose 16-byte name slot is completely filled (no NUL) is
// read correctly without overflow.
func TestEntry_NameFillsSlot(t *testing.T) {
	name := "aaaaaaaaaaaaaaaa" // exactly 16
	src := buildWAD([]spec{{name: name, typ: TypQPic, payload: []byte("p")}})
	w, _ := Open(src)
	if _, ok := w.byName[name]; !ok {
		t.Errorf("16-char name missing from byName: %v", w.byName)
	}
}

func TestFileInfo_DirMode(t *testing.T) {
	info := &fileInfo{name: ".", dir: true}
	if !info.IsDir() {
		t.Error("IsDir")
	}
	if info.Mode()&fs.ModeDir == 0 {
		t.Errorf("Mode missing ModeDir: %v", info.Mode())
	}
}

func TestCleanName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"FOO", "foo"},
		{"MixedCase", "mixedcase"},
		{"abc\x00trailing", "abc"},  // NUL truncates
		{"123_-", "123_-"},          // non-alpha preserved
	}
	for _, c := range cases {
		if got := cleanName(c.in); got != c.want {
			t.Errorf("cleanName(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

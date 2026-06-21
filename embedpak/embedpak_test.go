// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package embedpak

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/go-quake1/engine/vfs"
)

// withEmbedded swaps embeddedBytes for the duration of f and restores
// it afterwards. Lets the non-empty path be exercised without putting
// a real shareware pak0.pak in the repository.
func withEmbedded(t *testing.T, blob []byte, f func()) {
	t.Helper()
	saved := embeddedBytes
	embeddedBytes = blob
	defer func() { embeddedBytes = saved }()
	f()
}

// makeSyntheticPak builds a minimal valid PAK with one synthetic
// entry so pak.Open succeeds and IsEmpty returns false. Returns the
// raw bytes plus the (name, payload) of the single entry.
func makeSyntheticPak(name, payload string) []byte {
	const (
		headerSize = 12
		entrySize  = 64
		nameField  = 56
	)
	hdr := make([]byte, headerSize)
	copy(hdr[0:4], "PACK")
	payloadOff := int32(headerSize)
	payloadLen := int32(len(payload))
	dirOff := int32(headerSize + int(payloadLen))
	dirLen := int32(entrySize)
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(dirOff))
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(dirLen))

	entry := make([]byte, entrySize)
	copy(entry[:nameField], name)
	binary.LittleEndian.PutUint32(entry[56:60], uint32(payloadOff))
	binary.LittleEndian.PutUint32(entry[60:64], uint32(payloadLen))

	out := make([]byte, 0, headerSize+len(payload)+entrySize)
	out = append(out, hdr...)
	out = append(out, []byte(payload)...)
	out = append(out, entry...)
	return out
}

func TestBytes_PlaceholderIsTwelveBytes(t *testing.T) {
	got := Bytes()
	if len(got) != emptyPakSize {
		t.Fatalf("Bytes() len: got %d, want %d", len(got), emptyPakSize)
	}
	if string(got[0:4]) != "PACK" {
		t.Errorf("Bytes() magic: got %q, want %q", got[0:4], "PACK")
	}
	// Mutating the returned copy must not affect subsequent calls.
	got[0] = 0
	again := Bytes()
	if string(again[0:4]) != "PACK" {
		t.Errorf("Bytes() returned shared buffer; mutation leaked")
	}
}

func TestIsEmpty_Placeholder(t *testing.T) {
	if !IsEmpty() {
		t.Errorf("IsEmpty() with placeholder: got false, want true")
	}
}

func TestIsEmpty_NonPlaceholder(t *testing.T) {
	withEmbedded(t, makeSyntheticPak("foo.txt", "hello"), func() {
		if IsEmpty() {
			t.Errorf("IsEmpty() with synthetic pak: got true, want false")
		}
	})
}

func TestOpenAsFS_PlaceholderReturnsErrEmbedPakEmpty(t *testing.T) {
	f, err := OpenAsFS()
	if !errors.Is(err, ErrEmbedPakEmpty) {
		t.Errorf("OpenAsFS() err: got %v, want ErrEmbedPakEmpty", err)
	}
	if f != nil {
		t.Errorf("OpenAsFS() f: got %v, want nil", f)
	}
}

func TestOpenAsFS_NonEmptyReadsEntry(t *testing.T) {
	want := "hello"
	withEmbedded(t, makeSyntheticPak("foo.txt", want), func() {
		f, err := OpenAsFS()
		if err != nil {
			t.Fatalf("OpenAsFS() err: %v", err)
		}
		if f == nil {
			t.Fatalf("OpenAsFS() f: nil")
		}
		file, err := f.Open("foo.txt")
		if err != nil {
			t.Fatalf("f.Open(\"foo.txt\"): %v", err)
		}
		defer func() { _ = file.Close() }()
		got, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("io.ReadAll: %v", err)
		}
		if !bytes.Equal(got, []byte(want)) {
			t.Errorf("payload: got %q, want %q", got, want)
		}
	})
}

func TestAddToVFS_PlaceholderReturnsErrEmbedPakEmpty(t *testing.T) {
	sp := vfs.New()
	err := AddToVFS(sp)
	if !errors.Is(err, ErrEmbedPakEmpty) {
		t.Errorf("AddToVFS() err: got %v, want ErrEmbedPakEmpty", err)
	}
	if sp.Len() != 0 {
		t.Errorf("AddToVFS() sp.Len(): got %d, want 0", sp.Len())
	}
}

func TestAddToVFS_NonEmptyAddsSource(t *testing.T) {
	withEmbedded(t, makeSyntheticPak("foo.txt", "hello"), func() {
		sp := vfs.New()
		if err := AddToVFS(sp); err != nil {
			t.Fatalf("AddToVFS() err: %v", err)
		}
		if sp.Len() != 1 {
			t.Fatalf("AddToVFS() sp.Len(): got %d, want 1", sp.Len())
		}
		file, err := sp.Open("foo.txt")
		if err != nil {
			t.Fatalf("sp.Open(\"foo.txt\"): %v", err)
		}
		_ = file.Close()
	})
}

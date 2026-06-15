// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qpath

import "testing"

func TestSkipPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"foo.mdl", "foo.mdl"},
		{"id1/progs/player.mdl", "player.mdl"},
		{"a/b/c/", ""}, // trailing slash -> empty basename
		{"/abs.txt", "abs.txt"},
	}
	for _, c := range cases {
		if got := SkipPath(c.in); got != c.want {
			t.Errorf("SkipPath(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestStripExtension(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"foo.mdl", "foo"},
		{"id1/progs/player.mdl", "id1/progs/player"},
		{"noext", "noext"},
		{"", ""},
		{"id1.weird/noext", "id1.weird/noext"}, // basename has no dot
		{"id1/a.b.c.mdl", "id1/a.b.c"},
	}
	for _, c := range cases {
		if got := StripExtension(c.in); got != c.want {
			t.Errorf("StripExtension(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestFileExtension(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"foo.mdl", "mdl"},
		{"id1/progs/player.mdl", "mdl"},
		{"foo", ""},
		{"id1.weird/noext", ""},
		{"a.tar.gz", "gz"},
		{"", ""},
	}
	for _, c := range cases {
		if got := FileExtension(c.in); got != c.want {
			t.Errorf("FileExtension(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestFileBase(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"id1/progs/player.mdl", "player"},
		{"foo.mdl", "foo"},
		{"id1/x.mdl", "?model?"}, // basename "x" is 1 char < 2 -> sentinel
		{"x.mdl", "?model?"},
		{"id1/", "?model?"},      // empty basename
		{"", "?model?"},
		{"noext", "noext"},
		{"longname", "longname"},
	}
	for _, c := range cases {
		if got := FileBase(c.in); got != c.want {
			t.Errorf("FileBase(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestDefaultExtension(t *testing.T) {
	cases := []struct {
		path, ext, want string
	}{
		{"foo", ".mdl", "foo.mdl"},
		{"foo.mdl", ".bsp", "foo.mdl"}, // already has dot in basename
		{"id1.x/foo", ".bsp", "id1.x/foo.bsp"}, // dot in dir, NOT basename
		{"", ".txt", ".txt"},
	}
	for _, c := range cases {
		if got := DefaultExtension(c.path, c.ext); got != c.want {
			t.Errorf("DefaultExtension(%q,%q): got %q want %q", c.path, c.ext, got, c.want)
		}
	}
}

func TestCheckSuffix(t *testing.T) {
	if !CheckSuffix("id1/pak0.pak", ".pak") {
		t.Error("expected suffix match")
	}
	if !CheckSuffix("foo", "foo") {
		t.Error("self-match should hold")
	}
	if !CheckSuffix("foo", "") {
		t.Error("empty suffix always matches")
	}
	if CheckSuffix("PAK0.PAK", ".pak") {
		t.Error("case-sensitive should not match")
	}
	if CheckSuffix("short", "longer") {
		t.Error("suffix longer than path should not match")
	}
	if CheckSuffix("", ".pak") {
		t.Error("empty path should not match nonzero suffix")
	}
}

// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package entparse

import (
	"errors"
	"testing"
)

func TestParseEntities_Empty(t *testing.T) {
	got, err := ParseEntities(nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestParseEntities_WhitespaceOnly(t *testing.T) {
	got, err := ParseEntities([]byte("   \n\t  \r\n"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestParseEntities_SingleEntitySingleField(t *testing.T) {
	blob := []byte(`
{
"classname" "worldspawn"
}
`)
	got, err := ParseEntities(blob)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entities, want 1", len(got))
	}
	if got[0]["classname"] != "worldspawn" {
		t.Fatalf("classname = %q, want worldspawn", got[0]["classname"])
	}
	if len(got[0]) != 1 {
		t.Fatalf("got %d fields, want 1: %v", len(got[0]), got[0])
	}
}

func TestParseEntities_SingleEntityMultiField(t *testing.T) {
	blob := []byte(`{
"classname" "info_player_start"
"origin" "-32 -96 40"
"angle" "270"
}`)
	got, err := ParseEntities(blob)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entities, want 1", len(got))
	}
	want := map[string]string{
		"classname": "info_player_start",
		"origin":    "-32 -96 40",
		"angle":     "270",
	}
	for k, v := range want {
		if got[0][k] != v {
			t.Errorf("field %q = %q, want %q", k, got[0][k], v)
		}
	}
	if len(got[0]) != len(want) {
		t.Errorf("field count = %d, want %d (%v)", len(got[0]), len(want), got[0])
	}
}

func TestParseEntities_MultipleEntities(t *testing.T) {
	blob := []byte(`
{
"classname" "worldspawn"
"wad" "gfx/base.wad"
}
{
"classname" "info_player_start"
"origin" "0 0 24"
}
{
"classname" "light"
"origin" "100 200 300"
"light" "200"
}
`)
	got, err := ParseEntities(blob)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entities, want 3", len(got))
	}
	if got[0]["classname"] != "worldspawn" || got[0]["wad"] != "gfx/base.wad" {
		t.Errorf("ent0 = %v", got[0])
	}
	if got[1]["classname"] != "info_player_start" || got[1]["origin"] != "0 0 24" {
		t.Errorf("ent1 = %v", got[1])
	}
	if got[2]["light"] != "200" {
		t.Errorf("ent2 = %v", got[2])
	}
}

func TestParseEntities_QuotedValueWithWhitespace(t *testing.T) {
	blob := []byte(`{
"message" "Welcome to the dungeon, traveller."
}`)
	got, err := ParseEntities(blob)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got[0]["message"] != "Welcome to the dungeon, traveller." {
		t.Errorf("message = %q", got[0]["message"])
	}
}

func TestParseEntities_CommentsStripped(t *testing.T) {
	blob := []byte(`
// outer comment ignored
{
// inner comment ignored
"classname" "worldspawn" // trailing comment too
"sounds" "1"
}
// trailing comment after final entity
`)
	got, err := ParseEntities(blob)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0]["classname"] != "worldspawn" || got[0]["sounds"] != "1" {
		t.Fatalf("got %v", got)
	}
}

func TestParseEntities_TrailingJunkAfterFinalBraceIsKept(t *testing.T) {
	// tyrquake: trailing whitespace after the last '}' is ignored
	// because the outer COM_Parse call returns NULL on EOF. The Go
	// port matches by checking for the empty token on the outer
	// loop's first call.
	blob := []byte(`{
"classname" "worldspawn"
}


`)
	got, err := ParseEntities(blob)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entities, want 1", len(got))
	}
}

func TestParseEntities_EmptyEntity(t *testing.T) {
	// '{' immediately followed by '}' produces a zero-field entity.
	// Upstream stores nothing into the edict and ED_ParseEdict flips
	// the free flag; the parser-only port returns an empty map.
	blob := []byte(`{}`)
	got, err := ParseEntities(blob)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("got %v, want one zero-field entity", got)
	}
}

func TestParseEntities_EmptyKeyAndValuePreserved(t *testing.T) {
	// Upstream accepts ("","value") and ("key","") and just stores
	// the empty side. The port keeps the same behaviour because the
	// tokeniser yields "" for "" quoted strings.
	blob := []byte(`{
"" "blank-key value"
"blank-val" ""
}`)
	got, err := ParseEntities(blob)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got[0][""] != "blank-key value" {
		t.Errorf("empty-key entry: got %q", got[0][""])
	}
	if got[0]["blank-val"] != "" {
		t.Errorf("blank-val: got %q", got[0]["blank-val"])
	}
	if len(got[0]) != 2 {
		t.Errorf("field count = %d, want 2: %v", len(got[0]), got[0])
	}
}

func TestParseEntities_UnclosedBrace(t *testing.T) {
	blob := []byte(`{
"classname" "worldspawn"
`)
	got, err := ParseEntities(blob)
	if !errors.Is(err, ErrUnclosedBrace) {
		t.Fatalf("err = %v, want ErrUnclosedBrace", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestParseEntities_UnclosedBraceMidPair(t *testing.T) {
	// EOF after a key, before its value -- value-side parse hits EOF.
	blob := []byte(`{
"classname"`)
	_, err := ParseEntities(blob)
	if !errors.Is(err, ErrUnclosedBrace) {
		t.Fatalf("err = %v, want ErrUnclosedBrace", err)
	}
}

func TestParseEntities_UnmatchedClose(t *testing.T) {
	blob := []byte(`}`)
	got, err := ParseEntities(blob)
	if !errors.Is(err, ErrUnmatchedClose) {
		t.Fatalf("err = %v, want ErrUnmatchedClose", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestParseEntities_TopLevelJunk(t *testing.T) {
	// A non-brace top-level token (data outside any entity block)
	// collapses to ErrUnmatchedClose -- structurally the same kind
	// of "stray data at top level" failure.
	blob := []byte(`garbage { "classname" "worldspawn" }`)
	_, err := ParseEntities(blob)
	if !errors.Is(err, ErrUnmatchedClose) {
		t.Fatalf("err = %v, want ErrUnmatchedClose", err)
	}
}

func TestParseEntities_OrphanField(t *testing.T) {
	// Odd token count inside the block: key parses, but value-side
	// COM_Parse returns '}'. Upstream SV_Errors "closing brace
	// without data".
	blob := []byte(`{
"classname" "worldspawn"
"orphan"
}`)
	_, err := ParseEntities(blob)
	if !errors.Is(err, ErrOrphanField) {
		t.Fatalf("err = %v, want ErrOrphanField", err)
	}
}

func TestParseEntities_MultiEntityWithSecondUnclosed(t *testing.T) {
	// First entity parses fine, second '{' opens but never closes.
	// The whole call fails -- we return nil, err (no partial result),
	// matching the spec.
	blob := []byte(`{
"classname" "worldspawn"
}
{
"classname" "info_player_start"
`)
	got, err := ParseEntities(blob)
	if !errors.Is(err, ErrUnclosedBrace) {
		t.Fatalf("err = %v, want ErrUnclosedBrace", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

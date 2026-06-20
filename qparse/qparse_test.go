// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qparse

import "testing"

func TestToken_Empty(t *testing.T) {
	tok, rest := Token("")
	if tok != "" || rest != "" {
		t.Errorf("empty: got (%q, %q) want (\"\", \"\")", tok, rest)
	}
}

func TestToken_WhitespaceOnly(t *testing.T) {
	tok, rest := Token("   \t\n  ")
	if tok != "" || rest != "" {
		t.Errorf("whitespace: got (%q, %q) want (\"\", \"\")", tok, rest)
	}
}

func TestToken_SimpleWord(t *testing.T) {
	tok, rest := Token("foo bar baz")
	if tok != "foo" || rest != " bar baz" {
		t.Errorf("first: got (%q, %q) want (foo, ' bar baz')", tok, rest)
	}
	tok, rest = Token(rest)
	if tok != "bar" || rest != " baz" {
		t.Errorf("second: got (%q, %q)", tok, rest)
	}
	tok, rest = Token(rest)
	if tok != "baz" || rest != "" {
		t.Errorf("third: got (%q, %q)", tok, rest)
	}
	tok, rest = Token(rest)
	if tok != "" || rest != "" {
		t.Errorf("eof: got (%q, %q)", tok, rest)
	}
}

func TestToken_QuotedString(t *testing.T) {
	tok, rest := Token(`"hello world" next`)
	if tok != "hello world" || rest != " next" {
		t.Errorf("got (%q, %q) want (hello world, ' next')", tok, rest)
	}
}

func TestToken_UnterminatedQuoted(t *testing.T) {
	tok, rest := Token(`"hello`)
	if tok != "hello" || rest != "" {
		t.Errorf("got (%q, %q) want (hello, \"\")", tok, rest)
	}
}

func TestToken_EmptyQuotedString(t *testing.T) {
	tok, rest := Token(`"" after`)
	if tok != "" || rest != " after" {
		t.Errorf("got (%q, %q) want (\"\", ' after')", tok, rest)
	}
}

func TestToken_LineCommentSkipped(t *testing.T) {
	tok, rest := Token("// this is a comment\nfoo")
	if tok != "foo" || rest != "" {
		t.Errorf("got (%q, %q) want (foo, \"\")", tok, rest)
	}
}

func TestToken_LineCommentAtEOF(t *testing.T) {
	tok, rest := Token("// runs off the end no newline")
	if tok != "" || rest != "" {
		t.Errorf("got (%q, %q) want (\"\", \"\")", tok, rest)
	}
}

func TestToken_BlockComment(t *testing.T) {
	tok, rest := Token("/* skipped */ foo")
	if tok != "foo" || rest != "" {
		t.Errorf("got (%q, %q) want (foo, \"\")", tok, rest)
	}
}

func TestToken_BlockCommentUnterminated(t *testing.T) {
	tok, rest := Token("/* never ends")
	if tok != "" || rest != "" {
		t.Errorf("got (%q, %q) want (\"\", \"\")", tok, rest)
	}
}

func TestToken_CRandLF(t *testing.T) {
	tok, rest := Token("\r\nfoo\r\nbar")
	if tok != "foo" || rest != "\r\nbar" {
		t.Errorf("got (%q, %q)", tok, rest)
	}
}

func TestToken_SingleCharNotSplit(t *testing.T) {
	tok, rest := Token("{foo}")
	// In non-split mode, '{' is treated as a regular word byte.
	if tok != "{foo}" || rest != "" {
		t.Errorf("got (%q, %q) want ({foo}, \"\")", tok, rest)
	}
}

func TestTokenSplitSingleChars_Braces(t *testing.T) {
	tok, rest := TokenSplitSingleChars("{ foo }")
	if tok != "{" {
		t.Errorf("1st: got (%q, %q) want ({, ...)", tok, rest)
	}
	tok, rest = TokenSplitSingleChars(rest)
	if tok != "foo" {
		t.Errorf("2nd: got (%q, %q) want (foo, ...)", tok, rest)
	}
	tok, rest = TokenSplitSingleChars(rest)
	if tok != "}" {
		t.Errorf("3rd: got (%q, %q) want (}, ...)", tok, rest)
	}
	tok, rest = TokenSplitSingleChars(rest)
	if tok != "" || rest != "" {
		t.Errorf("4th (eof): got (%q, %q)", tok, rest)
	}
}

func TestTokenSplitSingleChars_NoSpaceAroundBrace(t *testing.T) {
	tok, rest := TokenSplitSingleChars("foo{bar")
	if tok != "foo" || rest != "{bar" {
		t.Errorf("1st: got (%q, %q) want (foo, '{bar')", tok, rest)
	}
	tok, rest = TokenSplitSingleChars(rest)
	if tok != "{" || rest != "bar" {
		t.Errorf("2nd: got (%q, %q) want ({, bar)", tok, rest)
	}
}

func TestTokenSplitSingleChars_AllSingles(t *testing.T) {
	for _, c := range []byte("{})(':") {
		s := string([]byte{c})
		tok, rest := TokenSplitSingleChars(s)
		if tok != s || rest != "" {
			t.Errorf("single %q: got (%q, %q) want (%q, \"\")", c, tok, rest, s)
		}
	}
}

func TestIsSingleChar(t *testing.T) {
	// All members of the set.
	for _, c := range []byte("{})(':") {
		if !isSingleChar(c) {
			t.Errorf("isSingleChar(%q) = false, want true", c)
		}
	}
	// Non-members.
	for _, c := range []byte("a0 \t,;") {
		if isSingleChar(c) {
			t.Errorf("isSingleChar(%q) = true, want false", c)
		}
	}
	// Zero byte (the sentinel).
	if isSingleChar(0) {
		t.Errorf("isSingleChar(0) = true, want false")
	}
}

// Comment-then-word-then-EOF exercises the goto skipwhite loop body.
func TestToken_CommentThenWord(t *testing.T) {
	tok, rest := Token("// c1\n// c2\nfoo bar")
	if tok != "foo" || rest != " bar" {
		t.Errorf("got (%q, %q) want (foo, ' bar')", tok, rest)
	}
}

// Block comment immediately followed by another covers the post-skip
// re-entry path.
func TestToken_TwoBlockComments(t *testing.T) {
	tok, rest := Token("/* a *//* b */foo")
	if tok != "foo" || rest != "" {
		t.Errorf("got (%q, %q) want (foo, \"\")", tok, rest)
	}
}

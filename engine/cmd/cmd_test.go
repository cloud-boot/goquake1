// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// --- Tokenize ----------------------------------------------------------------

func TestTokenize_Empty(t *testing.T) {
	if got := Tokenize(""); got != nil {
		t.Errorf("Tokenize empty: got %v want nil", got)
	}
	if got := Tokenize("   \t  "); got != nil {
		t.Errorf("Tokenize whitespace: got %v want nil", got)
	}
}

func TestTokenize_Simple(t *testing.T) {
	got := Tokenize("alias quick load")
	want := []string{"alias", "quick", "load"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestTokenize_QuotedString(t *testing.T) {
	got := Tokenize(`echo "hello world" again`)
	want := []string{"echo", "hello world", "again"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestTokenize_QuotedEmpty(t *testing.T) {
	got := Tokenize(`set foo ""`)
	want := []string{"set", "foo", ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestTokenize_UnterminatedQuote(t *testing.T) {
	// COM_Parse runs to EOF and emits whatever it accumulated.
	got := Tokenize(`echo "hello world`)
	want := []string{"echo", "hello world"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestTokenize_CommentOnly(t *testing.T) {
	if got := Tokenize("// just a comment"); got != nil {
		t.Errorf("got %v want nil", got)
	}
}

func TestTokenize_TrailingComment(t *testing.T) {
	got := Tokenize("echo hi // trailing")
	want := []string{"echo", "hi"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestTokenize_LeadingWhitespace(t *testing.T) {
	got := Tokenize("   \t  echo  hi  ")
	want := []string{"echo", "hi"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

// --- Registry ----------------------------------------------------------------

func TestRegistry_AddAndExecute(t *testing.T) {
	r := New()
	var seen []string
	r.Add("echo", func(args []string) { seen = append(seen, strings.Join(args, "|")) })
	if !r.Exists("echo") {
		t.Errorf("Exists(echo) false after Add")
	}
	if err := r.Execute("echo a b c"); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if len(seen) != 1 || seen[0] != "echo|a|b|c" {
		t.Errorf("Handler not invoked correctly: %v", seen)
	}
}

func TestRegistry_DuplicateAdd(t *testing.T) {
	r := New()
	first := false
	second := false
	r.Add("foo", func(args []string) { first = true })
	r.Add("foo", func(args []string) { second = true })
	_ = r.Execute("foo")
	if !first {
		t.Errorf("first handler should still fire (duplicate Add is a no-op)")
	}
	if second {
		t.Errorf("second handler must NOT fire -- Add must not overwrite")
	}
}

func TestRegistry_Remove(t *testing.T) {
	r := New()
	fired := false
	r.Add("foo", func(args []string) { fired = true })
	r.Remove("foo")
	if r.Exists("foo") {
		t.Errorf("Exists(foo) true after Remove")
	}
	// Remove of a missing name is a silent no-op.
	r.Remove("nonexistent")
	if err := r.Execute("foo"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if fired {
		t.Errorf("removed handler must not fire")
	}
}

func TestRegistry_MissingHandler(t *testing.T) {
	r := New()
	// Unknown command silently drops (host layer logs separately).
	if err := r.Execute("nosuchcmd a b"); err != nil {
		t.Errorf("missing-handler dispatch should not error: %v", err)
	}
}

func TestRegistry_EmptyLine(t *testing.T) {
	r := New()
	if err := r.Execute(""); err != nil {
		t.Errorf("empty line: %v", err)
	}
	if err := r.Execute("   \t  "); err != nil {
		t.Errorf("blank line: %v", err)
	}
	if err := r.Execute("// only a comment"); err != nil {
		t.Errorf("comment-only line: %v", err)
	}
}

// --- Alias -------------------------------------------------------------------

func TestRegistry_AliasInline(t *testing.T) {
	r := New()
	var seen []string
	r.Add("echo", func(args []string) { seen = append(seen, strings.Join(args[1:], " ")) })
	if !r.AddAlias("greet", "echo hello; echo world") {
		t.Errorf("AddAlias rejected a valid name")
	}
	if !r.AliasExists("greet") {
		t.Errorf("AliasExists false after AddAlias")
	}
	if err := r.Execute("greet"); err != nil {
		t.Fatalf("greet: %v", err)
	}
	want := []string{"hello", "world"}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("alias expansion: got %v want %v", seen, want)
	}
}

func TestRegistry_AliasOverwrite(t *testing.T) {
	r := New()
	count := 0
	r.Add("inc", func(args []string) { count++ })
	r.AddAlias("a", "inc")
	r.AddAlias("a", "inc; inc")
	if err := r.Execute("a"); err != nil {
		t.Fatalf("a: %v", err)
	}
	if count != 2 {
		t.Errorf("alias overwrite: count=%d want 2", count)
	}
}

func TestRegistry_AliasNameTooLong(t *testing.T) {
	r := New()
	name := strings.Repeat("x", MaxAliasNameLen)
	if r.AddAlias(name, "body") {
		t.Errorf("AddAlias should reject names of length >= MaxAliasNameLen")
	}
	if r.AliasExists(name) {
		t.Errorf("alias must not exist after rejection")
	}
}

func TestRegistry_AliasRecursionGuard(t *testing.T) {
	r := New()
	r.AddAlias("loop", "loop")
	err := r.Execute("loop")
	if !errors.Is(err, ErrAliasRecursion) {
		t.Errorf("self-referential alias: got err %v want ErrAliasRecursion", err)
	}
}

// --- Buffer ------------------------------------------------------------------

func TestBuffer_AddExecute(t *testing.T) {
	r := New()
	var seen []string
	r.Add("echo", func(args []string) { seen = append(seen, strings.Join(args[1:], " ")) })

	b := NewBuffer()
	b.Add("echo first\n")
	b.Add("echo second\n")
	if b.Len() == 0 {
		t.Errorf("Len() == 0 after Add")
	}
	if err := b.Execute(r); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{"first", "second"}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("got %v want %v", seen, want)
	}
	if b.Len() != 0 {
		t.Errorf("buffer not drained: %d bytes left", b.Len())
	}
}

func TestBuffer_SemicolonSplit(t *testing.T) {
	r := New()
	var seen []string
	r.Add("echo", func(args []string) { seen = append(seen, strings.Join(args[1:], " ")) })

	b := NewBuffer()
	b.Add(`echo a; echo b; echo "c ; d"`)
	if err := b.Execute(r); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{"a", "b", "c ; d"}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("got %v want %v", seen, want)
	}
}

func TestBuffer_Insert(t *testing.T) {
	r := New()
	var seen []string
	r.Add("echo", func(args []string) { seen = append(seen, args[1]) })

	b := NewBuffer()
	b.Add("echo last\n")
	b.Insert("echo first")
	if err := b.Execute(r); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{"first", "last"}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("got %v want %v", seen, want)
	}
}

func TestBuffer_InsertIntoEmpty(t *testing.T) {
	r := New()
	var seen []string
	r.Add("echo", func(args []string) { seen = append(seen, args[1]) })
	b := NewBuffer()
	b.Insert("echo solo")
	if err := b.Execute(r); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(seen) != 1 || seen[0] != "solo" {
		t.Errorf("got %v want [solo]", seen)
	}
}

func TestBuffer_Wait(t *testing.T) {
	r := New()
	var seen []string
	r.Add("echo", func(args []string) { seen = append(seen, args[1]) })

	b := NewBuffer()
	r.Add("wait", b.WaitHandler)
	b.Add("echo a; wait; echo b")

	if err := b.Execute(r); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if !reflect.DeepEqual(seen, []string{"a"}) {
		t.Errorf("after first Execute: got %v want [a]", seen)
	}
	if b.Len() == 0 {
		t.Errorf("buffer should still hold 'echo b' after wait")
	}
	if err := b.Execute(r); err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if !reflect.DeepEqual(seen, []string{"a", "b"}) {
		t.Errorf("after second Execute: got %v want [a, b]", seen)
	}
}

func TestBuffer_AliasInsertsAtHead(t *testing.T) {
	r := New()
	var seen []string
	r.Add("echo", func(args []string) { seen = append(seen, args[1]) })
	r.AddAlias("expand", "echo from_alias")

	b := NewBuffer()
	r.AttachBuffer(b)
	b.Add("expand; echo tail")
	if err := b.Execute(r); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// With a buffer attached, the alias body queues at the head, so it
	// runs before the rest of the line that triggered it.
	want := []string{"from_alias", "tail"}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("got %v want %v", seen, want)
	}
}

func TestBuffer_ExecutePropagatesError(t *testing.T) {
	r := New()
	r.AddAlias("loop", "loop")
	b := NewBuffer()
	b.Add("loop\n")
	err := b.Execute(r)
	if !errors.Is(err, ErrAliasRecursion) {
		t.Errorf("got err %v want ErrAliasRecursion", err)
	}
}

func TestBuffer_QuotedNewlineSplits(t *testing.T) {
	// '\n' inside a quoted string still splits in Cbuf_Execute -- the
	// upstream loop only quotes-protects ';', not '\n'. Confirm the Go
	// port preserves that asymmetry: the line break inside the quoted
	// arg DOES cut the buffer, leaving a stray fragment that dispatches
	// as its own (unknown) command on the second iteration.
	r := New()
	var echoes []string
	r.Add("echo", func(args []string) {
		echoes = append(echoes, strings.Join(args[1:], "|"))
	})
	var unknown []string
	r.Add("b\"", func(args []string) { unknown = append(unknown, args[0]) })
	b := NewBuffer()
	b.Add("echo \"a\nb\"")
	if err := b.Execute(r); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// First line: echo "a -> echo handler with unterminated-quote token "a".
	if !reflect.DeepEqual(echoes, []string{"a"}) {
		t.Errorf("echo lines: got %v want [a]", echoes)
	}
	// Second line: b" -> dispatches as the literal command "b\"".
	if !reflect.DeepEqual(unknown, []string{"b\""}) {
		t.Errorf("stray fragment: got %v want [b\"]", unknown)
	}
}

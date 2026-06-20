// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sizebuf

import (
	"bytes"
	"errors"
	"testing"
)

// --- constructor + accessors -------------------------------------------------

func TestNew_WrapsBacking(t *testing.T) {
	backing := make([]byte, 32)
	b := New(backing)
	if b.Len() != 0 {
		t.Errorf("Len: got %d want 0", b.Len())
	}
	if cap(b.data) != 32 {
		t.Errorf("cap: got %d want 32", cap(b.data))
	}
	if b.AllowOverflow {
		t.Errorf("AllowOverflow: default must be false")
	}
	if b.Overflowed() {
		t.Errorf("Overflowed: default must be false")
	}
	if got := b.Bytes(); len(got) != 0 {
		t.Errorf("Bytes: empty want len 0, got %d", len(got))
	}
}

func TestNew_PreservesCapWhenInputHasLen(t *testing.T) {
	// Caller might hand over a slice with a non-zero length already
	// (e.g. backing := buf[:16] where cap is 32). The constructor must
	// reset length but preserve the full capacity.
	raw := make([]byte, 32)
	b := New(raw[:8])
	if cap(b.data) != 32 {
		t.Errorf("cap: got %d want 32", cap(b.data))
	}
	if b.Len() != 0 {
		t.Errorf("Len: got %d want 0", b.Len())
	}
}

// --- Clear -------------------------------------------------------------------

func TestClear_ResetsCursorAndFlag(t *testing.T) {
	b := New(make([]byte, 8))
	b.AllowOverflow = true
	if _, err := b.GetSpace(6); err != nil {
		t.Fatalf("setup GetSpace(6): %v", err)
	}
	if _, err := b.GetSpace(4); err != nil { // 6+4>8 -> truncate, flag set
		t.Fatalf("setup GetSpace(4): %v", err)
	}
	if !b.Overflowed() {
		t.Fatal("setup: expected Overflowed=true")
	}
	b.Clear()
	if b.Len() != 0 {
		t.Errorf("Len: got %d want 0", b.Len())
	}
	if b.Overflowed() {
		t.Errorf("Overflowed: got true want false after Clear")
	}
}

// --- GetSpace ----------------------------------------------------------------

func TestGetSpace_HappyPath(t *testing.T) {
	b := New(make([]byte, 16))
	dst, err := b.GetSpace(4)
	if err != nil {
		t.Fatalf("GetSpace: unexpected error %v", err)
	}
	if len(dst) != 4 {
		t.Errorf("len(dst): got %d want 4", len(dst))
	}
	if b.Len() != 4 {
		t.Errorf("Len: got %d want 4", b.Len())
	}
	// Returned slice must alias the backing store.
	dst[0] = 0xAA
	if b.Bytes()[0] != 0xAA {
		t.Errorf("aliasing: got %#x want 0xAA", b.Bytes()[0])
	}
}

func TestGetSpace_RequestTooLargeOverridesOverflowOnlyWithAllowOverflow(t *testing.T) {
	// Matches tyrquake's check order: without AllowOverflow, a
	// length > maxsize request still surfaces as ErrSizeBufOverflow
	// (the upstream Sys_Error("overflow without allowoverflow") fires
	// first). With AllowOverflow set, the request-too-large guard is
	// reachable.
	b := New(make([]byte, 8))
	_, err := b.GetSpace(9)
	if !errors.Is(err, ErrSizeBufOverflow) {
		t.Errorf("disallowed err: got %v want ErrSizeBufOverflow", err)
	}
	if b.Len() != 0 {
		t.Errorf("Len: buffer must be untouched, got %d want 0", b.Len())
	}

	b2 := New(make([]byte, 8))
	b2.AllowOverflow = true
	_, err = b2.GetSpace(9)
	if !errors.Is(err, ErrSizeBufRequestTooLarge) {
		t.Errorf("allowed err: got %v want ErrSizeBufRequestTooLarge", err)
	}
	if b2.Len() != 0 {
		t.Errorf("Len: buffer must be untouched, got %d want 0", b2.Len())
	}
}

func TestGetSpace_OverflowDisallowed(t *testing.T) {
	b := New(make([]byte, 8))
	if _, err := b.GetSpace(6); err != nil {
		t.Fatalf("setup GetSpace(6): %v", err)
	}
	_, err := b.GetSpace(4) // cursize=6 + 4 > 8
	if !errors.Is(err, ErrSizeBufOverflow) {
		t.Errorf("err: got %v want ErrSizeBufOverflow", err)
	}
	if b.Len() != 6 {
		t.Errorf("Len: got %d want 6 (untouched on disallowed overflow)", b.Len())
	}
	if b.Overflowed() {
		t.Errorf("Overflowed: must remain false when overflow is disallowed")
	}
}

func TestGetSpace_OverflowAllowed(t *testing.T) {
	b := New(make([]byte, 8))
	b.AllowOverflow = true
	if _, err := b.GetSpace(6); err != nil {
		t.Fatalf("setup GetSpace(6): %v", err)
	}
	dst, err := b.GetSpace(4) // forces truncation then re-allocation from 0
	if err != nil {
		t.Fatalf("GetSpace: unexpected error %v", err)
	}
	if len(dst) != 4 {
		t.Errorf("len(dst): got %d want 4", len(dst))
	}
	if b.Len() != 4 {
		t.Errorf("Len: got %d want 4 (post-truncation)", b.Len())
	}
	if !b.Overflowed() {
		t.Errorf("Overflowed: want true after AllowOverflow truncation")
	}
}

// --- Write -------------------------------------------------------------------

func TestWrite_AppendsBytes(t *testing.T) {
	b := New(make([]byte, 16))
	if err := b.Write([]byte{1, 2, 3}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := b.Write([]byte{4, 5}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := []byte{1, 2, 3, 4, 5}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("Bytes: got %v want %v", b.Bytes(), want)
	}
}

func TestWrite_PropagatesOverflowError(t *testing.T) {
	b := New(make([]byte, 4))
	err := b.Write([]byte{1, 2, 3, 4, 5})
	if !errors.Is(err, ErrSizeBufOverflow) {
		t.Errorf("err: got %v want ErrSizeBufOverflow", err)
	}
}

func TestWrite_PropagatesRequestTooLarge(t *testing.T) {
	b := New(make([]byte, 4))
	b.AllowOverflow = true
	err := b.Write([]byte{1, 2, 3, 4, 5})
	if !errors.Is(err, ErrSizeBufRequestTooLarge) {
		t.Errorf("err: got %v want ErrSizeBufRequestTooLarge", err)
	}
}

// --- Print -------------------------------------------------------------------

func TestPrint_EmptyBufferAppendsStringAndNul(t *testing.T) {
	b := New(make([]byte, 16))
	if err := b.Print("hi"); err != nil {
		t.Fatalf("Print: %v", err)
	}
	want := []byte{'h', 'i', 0}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("Bytes: got %v want %v", b.Bytes(), want)
	}
}

func TestPrint_NonNulTailDoesNotOverwrite(t *testing.T) {
	b := New(make([]byte, 16))
	// Prime with raw bytes (no trailing NUL).
	if err := b.Write([]byte{'a', 'b', 'c'}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := b.Print("de"); err != nil {
		t.Fatalf("Print: %v", err)
	}
	want := []byte{'a', 'b', 'c', 'd', 'e', 0}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("Bytes: got %v want %v", b.Bytes(), want)
	}
}

func TestPrint_ConsecutivePrintsOverwriteTrailingNul(t *testing.T) {
	// tyrquake quirk: a second Print after a Print reuses the prior
	// NUL slot, so two "ab" prints yield "abab\0" (5 bytes), not
	// "ab\0ab\0" (6 bytes).
	b := New(make([]byte, 16))
	if err := b.Print("ab"); err != nil {
		t.Fatalf("Print1: %v", err)
	}
	if err := b.Print("cd"); err != nil {
		t.Fatalf("Print2: %v", err)
	}
	want := []byte{'a', 'b', 'c', 'd', 0}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("Bytes: got %v want %v", b.Bytes(), want)
	}
	if b.Len() != 5 {
		t.Errorf("Len: got %d want 5", b.Len())
	}
}

func TestPrint_EmptyStringAfterPrintIsNoOp(t *testing.T) {
	b := New(make([]byte, 16))
	if err := b.Print("hi"); err != nil {
		t.Fatalf("Print: %v", err)
	}
	before := append([]byte(nil), b.Bytes()...)
	if err := b.Print(""); err != nil {
		t.Fatalf("Print(\"\"): %v", err)
	}
	if !bytes.Equal(b.Bytes(), before) {
		t.Errorf("Bytes: got %v want %v (no-op)", b.Bytes(), before)
	}
}

func TestPrint_EmptyStringOnFreshBufferWritesNul(t *testing.T) {
	b := New(make([]byte, 4))
	if err := b.Print(""); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if !bytes.Equal(b.Bytes(), []byte{0}) {
		t.Errorf("Bytes: got %v want [0]", b.Bytes())
	}
}

func TestPrint_PropagatesOverflowOnFreshBranchOversize(t *testing.T) {
	// Fresh branch, AllowOverflow=false: a too-large Print surfaces
	// as ErrSizeBufOverflow (upstream check order).
	b := New(make([]byte, 4))
	err := b.Print("toolong") // 7 + NUL > 4
	if !errors.Is(err, ErrSizeBufOverflow) {
		t.Errorf("err: got %v want ErrSizeBufOverflow", err)
	}
}

func TestPrint_PropagatesRequestTooLargeOnFreshBranch(t *testing.T) {
	// Fresh branch, AllowOverflow=true: a too-large Print is rejected
	// with ErrSizeBufRequestTooLarge.
	b := New(make([]byte, 4))
	b.AllowOverflow = true
	err := b.Print("toolong")
	if !errors.Is(err, ErrSizeBufRequestTooLarge) {
		t.Errorf("err: got %v want ErrSizeBufRequestTooLarge", err)
	}
}

func TestPrint_PropagatesOverflowOnFreshBranch(t *testing.T) {
	b := New(make([]byte, 8))
	// Leave a non-NUL tail so the fresh-buffer branch is taken.
	if err := b.Write([]byte{'x', 'y', 'z', 'w'}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// "hello" + NUL == 6 bytes, cursize=4, 4+6=10 > 8.
	err := b.Print("hello")
	if !errors.Is(err, ErrSizeBufOverflow) {
		t.Errorf("err: got %v want ErrSizeBufOverflow", err)
	}
}

func TestPrint_PropagatesRequestTooLargeOnReuseBranchRegardlessOfAllowOverflow(t *testing.T) {
	// Reuse branch: when n+1 > cap the buffer can never satisfy the
	// reservation (even an AllowOverflow truncation would lose the
	// trailing NUL slot), so Print surfaces ErrSizeBufRequestTooLarge
	// upfront, independent of AllowOverflow.
	for _, allow := range []bool{false, true} {
		b := New(make([]byte, 4))
		b.AllowOverflow = allow
		if err := b.Print("a"); err != nil { // buffer = "a\0", cursize=2
			t.Fatalf("Print(%v): %v", allow, err)
		}
		err := b.Print("toolong")
		if !errors.Is(err, ErrSizeBufRequestTooLarge) {
			t.Errorf("AllowOverflow=%v err: got %v want ErrSizeBufRequestTooLarge", allow, err)
		}
	}
}

func TestPrint_PropagatesOverflowOnReuseBranchDisallowed(t *testing.T) {
	b := New(make([]byte, 6))
	if err := b.Print("ab"); err != nil { // buffer = "ab\0", cursize=3
		t.Fatalf("Print: %v", err)
	}
	// "cdef" is 4 bytes; net request is 4 (reuses prior NUL), but
	// cursize=3 + 4 = 7 > 6, and AllowOverflow is false.
	err := b.Print("cdef")
	if !errors.Is(err, ErrSizeBufOverflow) {
		t.Errorf("err: got %v want ErrSizeBufOverflow", err)
	}
}

func TestPrint_OverflowReuseBranchAllowed(t *testing.T) {
	// Force the reuse-branch overflow fallback: cursize+n > cap but
	// n <= cap, with AllowOverflow=true. GetSpace(n) truncates,
	// flips overflowed, returns dst at offset 0; Print then appends
	// the trailing NUL.
	b := New(make([]byte, 6))
	b.AllowOverflow = true
	if err := b.Print("ab"); err != nil { // cursize=3, last byte 0
		t.Fatalf("Print: %v", err)
	}
	// "cdef" is 4 bytes -> request n=4; 3+4=7>6 -> truncates to 0.
	if err := b.Print("cdef"); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if !b.Overflowed() {
		t.Errorf("Overflowed: want true after truncating Print")
	}
	want := []byte{'c', 'd', 'e', 'f', 0}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("Bytes: got %v want %v", b.Bytes(), want)
	}
}

func TestPrint_OverflowReuseBranchAllowed_FailsOnTailNul(t *testing.T) {
	// Cap so tight that even the truncation fallback can't fit the
	// trailing NUL: n == cap, leaving no room for the extra byte.
	b := New(make([]byte, 4))
	b.AllowOverflow = true
	if err := b.Print("ab"); err != nil { // cursize=3, last byte 0
		t.Fatalf("Print: %v", err)
	}
	// "cdef" has n=4 == cap; reuse-branch enters, GetSpace(4)
	// truncates (cursize=0 -> 4), but the subsequent GetSpace(1)
	// for the NUL exceeds cap and yields ErrSizeBufRequestTooLarge.
	err := b.Print("cdef")
	if !errors.Is(err, ErrSizeBufRequestTooLarge) {
		t.Errorf("err: got %v want ErrSizeBufRequestTooLarge", err)
	}
}

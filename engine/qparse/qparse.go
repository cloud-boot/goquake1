// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qparse

// singleChars is the upstream set of characters that, under the
// split-single-chars mode, tokenise individually. tyrquake:
// common.c `static const char single_chars[] = "{})(':";`.
const singleChars = "{})(':"

// Token returns the next whitespace-or-quoted token in data and the
// remaining unconsumed tail. At EOF both return values are empty.
// tyrquake: COM_Parse (QW_HACK build, i.e. COM_Parse_ with
// split_single_chars = false).
func Token(data string) (token, rest string) {
	return parse(data, false)
}

// TokenSplitSingleChars behaves like [Token] but additionally treats
// each character in "{})(':" as a standalone token. The BSP entity-
// string parser uses this so braces tokenise on their own. tyrquake:
// COM_Parse (NQ_HACK build, i.e. COM_Parse_ with split_single_chars =
// true).
func TokenSplitSingleChars(data string) (token, rest string) {
	return parse(data, true)
}

// parse is the shared body of COM_Parse_. Indices into `data` stand in
// for the C `const char *data` cursor; advancing the pointer becomes
// `i++`, dereferencing past the end yields the sentinel byte 0 to keep
// the `while (*data)` loop shape intact.
func parse(data string, splitSingleChars bool) (token, rest string) {
	i := 0
	n := len(data)

	// at returns the byte at offset off, or 0 when off is past the end.
	// Mirrors C's NUL-terminator behaviour so the comment-skip and
	// quoted-string loops can read past the last real byte safely.
	at := func(off int) byte {
		if off >= n {
			return 0
		}
		return data[off]
	}

skipwhite:
	for {
		c := at(i)
		if c == 0 {
			return "", ""
		}
		if c > ' ' {
			break
		}
		i++
	}

	// // line comment: discard to the next newline, then re-enter the
	// whitespace skip.
	if at(i) == '/' && at(i+1) == '/' {
		for i < n && data[i] != '\n' {
			i++
		}
		goto skipwhite
	}

	// /* ... */ block comment: discard up to and including the closing
	// */, then re-enter the whitespace skip. Tracks the C version's
	// "if (*data) data += 2" guard so an unterminated block at EOF
	// leaves the cursor at the end rather than walking off it.
	if at(i) == '/' && at(i+1) == '*' {
		i += 2
		for i < n && !(data[i] == '*' && at(i+1) == '/') {
			i++
		}
		if i < n {
			i += 2
		}
		goto skipwhite
	}

	// Quoted string: token is the content between the quotes; closing
	// quote OR end-of-input terminates. On a real closing quote the
	// cursor steps past it; on EOF inside the string the cursor stays
	// at the end so the caller sees rest == "".
	if at(i) == '"' {
		i++
		start := i
		for {
			c := at(i)
			if c == 0 {
				return data[start:i], ""
			}
			if c == '"' {
				return data[start:i], data[i+1:]
			}
			i++
		}
	}

	// Single-character token: in split-single-chars mode any byte in
	// "{})(':" tokenises on its own, the cursor advances past it.
	if splitSingleChars && isSingleChar(at(i)) {
		return data[i : i+1], data[i+1:]
	}

	// Regular word: run until the next whitespace byte (c <= ' ', the
	// C `while (c > 32)` loop), or, under split-single-chars, until the
	// next single-char punctuator. The first byte is always consumed,
	// matching the C do/while.
	start := i
	for {
		i++
		c := at(i)
		if splitSingleChars && isSingleChar(c) {
			break
		}
		if c <= ' ' {
			break
		}
	}
	return data[start:i], data[i:]
}

// isSingleChar reports whether c belongs to the single_chars set.
// tyrquake: strchr(single_chars, c) != NULL.
func isSingleChar(c byte) bool {
	if c == 0 {
		return false
	}
	for j := 0; j < len(singleChars); j++ {
		if singleChars[j] == c {
			return true
		}
	}
	return false
}

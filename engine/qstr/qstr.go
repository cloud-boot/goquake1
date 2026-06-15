// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qstr

// StrBufLen mirrors tyrquake COM_STRBUF_LEN. Each slot returned by
// StrBuf is exactly this many bytes.
const StrBufLen = 2048

// numStrBufs mirrors the slot count baked into COM_GetStrBuf's
// `buffers[8][COM_STRBUF_LEN]` plus the `7 & ++index` mask.
const numStrBufs = 8

var (
	strBufStorage [numStrBufs][StrBufLen]byte
	strBufIndex   int
)

// StrBuf returns one of 8 rotating transient byte buffers, each
// StrBufLen long. The C version is used to back va()-style transient
// formatting where caller lifetimes are intentionally bounded by the
// ring size. tyrquake: COM_GetStrBuf.
func StrBuf() []byte {
	// `7 & ++index` in C: pre-increment so the first call returns
	// slot 1 (not slot 0). Preserve that quirk so any byte-exact
	// expectations downstream still hold.
	strBufIndex++
	return strBufStorage[(numStrBufs-1)&strBufIndex][:]
}

// Atoi parses str as a signed integer, supporting tyrquake's three
// forms: hex (`0x` / `0X` prefix), single-quoted-character literal
// (`'X'` returns the codepoint), and decimal. Parsing stops at the
// first non-matching byte and the accumulated value is returned;
// strings that start with garbage return 0. tyrquake: Q_atoi.
func Atoi(s string) int {
	i := 0
	sign := 1
	if i < len(s) && s[i] == '-' {
		sign = -1
		i++
	}

	val := 0

	// Hex path: `0x` or `0X` prefix, then [0-9a-fA-F]*.
	if i+1 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X') {
		i += 2
		for i < len(s) {
			c := s[i]
			switch {
			case c >= '0' && c <= '9':
				val = (val << 4) + int(c-'0')
			case c >= 'a' && c <= 'f':
				val = (val << 4) + int(c-'a') + 10
			case c >= 'A' && c <= 'F':
				val = (val << 4) + int(c-'A') + 10
			default:
				return val * sign
			}
			i++
		}
		return val * sign
	}

	// Quoted-character path: 'X -> ASCII value (sign applied).
	// tyrquake reads str[1] unconditionally; an unterminated `'`
	// with no following byte would be undefined behaviour in C, so
	// we bound-check and treat it as zero.
	if i < len(s) && s[i] == '\'' {
		if i+1 < len(s) {
			return sign * int(s[i+1])
		}
		return 0
	}

	// Decimal path.
	for i < len(s) {
		c := s[i]
		if c < '0' || c > '9' {
			return val * sign
		}
		val = val*10 + int(c-'0')
		i++
	}
	return val * sign
}

// Atof parses str as a float, supporting tyrquake's three forms in
// parallel with Atoi: hex (`0x`/`0X`), quoted-character (`'X'`), and
// decimal-with-optional-`.`. The decimal path does NOT honour
// exponent notation (`1e5`); tyrquake's C source breaks the loop on
// the `e`, returning the accumulated value. We preserve that.
// tyrquake: Q_atof.
func Atof(s string) float32 {
	i := 0
	sign := 1
	if i < len(s) && s[i] == '-' {
		sign = -1
		i++
	}

	var val float64

	// Hex path mirrors Atoi but in float64 to match the upstream
	// double-precision accumulator.
	if i+1 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X') {
		i += 2
		for i < len(s) {
			c := s[i]
			switch {
			case c >= '0' && c <= '9':
				val = val*16 + float64(c-'0')
			case c >= 'a' && c <= 'f':
				val = val*16 + float64(c-'a') + 10
			case c >= 'A' && c <= 'F':
				val = val*16 + float64(c-'A') + 10
			default:
				return float32(val * float64(sign))
			}
			i++
		}
		return float32(val * float64(sign))
	}

	// Quoted-character path: same caveat as Atoi for unterminated `'`.
	if i < len(s) && s[i] == '\'' {
		if i+1 < len(s) {
			return float32(sign) * float32(s[i+1])
		}
		return 0
	}

	// Decimal path with optional single '.'.
	decimal := -1
	total := 0
	for i < len(s) {
		c := s[i]
		if c == '.' {
			decimal = total
			i++
			continue
		}
		if c < '0' || c > '9' {
			break
		}
		val = val*10 + float64(c-'0')
		total++
		i++
	}

	if decimal == -1 {
		return float32(val * float64(sign))
	}
	for total > decimal {
		val /= 10
		total--
	}
	return float32(val * float64(sign))
}

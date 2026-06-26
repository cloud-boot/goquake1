// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package savegame

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// stringWriter is the lowest-common-denominator surface Encode +
// writeBlock target. io.Writer covers it; bufio.Writer adds the
// WriteString fast-path. The interface lets Encode operate on a raw
// io.Writer without a bufio wrapper (so per-write errors surface at
// the failing Fprintf rather than getting swallowed by the buffer).
type stringWriter interface {
	io.Writer
	WriteString(s string) (int, error)
}

// asStringWriter wraps an io.Writer into a stringWriter if it doesn't
// already implement the WriteString fast-path.
func asStringWriter(w io.Writer) stringWriter {
	if sw, ok := w.(stringWriter); ok {
		return sw
	}
	return &ioStringWriter{w: w}
}

type ioStringWriter struct{ w io.Writer }

func (s *ioStringWriter) Write(p []byte) (int, error)       { return s.w.Write(p) }
func (s *ioStringWriter) WriteString(t string) (int, error) { return s.w.Write([]byte(t)) }

// SpawnParmCount is the per-client carry-across-maps float array size
// the upstream coop chain serializes verbatim into every save header.
// Mirrors server.NumSpawnParms but kept local so this package has no
// upward dep on the server package.
//
// tyrquake: NUM_SPAWN_PARMS = 16 in NQ/server.h.
const SpawnParmCount = 16

// FormatVersion is the on-disk save version the upstream's
// Host_Savegame_f writes on the first line. The Go port matches the
// vanilla NetQuake value verbatim so a save produced here can be
// inspected against a tyrquake reference dump.
//
// tyrquake: SAVEGAME_VERSION = 5 in NQ/host_cmd.c.
const FormatVersion = 5

// EdictSnap is one per-edict {"key" "value"} block. The key/value
// pairs are stored verbatim in arrival order so a round-trip
// (Encode -> Load) preserves the textual shape byte-for-byte (the
// FieldKV slice is the canonical ordering; Encode emits in slice
// order, Load fills the slice in read order).
type EdictSnap struct {
	// Free mirrors the snapshotted edict's Free flag. A free slot is
	// emitted as an empty "{}" pair so the slot index stays aligned
	// with the live edict pool on restore.
	Free bool

	// FieldKV is the ordered list of (key, value) pairs the snapshot
	// pass dumped for this edict. Empty when Free == true.
	FieldKV []KV
}

// KV is one quoted-key / quoted-value pair inside a "{}" block.
// Keys are field names (matching FieldDefs[*].SName); values are
// type-rendered strings (a float reads back as e.g. "100.000000",
// a vector as "100 200 50", a string as the raw bytes between the
// quotes).
type KV struct {
	Key   string
	Value string
}

// Save is one savegame snapshot. The shape mirrors the upstream's
// .sav file line-by-line:
//
//	<comment>           -- one-line operator-visible label
//	<spawn_parms 0..15> -- 16 lines, one float each
//	<skill>             -- 1 line
//	<map_name>          -- 1 line
//	<sv.time>           -- 1 line
//	<globals>           -- ED_PrintGlobals block (Encode emits the raw
//	                       global key/value pairs the QC field-def table
//	                       marks with DEF_SAVEGLOBAL)
//	<per-edict>         -- repeated "{...}" blocks per non-free edict
//	                       plus empty "{}" placeholders for free slots
//
// Comment / MapName must be single-line UTF-8 (no embedded newlines);
// Encode does not validate, but Load rejects multi-line values via
// the line scanner.
type Save struct {
	// Comment is the operator-visible label rendered next to each
	// save slot in the load picker (typically "<map> kills/secrets").
	Comment string

	// SpawnParms carries the per-client persisted-stat float array.
	// tyrquake: Client.SpawnParms[NUM_SPAWN_PARMS] in NQ/server.h.
	SpawnParms [SpawnParmCount]float32

	// Skill is the active difficulty rung at snapshot time (0..3).
	// tyrquake: skill cvar value.
	Skill int

	// MapName is the bare map slug ("e1m1"); LoadSlot expands it to
	// "maps/<MapName>.bsp" via the server's MapBSPPath helper.
	MapName string

	// Time is sv.time at snapshot moment. Restored verbatim onto the
	// re-spawned Server so per-edict nextthink deadlines stay relative
	// to the same wall-clock.
	Time float32

	// Globals is the ordered list of (key, value) pairs the snapshot
	// dumped from the QC global table. Empty when no DEF_SAVEGLOBAL
	// globals are declared.
	Globals []KV

	// Edicts is the per-slot snapshot, indexed identically to
	// Server.Edicts. Slot 0 is the worldspawn entity; slots 1..N
	// are clients + monsters + items.
	Edicts []EdictSnap
}

// Sentinel errors.
var (
	// ErrShortRead fires when Load runs out of input before consuming
	// the header (version / comment / spawn parms / skill / map /
	// time). A truncated saves dies cleanly here.
	ErrShortRead = errors.New("savegame: short read (truncated header)")

	// ErrBadVersion fires when the on-disk version line is not
	// strconv-parsable or differs from FormatVersion.
	ErrBadVersion = errors.New("savegame: bad format version")

	// ErrBadNumber fires when a header line that should be a float /
	// int fails to parse (skill, sv.time, spawn parms).
	ErrBadNumber = errors.New("savegame: bad number in header")

	// ErrUnterminatedString fires when a quoted key/value pair has no
	// closing quote on the same line.
	ErrUnterminatedString = errors.New("savegame: unterminated quoted string")

	// ErrMalformedBlock fires when an edict-block "{" / "}" pair is
	// unbalanced or contains a token that is neither a quoted KV pair
	// nor the closing "}".
	ErrMalformedBlock = errors.New("savegame: malformed edict block")
)

// Encode writes s to w in the upstream's text save format. The bytes
// are independent of host endianness (the on-disk shape is line-
// oriented ASCII); a save produced on amd64 reads byte-for-byte
// identical on big-endian s390x.
//
// Caller is responsible for flushing buffered writers.
//
// Returns the first write error encountered. Empty Save.Edicts is
// tolerated (the per-edict block sequence is empty).
func (s *Save) Encode(w io.Writer) error {
	sw := asStringWriter(w)
	if _, err := fmt.Fprintf(sw, "%d\n", FormatVersion); err != nil {
		return err
	}
	// Comment is a single line; embedded newlines would break Load.
	if _, err := fmt.Fprintf(sw, "%s\n", sanitizeLine(s.Comment)); err != nil {
		return err
	}
	for i := 0; i < SpawnParmCount; i++ {
		if _, err := fmt.Fprintf(sw, "%g\n", s.SpawnParms[i]); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(sw, "%d\n", s.Skill); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(sw, "%s\n", sanitizeLine(s.MapName)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(sw, "%g\n", s.Time); err != nil {
		return err
	}
	// Globals block. Upstream wraps the block in "{" / "}" with one
	// quoted KV pair per line.
	if err := writeBlock(sw, s.Globals); err != nil {
		return err
	}
	for i := range s.Edicts {
		if s.Edicts[i].Free {
			if _, err := sw.WriteString("{\n}\n"); err != nil {
				return err
			}
			continue
		}
		if err := writeBlock(sw, s.Edicts[i].FieldKV); err != nil {
			return err
		}
	}
	return nil
}

// writeBlock emits one "{" / KV-lines / "}" group.
func writeBlock(w stringWriter, kvs []KV) error {
	if _, err := w.WriteString("{\n"); err != nil {
		return err
	}
	for _, kv := range kvs {
		if _, err := fmt.Fprintf(w, "\"%s\" \"%s\"\n", kv.Key, kv.Value); err != nil {
			return err
		}
	}
	if _, err := w.WriteString("}\n"); err != nil {
		return err
	}
	return nil
}

// sanitizeLine collapses any embedded CR / LF into a single space so
// the line-oriented decoder isn't tricked into splitting a comment /
// mapname across multiple records. Tabs are preserved.
func sanitizeLine(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	r := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")
	return r.Replace(s)
}

// Load parses the upstream text-format save from r. Returns a fully
// populated *Save on success; a sentinel error (see ErrShortRead /
// ErrBadVersion / ErrBadNumber / ErrUnterminatedString /
// ErrMalformedBlock) on a malformed input.
//
// The reader is consumed up to (and including) the closing "}" of the
// last per-edict block; bytes past that are not read. EOF after the
// expected header + globals block + zero-or-more edict blocks is the
// happy path.
func Load(r io.Reader) (*Save, error) {
	sc := bufio.NewScanner(r)
	// Saves can carry very long lines (a vec3 dump of a long
	// classname); bump the buffer over the default 64 KiB.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	next := func() (string, bool) { return scanLine(sc) }

	verLine, ok := next()
	if !ok {
		return nil, ErrShortRead
	}
	ver, err := strconv.Atoi(strings.TrimSpace(verLine))
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrBadVersion, verLine)
	}
	if ver != FormatVersion {
		return nil, fmt.Errorf("%w: got %d want %d", ErrBadVersion, ver, FormatVersion)
	}

	comment, ok := next()
	if !ok {
		return nil, ErrShortRead
	}

	s := &Save{Comment: comment}
	for i := 0; i < SpawnParmCount; i++ {
		line, ok := next()
		if !ok {
			return nil, ErrShortRead
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(line), 32)
		if err != nil {
			return nil, fmt.Errorf("%w: spawn parm %d: %q", ErrBadNumber, i, line)
		}
		s.SpawnParms[i] = float32(f)
	}

	skillLine, ok := next()
	if !ok {
		return nil, ErrShortRead
	}
	skill, err := strconv.Atoi(strings.TrimSpace(skillLine))
	if err != nil {
		return nil, fmt.Errorf("%w: skill: %q", ErrBadNumber, skillLine)
	}
	s.Skill = skill

	mapName, ok := next()
	if !ok {
		return nil, ErrShortRead
	}
	s.MapName = mapName

	timeLine, ok := next()
	if !ok {
		return nil, ErrShortRead
	}
	t, err := strconv.ParseFloat(strings.TrimSpace(timeLine), 32)
	if err != nil {
		return nil, fmt.Errorf("%w: time: %q", ErrBadNumber, timeLine)
	}
	s.Time = float32(t)

	// Globals block first, then zero-or-more edict blocks.
	globals, err := readBlock(sc, next)
	if err != nil {
		return nil, err
	}
	s.Globals = globals

	for {
		line, ok := next()
		if !ok {
			break
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if trim != "{" {
			return nil, fmt.Errorf("%w: edict header %q", ErrMalformedBlock, line)
		}
		kvs, err := readBlockBody(sc, next)
		if err != nil {
			return nil, err
		}
		s.Edicts = append(s.Edicts, EdictSnap{
			Free:    len(kvs) == 0,
			FieldKV: kvs,
		})
	}

	return s, nil
}

// scanLine returns the next line via sc. Returns ("", false) at EOF.
// CRLF inputs are handled by bufio.ScanLines's built-in "\r\n" strip,
// so callers see plain UTF-8 with no trailing whitespace.
func scanLine(sc *bufio.Scanner) (string, bool) {
	if !sc.Scan() {
		return "", false
	}
	return sc.Text(), true
}

// readBlock consumes a full "{" header + body via readBlockBody.
func readBlock(sc *bufio.Scanner, next func() (string, bool)) ([]KV, error) {
	line, ok := next()
	if !ok {
		return nil, fmt.Errorf("%w: missing block header", ErrMalformedBlock)
	}
	if strings.TrimSpace(line) != "{" {
		return nil, fmt.Errorf("%w: block header %q", ErrMalformedBlock, line)
	}
	return readBlockBody(sc, next)
}

// readBlockBody consumes KV lines until the matching "}" closer.
// Empty body (immediate "}") is the canonical "free slot" marker.
func readBlockBody(_ *bufio.Scanner, next func() (string, bool)) ([]KV, error) {
	var out []KV
	for {
		line, ok := next()
		if !ok {
			return nil, fmt.Errorf("%w: unterminated block", ErrMalformedBlock)
		}
		trim := strings.TrimSpace(line)
		if trim == "}" {
			return out, nil
		}
		kv, err := parseKVLine(trim)
		if err != nil {
			return nil, err
		}
		out = append(out, kv)
	}
}

// parseKVLine extracts "<key>" "<value>" from one line. The upstream
// format guarantees double-quote framing on both halves; embedded
// quotes are not escaped (vanilla Q1 saves have no such case).
func parseKVLine(line string) (KV, error) {
	key, rest, err := readQuoted(line)
	if err != nil {
		return KV{}, err
	}
	val, _, err := readQuoted(rest)
	if err != nil {
		return KV{}, err
	}
	return KV{Key: key, Value: val}, nil
}

// readQuoted consumes leading whitespace, expects a double-quote, then
// returns everything up to the next double-quote plus the unconsumed
// tail (which may start with another quoted token).
func readQuoted(s string) (token, rest string, err error) {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	if i >= len(s) || s[i] != '"' {
		return "", "", fmt.Errorf("%w: %q", ErrUnterminatedString, s)
	}
	i++ // step past opening quote
	start := i
	for i < len(s) && s[i] != '"' {
		i++
	}
	if i >= len(s) {
		return "", "", fmt.Errorf("%w: %q", ErrUnterminatedString, s)
	}
	tok := s[start:i]
	i++ // step past closing quote
	return tok, s[i:], nil
}

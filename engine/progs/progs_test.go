// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// build constructs a minimal valid progs.dat from typed sections and
// returns a bytes.Reader + the synthesised size.
type buildSpec struct {
	stmts []Statement
	gdefs []Def
	fdefs []Def
	funs  []Function
	strs  []byte
	gvals []byte
	entityFields int32
	version int32 // 0 -> default ProgVersion
}

func build(spec buildSpec) ([]byte, int64) {
	v := spec.version
	if v == 0 {
		v = ProgVersion
	}
	// Place sections sequentially after the header.
	pos := int64(headerSize)
	ofsStmts := pos
	stmtBytes := make([]byte, len(spec.stmts)*statementSize)
	for i, s := range spec.stmts {
		off := i * statementSize
		binary.LittleEndian.PutUint16(stmtBytes[off:off+2], uint16(s.Op))
		binary.LittleEndian.PutUint16(stmtBytes[off+2:off+4], uint16(s.A))
		binary.LittleEndian.PutUint16(stmtBytes[off+4:off+6], uint16(s.B))
		binary.LittleEndian.PutUint16(stmtBytes[off+6:off+8], uint16(s.C))
	}
	pos += int64(len(stmtBytes))

	ofsGDefs := pos
	gdefBytes := encodeDefs(spec.gdefs)
	pos += int64(len(gdefBytes))

	ofsFDefs := pos
	fdefBytes := encodeDefs(spec.fdefs)
	pos += int64(len(fdefBytes))

	ofsFuns := pos
	funBytes := encodeFuns(spec.funs)
	pos += int64(len(funBytes))

	ofsStrs := pos
	pos += int64(len(spec.strs))

	ofsGlobals := pos
	pos += int64(len(spec.gvals))

	hdr := Header{
		Version:        v,
		CRC:            -559038737, // 0xdeadbeef reinterpreted as int32
		OfsStatements:  int32(ofsStmts),
		NumStatements:  int32(len(spec.stmts)),
		OfsGlobalDefs:  int32(ofsGDefs),
		NumGlobalDefs:  int32(len(spec.gdefs)),
		OfsFieldDefs:   int32(ofsFDefs),
		NumFieldDefs:   int32(len(spec.fdefs)),
		OfsFunctions:   int32(ofsFuns),
		NumFunctions:   int32(len(spec.funs)),
		OfsStrings:     int32(ofsStrs),
		StringsSize:    int32(len(spec.strs)),
		OfsGlobals:     int32(ofsGlobals),
		NumGlobals:     int32(len(spec.gvals) / globalSlotSize),
		EntityFields:   spec.entityFields,
	}
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.LittleEndian, &hdr)
	buf.Write(stmtBytes)
	buf.Write(gdefBytes)
	buf.Write(fdefBytes)
	buf.Write(funBytes)
	buf.Write(spec.strs)
	buf.Write(spec.gvals)
	out := buf.Bytes()
	return out, int64(len(out))
}

func encodeDefs(defs []Def) []byte {
	out := make([]byte, len(defs)*defSize)
	for i, d := range defs {
		off := i * defSize
		binary.LittleEndian.PutUint16(out[off:off+2], d.Type)
		binary.LittleEndian.PutUint16(out[off+2:off+4], d.Ofs)
		binary.LittleEndian.PutUint32(out[off+4:off+8], uint32(d.SName))
	}
	return out
}

func encodeFuns(funs []Function) []byte {
	out := make([]byte, len(funs)*functionSize)
	for i, f := range funs {
		off := i * functionSize
		binary.LittleEndian.PutUint32(out[off:off+4], uint32(f.FirstStatement))
		binary.LittleEndian.PutUint32(out[off+4:off+8], uint32(f.ParmStart))
		binary.LittleEndian.PutUint32(out[off+8:off+12], uint32(f.Locals))
		binary.LittleEndian.PutUint32(out[off+12:off+16], uint32(f.Profile))
		binary.LittleEndian.PutUint32(out[off+16:off+20], uint32(f.SName))
		binary.LittleEndian.PutUint32(out[off+20:off+24], uint32(f.SFile))
		binary.LittleEndian.PutUint32(out[off+24:off+28], uint32(f.NumParms))
		copy(out[off+28:off+28+MaxParms], f.ParmSize[:])
	}
	return out
}

// --- happy path -------------------------------------------------------------

func TestLoad_MinimalValid(t *testing.T) {
	raw, sz := build(buildSpec{
		stmts: []Statement{
			{Op: OP_DONE, A: 0, B: 0, C: 0},
		},
		gdefs: []Def{{Type: uint16(EvFloat), Ofs: OfsReturn, SName: 1}},
		fdefs: []Def{{Type: uint16(EvVector), Ofs: 0, SName: 1}},
		funs: []Function{
			{FirstStatement: 0, ParmStart: 0, Locals: 0, SName: 1, NumParms: 0},
		},
		strs:  append([]byte{0}, []byte("hello\x00")...),
		gvals: make([]byte, 4),
		entityFields: 64,
	})
	p, err := Load(bytes.NewReader(raw), sz)
	if err != nil {
		t.Fatal(err)
	}
	if p.Header.Version != ProgVersion {
		t.Errorf("Version: %d", p.Header.Version)
	}
	if len(p.Statements) != 1 || p.Statements[0].Op != OP_DONE {
		t.Errorf("Statements: %+v", p.Statements)
	}
	if len(p.GlobalDefs) != 1 || p.GlobalDefs[0].Type != uint16(EvFloat) {
		t.Errorf("GlobalDefs: %+v", p.GlobalDefs)
	}
	if len(p.FieldDefs) != 1 || p.FieldDefs[0].Type != uint16(EvVector) {
		t.Errorf("FieldDefs: %+v", p.FieldDefs)
	}
	if len(p.Functions) != 1 || p.Functions[0].SName != 1 {
		t.Errorf("Functions: %+v", p.Functions)
	}
	if got := p.String(1); got != "hello" {
		t.Errorf("String(1): %q want hello", got)
	}
}

// --- error paths ------------------------------------------------------------

func TestLoad_NilSrc(t *testing.T) {
	if _, err := Load(nil, 100); err == nil {
		t.Error("expected nil-src error")
	}
}

func TestLoad_ShortHeader(t *testing.T) {
	if _, err := Load(bytes.NewReader([]byte{1, 2}), 2); !errors.Is(err, ErrShortRead) {
		t.Errorf("got %v want ErrShortRead", err)
	}
}

type errReader struct{}

func (errReader) ReadAt([]byte, int64) (int, error) { return 0, errors.New("io fail") }

func TestLoad_ReadFails(t *testing.T) {
	if _, err := Load(errReader{}, 100); err == nil {
		t.Error("expected io error")
	}
}

type shortReader struct{}

func (shortReader) ReadAt(p []byte, _ int64) (int, error) { return len(p) / 2, io.EOF }

func TestLoad_ShortRead(t *testing.T) {
	if _, err := Load(shortReader{}, 100); !errors.Is(err, ErrShortRead) {
		t.Errorf("got %v want ErrShortRead", err)
	}
}

func TestLoad_BadVersion(t *testing.T) {
	raw, sz := build(buildSpec{version: 7, strs: []byte{0}, gvals: make([]byte, 4)})
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrBadVersion) {
		t.Errorf("got %v want ErrBadVersion", err)
	}
}

// Header decode fails when the buffer is exactly headerSize but
// the inner binary.Read can still error on weird buffers. Cover the
// decode-error branch by passing a buffer of the right size that
// claims version 6 but truncates Globals.
func TestLoad_SectionOutOfRange(t *testing.T) {
	// Build a valid file but lie about NumStatements so the section
	// extends past EOF.
	raw, sz := build(buildSpec{
		strs:  []byte{0},
		gvals: make([]byte, 4),
	})
	// Patch NumStatements to a value that exceeds the file.
	binary.LittleEndian.PutUint32(raw[12:16], 1<<20) // ofs_statements unchanged, but num huge
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

// --- String helper ----------------------------------------------------------

// Direct parse() invocation with a buffer too short for the header
// covers the binary.Read error path that Load's size-check normally
// prevents from firing.
func TestParse_TruncatedHeader(t *testing.T) {
	if _, err := parse([]byte{1, 2, 3, 4}); err == nil {
		t.Error("expected header-decode error on truncated buffer")
	}
}

func TestProgs_String_EdgeCases(t *testing.T) {
	p := &Progs{Strings: []byte("\x00foo\x00bar\x00trailing-no-nul")}
	cases := []struct {
		off  int32
		want string
	}{
		{0, ""},
		{1, "foo"},
		{5, "bar"},
		{9, "trailing-no-nul"},
		{-1, ""},
		{1000, ""},
	}
	for _, c := range cases {
		if got := p.String(c.off); got != c.want {
			t.Errorf("String(%d): got %q want %q", c.off, got, c.want)
		}
	}
}

// --- whole-section bookkeeping ---------------------------------------------

func TestCheckSection_Empty(t *testing.T) {
	if err := checkSection(0, 0, 0, 4); err != nil {
		t.Errorf("empty section should be valid: %v", err)
	}
}

func TestCheckSection_NegativeArgs(t *testing.T) {
	if err := checkSection(1024, -1, 1, 4); !errors.Is(err, ErrSectionOutOfRange) {
		t.Error("negative ofs should fail")
	}
	if err := checkSection(1024, 0, -1, 4); !errors.Is(err, ErrSectionOutOfRange) {
		t.Error("negative count should fail")
	}
}

func TestCheckSection_OfsPastEnd(t *testing.T) {
	if err := checkSection(100, 200, 1, 4); !errors.Is(err, ErrSectionOutOfRange) {
		t.Error("ofs > fileSize should fail")
	}
}

func TestLoad_GlobalsOutOfRange(t *testing.T) {
	raw, sz := build(buildSpec{strs: []byte{0}, gvals: make([]byte, 4)})
	// Lie about NumGlobals to push past EOF.
	binary.LittleEndian.PutUint32(raw[13*4:13*4+4], 1<<20) // NumGlobals field offset
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

func TestLoad_StringsOutOfRange(t *testing.T) {
	raw, sz := build(buildSpec{strs: []byte{0}, gvals: make([]byte, 4)})
	// Lie about StringsSize.
	binary.LittleEndian.PutUint32(raw[11*4:11*4+4], 1<<20) // StringsSize field offset
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

// Cover GlobalDefs / FieldDefs / Functions out-of-range paths.
func TestLoad_GlobalDefsOutOfRange(t *testing.T) {
	raw, sz := build(buildSpec{strs: []byte{0}, gvals: make([]byte, 4)})
	binary.LittleEndian.PutUint32(raw[5*4:5*4+4], 1<<20) // NumGlobalDefs
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

func TestLoad_FieldDefsOutOfRange(t *testing.T) {
	raw, sz := build(buildSpec{strs: []byte{0}, gvals: make([]byte, 4)})
	binary.LittleEndian.PutUint32(raw[7*4:7*4+4], 1<<20) // NumFieldDefs
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

func TestLoad_FunctionsOutOfRange(t *testing.T) {
	raw, sz := build(buildSpec{strs: []byte{0}, gvals: make([]byte, 4)})
	binary.LittleEndian.PutUint32(raw[9*4:9*4+4], 1<<20) // NumFunctions
	if _, err := Load(bytes.NewReader(raw), sz); !errors.Is(err, ErrSectionOutOfRange) {
		t.Errorf("got %v want ErrSectionOutOfRange", err)
	}
}

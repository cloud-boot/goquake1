// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

import (
	"errors"
	"testing"
)

// progsForVM returns a stub Progs with:
//   - Functions[0] = empty (the upstream "null function" slot)
//   - Functions[1] = the test function, starting at Statements[1]
//   - Statements provided by the caller via withStatements
//   - Globals sized to 256 slots (1024 bytes) -- big enough for any
//     test offset we use
func progsForVM(stmts []Statement) *Progs {
	p := &Progs{
		Header:     Header{EntityFields: 32},
		Strings:    []byte{0},
		Globals:    make([]byte, 256*globalSlotSize),
		Statements: append([]Statement{{Op: OP_DONE}}, stmts...),
		Functions: []Function{
			{FirstStatement: 0, SName: 0},
			{FirstStatement: 1, SName: 0, NumParms: 0, Locals: 0, ParmStart: 0},
		},
	}
	return p
}

// withStatements appends OP_DONE after the statements so Run
// terminates cleanly even when the test program is just one op.
func withStatements(ops ...Statement) []Statement {
	return append(ops, Statement{Op: OP_DONE})
}

// --- VM construction --------------------------------------------------------

func TestNewVM_NilProgsPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil progs")
		}
	}()
	NewVM(nil)
}

func TestNewVM_IsolatesGlobals(t *testing.T) {
	p := progsForVM(nil)
	v1 := NewVM(p)
	v2 := NewVM(p)
	if &v1.globals[0] == &v2.globals[0] {
		t.Error("VMs should not share underlying globals storage")
	}
	_ = v1.SetGlobalFloat(10, 42)
	g2, _ := v2.GlobalFloat(10)
	if g2 != 0 {
		t.Errorf("VM2 saw VM1's mutation: %v", g2)
	}
}

// --- global accessors -------------------------------------------------------

func TestGlobalAccessors_RoundTrip(t *testing.T) {
	vm := NewVM(progsForVM(nil))
	if err := vm.SetGlobalFloat(10, 3.14); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(10); v != 3.14 {
		t.Errorf("float: %v", v)
	}
	if err := vm.SetGlobalInt(20, -1234); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalInt(20); v != -1234 {
		t.Errorf("int: %v", v)
	}
	if err := vm.SetGlobalVector(30, [3]float32{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalVector(30); v != [3]float32{1, 2, 3} {
		t.Errorf("vec: %v", v)
	}
}

func TestGlobalAccessors_OutOfRange(t *testing.T) {
	vm := NewVM(progsForVM(nil))
	if _, err := vm.GlobalFloat(-1); !errors.Is(err, ErrGlobalOffset) {
		t.Error("Float neg")
	}
	if _, err := vm.GlobalInt(1 << 20); !errors.Is(err, ErrGlobalOffset) {
		t.Error("Int far")
	}
	if _, err := vm.GlobalVector(-1); !errors.Is(err, ErrGlobalOffset) {
		t.Error("Vec neg")
	}
	if err := vm.SetGlobalFloat(-1, 0); !errors.Is(err, ErrGlobalOffset) {
		t.Error("SetFloat neg")
	}
	if err := vm.SetGlobalInt(1<<20, 0); !errors.Is(err, ErrGlobalOffset) {
		t.Error("SetInt far")
	}
	if err := vm.SetGlobalVector(-1, [3]float32{}); !errors.Is(err, ErrGlobalOffset) {
		t.Error("SetVec neg")
	}
}

// --- Run dispatch + the simple opcodes --------------------------------------

// helper: set up two scalar inputs, run the binary op, verify result.
func runBinaryF(t *testing.T, op Op, av, bv, want float32) {
	t.Helper()
	p := progsForVM(withStatements(Statement{Op: op, A: 10, B: 11, C: 12}))
	vm := NewVM(p)
	_ = vm.SetGlobalFloat(10, av)
	_ = vm.SetGlobalFloat(11, bv)
	if err := vm.Run(1); err != nil {
		t.Fatalf("%v Run: %v", op, err)
	}
	got, _ := vm.GlobalFloat(12)
	if got != want {
		t.Errorf("op=%d %v op %v -> got %v want %v", op, av, bv, got, want)
	}
}

func TestRun_Arithmetic_Float(t *testing.T) {
	runBinaryF(t, OP_ADD_F, 2, 3, 5)
	runBinaryF(t, OP_SUB_F, 7, 3, 4)
	runBinaryF(t, OP_MUL_F, 2, 3, 6)
	runBinaryF(t, OP_DIV_F, 10, 2, 5)
	runBinaryF(t, OP_BITAND, 6, 3, 2)
	runBinaryF(t, OP_BITOR, 6, 3, 7)
}

func TestRun_DivByZero(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_DIV_F, A: 10, B: 11, C: 12}))
	vm := NewVM(p)
	_ = vm.SetGlobalFloat(10, 1)
	_ = vm.SetGlobalFloat(11, 0)
	if err := vm.Run(1); !errors.Is(err, ErrDivByZero) {
		t.Errorf("got %v want ErrDivByZero", err)
	}
}

func TestRun_Vector_AddSub(t *testing.T) {
	p := progsForVM(withStatements(
		Statement{Op: OP_ADD_V, A: 30, B: 33, C: 36},
		Statement{Op: OP_SUB_V, A: 30, B: 33, C: 39},
	))
	vm := NewVM(p)
	_ = vm.SetGlobalVector(30, [3]float32{1, 2, 3})
	_ = vm.SetGlobalVector(33, [3]float32{4, 5, 6})
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalVector(36); v != [3]float32{5, 7, 9} {
		t.Errorf("ADD_V: %v", v)
	}
	if v, _ := vm.GlobalVector(39); v != [3]float32{-3, -3, -3} {
		t.Errorf("SUB_V: %v", v)
	}
}

func TestRun_Vector_DotAndScale(t *testing.T) {
	p := progsForVM(withStatements(
		Statement{Op: OP_MUL_V, A: 30, B: 33, C: 12},  // dot product -> float
		Statement{Op: OP_MUL_FV, A: 12, B: 33, C: 36}, // scalar * vec
		Statement{Op: OP_MUL_VF, A: 30, B: 12, C: 39}, // vec * scalar
	))
	vm := NewVM(p)
	_ = vm.SetGlobalVector(30, [3]float32{1, 2, 3})
	_ = vm.SetGlobalVector(33, [3]float32{4, 5, 6})
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(12); v != 32 { // 1*4 + 2*5 + 3*6
		t.Errorf("MUL_V dot: %v", v)
	}
	// After MUL_V slot 12 = 32; MUL_FV uses slot 12 as the scalar.
	if v, _ := vm.GlobalVector(36); v != [3]float32{128, 160, 192} {
		t.Errorf("MUL_FV: %v", v)
	}
	if v, _ := vm.GlobalVector(39); v != [3]float32{32, 64, 96} {
		t.Errorf("MUL_VF: %v", v)
	}
}

// --- comparison + logical ---------------------------------------------------

func TestRun_Comparison_Float(t *testing.T) {
	cases := []struct {
		op   Op
		a, b float32
		want float32
	}{
		{OP_GE, 5, 5, 1}, {OP_GE, 4, 5, 0},
		{OP_LE, 5, 5, 1}, {OP_LE, 6, 5, 0},
		{OP_GT, 6, 5, 1}, {OP_GT, 5, 5, 0},
		{OP_LT, 4, 5, 1}, {OP_LT, 5, 5, 0},
		{OP_EQ_F, 1, 1, 1}, {OP_EQ_F, 1, 2, 0},
		{OP_NE_F, 1, 2, 1}, {OP_NE_F, 1, 1, 0},
	}
	for _, c := range cases {
		runBinaryF(t, c.op, c.a, c.b, c.want)
	}
}

func TestRun_Logical_AndOr(t *testing.T) {
	runBinaryF(t, OP_AND, 0, 0, 0)
	runBinaryF(t, OP_AND, 1, 0, 0)
	runBinaryF(t, OP_AND, 1, 1, 1)
	runBinaryF(t, OP_OR, 0, 0, 0)
	runBinaryF(t, OP_OR, 1, 0, 1)
	runBinaryF(t, OP_OR, 0, 1, 1)
}

func TestRun_Comparison_Vector(t *testing.T) {
	p := progsForVM(withStatements(
		Statement{Op: OP_EQ_V, A: 30, B: 33, C: 12},
		Statement{Op: OP_NE_V, A: 30, B: 33, C: 13},
	))
	vm := NewVM(p)
	_ = vm.SetGlobalVector(30, [3]float32{1, 2, 3})
	_ = vm.SetGlobalVector(33, [3]float32{1, 2, 3})
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(12); v != 1 {
		t.Errorf("EQ_V equal: %v", v)
	}
	if v, _ := vm.GlobalFloat(13); v != 0 {
		t.Errorf("NE_V equal: %v", v)
	}
}

func TestRun_Comparison_String(t *testing.T) {
	p := progsForVM(withStatements(
		Statement{Op: OP_EQ_S, A: 10, B: 11, C: 12},
		Statement{Op: OP_NE_S, A: 10, B: 11, C: 13},
	))
	// Populate string table.
	p.Strings = []byte("\x00hello\x00world\x00hello\x00")
	vm := NewVM(p)
	_ = vm.SetGlobalInt(10, 1)  // "hello"
	_ = vm.SetGlobalInt(11, 13) // "hello"
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(12); v != 1 {
		t.Errorf("EQ_S equal strings: %v", v)
	}
	if v, _ := vm.GlobalFloat(13); v != 0 {
		t.Errorf("NE_S equal strings: %v", v)
	}
}

func TestRun_Comparison_EntityFunc(t *testing.T) {
	p := progsForVM(withStatements(
		Statement{Op: OP_EQ_E, A: 10, B: 11, C: 12},
		Statement{Op: OP_NE_E, A: 10, B: 11, C: 13},
		Statement{Op: OP_EQ_FNC, A: 10, B: 11, C: 14},
		Statement{Op: OP_NE_FNC, A: 10, B: 11, C: 15},
	))
	vm := NewVM(p)
	_ = vm.SetGlobalInt(10, 7)
	_ = vm.SetGlobalInt(11, 7)
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(12); v != 1 {
		t.Errorf("EQ_E: %v", v)
	}
	if v, _ := vm.GlobalFloat(13); v != 0 {
		t.Errorf("NE_E: %v", v)
	}
	if v, _ := vm.GlobalFloat(14); v != 1 {
		t.Errorf("EQ_FNC: %v", v)
	}
	if v, _ := vm.GlobalFloat(15); v != 0 {
		t.Errorf("NE_FNC: %v", v)
	}
}

// --- NOT_* ------------------------------------------------------------------

func TestRun_NotFamily(t *testing.T) {
	p := progsForVM(withStatements(
		Statement{Op: OP_NOT_F, A: 10, C: 20},
		Statement{Op: OP_NOT_V, A: 30, C: 21}, // vector all-zero -> 1
		Statement{Op: OP_NOT_S, A: 13, C: 22}, // string ofs 0 -> 1
		Statement{Op: OP_NOT_FNC, A: 14, C: 23},
		Statement{Op: OP_NOT_ENT, A: 14, C: 24},
	))
	p.Strings = []byte{0}
	vm := NewVM(p)
	_ = vm.SetGlobalFloat(10, 0)
	_ = vm.SetGlobalVector(30, [3]float32{0, 0, 0})
	_ = vm.SetGlobalInt(13, 0)
	_ = vm.SetGlobalInt(14, 0)
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	for _, slot := range []int{20, 21, 22, 23, 24} {
		v, _ := vm.GlobalFloat(slot)
		if v != 1 {
			t.Errorf("slot %d: got %v want 1", slot, v)
		}
	}
}

// --- STORE_* ----------------------------------------------------------------

func TestRun_StoreFloat(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_STORE_F, A: 10, B: 11}))
	vm := NewVM(p)
	_ = vm.SetGlobalFloat(10, 9.5)
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(11); v != 9.5 {
		t.Errorf("STORE_F: %v", v)
	}
}

func TestRun_StoreVector(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_STORE_V, A: 30, B: 33}))
	vm := NewVM(p)
	_ = vm.SetGlobalVector(30, [3]float32{1, 2, 3})
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalVector(33); v != [3]float32{1, 2, 3} {
		t.Errorf("STORE_V: %v", v)
	}
}

// --- jumps ------------------------------------------------------------------

func TestRun_GotoSkipsAhead(t *testing.T) {
	// GOTO +2 skips the SUB_F. Should leave slot 12 = 5 (from ADD_F),
	// not 1 (which SUB_F would have written).
	p := progsForVM(withStatements(
		Statement{Op: OP_ADD_F, A: 10, B: 11, C: 12},
		Statement{Op: OP_GOTO, A: 2},
		Statement{Op: OP_SUB_F, A: 10, B: 11, C: 12},
		Statement{Op: OP_DONE},
	))
	vm := NewVM(p)
	_ = vm.SetGlobalFloat(10, 3)
	_ = vm.SetGlobalFloat(11, 2)
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(12); v != 5 {
		t.Errorf("GOTO skip: got %v want 5", v)
	}
}

func TestRun_IfNotTakesBranch(t *testing.T) {
	// IFNOT a +2 skips next stmt when a==0.
	p := progsForVM(withStatements(
		Statement{Op: OP_IFNOT, A: 10, B: 2},
		Statement{Op: OP_STORE_F, A: 11, B: 12}, // skipped
		Statement{Op: OP_DONE},
	))
	vm := NewVM(p)
	_ = vm.SetGlobalInt(10, 0)
	_ = vm.SetGlobalFloat(11, 99)
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(12); v != 0 {
		t.Errorf("IFNOT branch taken (skip): slot 12 should be 0, got %v", v)
	}
}

func TestRun_IfTakesBranch(t *testing.T) {
	p := progsForVM(withStatements(
		Statement{Op: OP_IF, A: 10, B: 2},
		Statement{Op: OP_STORE_F, A: 11, B: 12}, // skipped when a!=0
		Statement{Op: OP_DONE},
	))
	vm := NewVM(p)
	_ = vm.SetGlobalInt(10, 1)
	_ = vm.SetGlobalFloat(11, 99)
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(12); v != 0 {
		t.Errorf("IF branch taken (skip): slot 12 should be 0, got %v", v)
	}
}

func TestRun_IfNotFallsThrough(t *testing.T) {
	p := progsForVM(withStatements(
		Statement{Op: OP_IFNOT, A: 10, B: 2},    // a!=0 -> don't take branch
		Statement{Op: OP_STORE_F, A: 11, B: 12}, // runs
		Statement{Op: OP_DONE},
	))
	vm := NewVM(p)
	_ = vm.SetGlobalInt(10, 1)
	_ = vm.SetGlobalFloat(11, 7)
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(12); v != 7 {
		t.Errorf("IFNOT fall-through: %v", v)
	}
}

// --- RETURN propagates value -----------------------------------------------

func TestRun_ReturnCopiesToReturnSlot(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_RETURN, A: 30}))
	vm := NewVM(p)
	_ = vm.SetGlobalVector(30, [3]float32{7, 8, 9})
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalVector(OfsReturn); v != [3]float32{7, 8, 9} {
		t.Errorf("OfsReturn: %v want {7,8,9}", v)
	}
}

// --- error paths -----------------------------------------------------------

func TestRun_NullFunctionRejected(t *testing.T) {
	vm := NewVM(progsForVM(nil))
	if err := vm.Run(0); !errors.Is(err, ErrBadFunctionIndex) {
		t.Errorf("got %v want ErrBadFunctionIndex", err)
	}
	if err := vm.Run(100); !errors.Is(err, ErrBadFunctionIndex) {
		t.Errorf("got %v want ErrBadFunctionIndex", err)
	}
	if err := vm.Run(-1); !errors.Is(err, ErrBadFunctionIndex) {
		t.Errorf("got %v want ErrBadFunctionIndex", err)
	}
}

func TestRun_StatementOutOfRange(t *testing.T) {
	// FirstStatement = 999 (past end of Statements).
	p := progsForVM(nil)
	p.Functions[1].FirstStatement = 999
	vm := NewVM(p)
	if err := vm.Run(1); !errors.Is(err, ErrBadStatement) {
		t.Errorf("got %v want ErrBadStatement", err)
	}
}

// --- OP_STATE -----------------------------------------------------------

func TestRun_STATE_NoArena(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_STATE, A: 10, B: 11}))
	vm := NewVM(p)
	if err := vm.Run(1); !errors.Is(err, ErrNoArena) {
		t.Errorf("got %v want ErrNoArena", err)
	}
}

func TestRun_STATE_NoHooks(t *testing.T) {
	vm, _ := vmWithArena(withStatements(Statement{Op: OP_STATE, A: 10, B: 11}))
	// arena set, but hooks + field offsets not.
	if err := vm.Run(1); !errors.Is(err, ErrNoStateHooks) {
		t.Errorf("got %v want ErrNoStateHooks", err)
	}
}

func TestRun_STATE_HooksPartial(t *testing.T) {
	// timeSource + selfEdict set but not field offsets -> still
	// ErrNoStateHooks.
	vm, _ := vmWithArena(withStatements(Statement{Op: OP_STATE, A: 10, B: 11}))
	vm.SetStateHooks(func() float32 { return 0 }, func() int32 { return 0 })
	if err := vm.Run(1); !errors.Is(err, ErrNoStateHooks) {
		t.Errorf("got %v want ErrNoStateHooks", err)
	}
}

func TestRun_STATE_HappyPath(t *testing.T) {
	vm, a := vmWithArena(withStatements(Statement{Op: OP_STATE, A: 10, B: 11}))
	selfE, _ := a.Get(1)
	// Hooks: server time = 5.0, self = edict 1.
	vm.SetStateHooks(
		func() float32 { return 5.0 },
		func() int32 { return a.PointerForEdict(selfE) },
	)
	// Field offsets within the entity field block (in slots).
	vm.SetStateFieldOffsets(0 /*nextthink*/, 1 /*frame*/, 2 /*think*/)
	// Globals: A=10 holds the new frame number; B=11 holds the new think function index.
	_ = vm.SetGlobalFloat(10, 23) // new frame
	_ = vm.SetGlobalInt(11, 99)   // new think func index
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := selfE.FieldFloat(0); v != 5.1 { // sv.time + 0.1
		t.Errorf("nextthink: got %v want 5.1", v)
	}
	if v, _ := selfE.FieldFloat(1); v != 23 {
		t.Errorf("frame: got %v want 23", v)
	}
	if v, _ := selfE.FieldInt(2); v != 99 {
		t.Errorf("think: got %v want 99", v)
	}
}

func TestRun_STATE_BadSelfPointer(t *testing.T) {
	vm, _ := vmWithArena(withStatements(Statement{Op: OP_STATE, A: 10, B: 11}))
	vm.SetStateHooks(
		func() float32 { return 0 },
		func() int32 { return -1 }, // bad self pointer
	)
	vm.SetStateFieldOffsets(0, 1, 2)
	if err := vm.Run(1); !errors.Is(err, ErrEdictIndex) {
		t.Errorf("got %v want ErrEdictIndex", err)
	}
}

func TestRun_STATE_BadFieldOffset(t *testing.T) {
	// Field offsets that point past the edict field block surface as
	// ErrFieldOffset from each Field* call.
	tests := []struct {
		name                    string
		nextThink, frame, think int
	}{
		{"nextthink-far", 1 << 20, 0, 0},
		{"frame-far", 0, 1 << 20, 0},
		{"think-far", 0, 0, 1 << 20},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm, a := vmWithArena(withStatements(Statement{Op: OP_STATE, A: 10, B: 11}))
			selfE, _ := a.Get(1)
			vm.SetStateHooks(
				func() float32 { return 0 },
				func() int32 { return a.PointerForEdict(selfE) },
			)
			vm.SetStateFieldOffsets(tc.nextThink, tc.frame, tc.think)
			if err := vm.Run(1); !errors.Is(err, ErrFieldOffset) {
				t.Errorf("got %v want ErrFieldOffset", err)
			}
		})
	}
}

func TestRun_UnsupportedOpcode(t *testing.T) {
	// Synthesise an opcode value past every implemented case.
	p := progsForVM(withStatements(Statement{Op: Op(9999), A: 10, B: 11}))
	vm := NewVM(p)
	if err := vm.Run(1); !errors.Is(err, ErrUnsupportedOp) {
		t.Errorf("got %v want ErrUnsupportedOp", err)
	}
}

func TestRun_Runaway(t *testing.T) {
	// Single-stmt infinite loop via GOTO -1.
	p := progsForVM(withStatements(Statement{Op: OP_GOTO, A: 0}))
	vm := NewVM(p)
	if err := vm.Run(1); !errors.Is(err, ErrRunaway) {
		t.Errorf("got %v want ErrRunaway", err)
	}
}

func TestEnterFunction_StackOverflow(t *testing.T) {
	vm := NewVM(progsForVM(nil))
	for i := 0; i < MaxStackDepth; i++ {
		_ = vm.enterFunction(1)
	}
	if err := vm.enterFunction(1); !errors.Is(err, ErrStackOverflow) {
		t.Errorf("got %v want ErrStackOverflow", err)
	}
}

func TestLeaveFunction_StackUnderflow(t *testing.T) {
	vm := NewVM(progsForVM(nil))
	if err := vm.leaveFunction(); !errors.Is(err, ErrStackUnderflow) {
		t.Errorf("got %v want ErrStackUnderflow", err)
	}
}

// --- enter/leave locals round-trip ----------------------------------------

func TestEnterLeave_RestoresLocals(t *testing.T) {
	// Function 1 has ParmStart=100, Locals=3. EnterFunction should
	// save globals[100..103] before running. Run something that
	// mutates them, then LeaveFunction restores.
	p := progsForVM(nil)
	p.Functions[1].ParmStart = 100
	p.Functions[1].Locals = 3
	vm := NewVM(p)
	_ = vm.SetGlobalFloat(100, 1)
	_ = vm.SetGlobalFloat(101, 2)
	_ = vm.SetGlobalFloat(102, 3)

	if err := vm.enterFunction(1); err != nil {
		t.Fatal(err)
	}
	// Stomp locals.
	_ = vm.SetGlobalFloat(100, 99)
	_ = vm.SetGlobalFloat(101, 99)
	_ = vm.SetGlobalFloat(102, 99)
	if err := vm.leaveFunction(); err != nil {
		t.Fatal(err)
	}
	for slot, want := range map[int]float32{100: 1, 101: 2, 102: 3} {
		got, _ := vm.GlobalFloat(slot)
		if got != want {
			t.Errorf("slot %d: got %v want %v", slot, got, want)
		}
	}
}

// --- enter copies parms -----------------------------------------------------

func TestEnterFunction_CopiesParms(t *testing.T) {
	p := progsForVM(nil)
	p.Functions[1].ParmStart = 100
	p.Functions[1].Locals = 4
	p.Functions[1].NumParms = 1
	p.Functions[1].ParmSize[0] = 1
	vm := NewVM(p)
	_ = vm.SetGlobalFloat(OfsParm0, 42)
	if err := vm.enterFunction(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(100); v != 42 {
		t.Errorf("parm copy: got %v want 42", v)
	}
}

// Forces Run to fail at its enterFunction call -- pre-fill the stack
// past MaxStackDepth so the first enterFunction inside Run trips
// the overflow guard immediately.
func TestRun_PropagatesEnterFunctionError(t *testing.T) {
	vm := NewVM(progsForVM(nil))
	for i := 0; i < MaxStackDepth; i++ {
		if err := vm.enterFunction(1); err != nil {
			t.Fatalf("setup enterFunction[%d]: %v", i, err)
		}
	}
	// Now Run's internal enterFunction must trip ErrStackOverflow.
	if err := vm.Run(1); !errors.Is(err, ErrStackOverflow) {
		t.Errorf("got %v want ErrStackOverflow", err)
	}
}

// Force enterFunction's parm-copy loop to propagate an error by
// constricting the globals so OfsParm0 is past the end. NumParms=1,
// ParmSize[0]=1 -> reads OfsParm0 (slot 4). A 4-byte globals pool
// (one slot, indices 0 only) makes slot 4 out of range.
func TestEnterFunction_PropagatesParmReadError(t *testing.T) {
	p := progsForVM(nil)
	p.Globals = make([]byte, 4) // 1 slot only -- OfsParm0 (slot 4) is out of range
	p.Functions[1].NumParms = 1
	p.Functions[1].ParmSize[0] = 1
	p.Functions[1].ParmStart = 0
	vm := NewVM(p)
	if err := vm.enterFunction(1); !errors.Is(err, ErrGlobalOffset) {
		t.Errorf("got %v want ErrGlobalOffset", err)
	}
}

// Force enterFunction's parm-WRITE to fail (SetGlobalInt dst out of
// range). OfsParm0=4 must be readable so a 32-byte globals pool (8
// slots) lets the read succeed, but ParmStart=1000 makes the write
// fail.
func TestEnterFunction_PropagatesParmWriteError(t *testing.T) {
	p := progsForVM(nil)
	p.Globals = make([]byte, 32) // 8 slots; OfsParm0=4 reads slot 4 OK
	p.Functions[1].NumParms = 1
	p.Functions[1].ParmSize[0] = 1
	p.Functions[1].ParmStart = 1000 // way past 8-slot pool
	vm := NewVM(p)
	if err := vm.enterFunction(1); !errors.Is(err, ErrGlobalOffset) {
		t.Errorf("got %v want ErrGlobalOffset", err)
	}
}

// Force OP_RETURN's leaveFunction to fail by calling execOne
// directly on a VM whose stack is empty -- leaveFunction underflows.
// This guards against the "RETURN after stack manipulation"
// pathological case (savegame-load with a corrupt return stack).
func TestExecOne_ReturnPropagatesLeaveError(t *testing.T) {
	p := progsForVM(nil)
	vm := NewVM(p)
	_, err := vm.execOne(Statement{Op: OP_RETURN, A: 0}, 0)
	if !errors.Is(err, ErrStackUnderflow) {
		t.Errorf("got %v want ErrStackUnderflow", err)
	}
}

// Force OP_RETURN's SetGlobalVector(OfsReturn) to fail by shrinking
// globals so OfsReturn..OfsReturn+2 is out of range.
func TestRun_PropagatesReturnError(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_RETURN, A: 0}))
	// 8 bytes = 2 slots. OfsReturn=1 needs slots 1,2,3 (bytes 4..16)
	// which is out of range.
	p.Globals = make([]byte, 8)
	vm := NewVM(p)
	if err := vm.Run(1); !errors.Is(err, ErrGlobalOffset) {
		t.Errorf("got %v want ErrGlobalOffset", err)
	}
}

// --- CALL: builtin dispatch ---------------------------------------------

func TestRun_CALL_BuiltinDispatch(t *testing.T) {
	// Function 2 is a builtin: first_statement = -7 -> builtins[7].
	// The QuakeC program calls Function 2 with no args, then DONE.
	// The builtin writes its result into slot 100 (not OfsReturn)
	// because the caller's final DONE clobbers OfsReturn with whatever
	// its A operand reads -- the tested invariant here is that the
	// builtin RAN, not the return-value plumbing (which Run_CALL_
	// QuakeCFunctionDispatch covers separately).
	p := progsForVM(withStatements(
		Statement{Op: OP_CALL0, A: 10},
	))
	p.Functions = append(p.Functions, Function{
		FirstStatement: -7, // builtin index 7
		SName:          0,
	})
	vm := NewVM(p)
	called := false
	vm.RegisterBuiltin(7, func(v *VM) error {
		called = true
		if v.Argc() != 0 {
			t.Errorf("Argc inside CALL0: got %d", v.Argc())
		}
		_ = v.SetGlobalFloat(100, 42)
		return nil
	})
	_ = vm.SetGlobalInt(10, 2) // function index 2
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("builtin not invoked")
	}
	if v, _ := vm.GlobalFloat(100); v != 42 {
		t.Errorf("builtin output slot: got %v want 42", v)
	}
}

func TestRun_CALL_ArgcArity(t *testing.T) {
	// CALL3 expects argc=3 inside the builtin.
	p := progsForVM(withStatements(Statement{Op: OP_CALL3, A: 10}))
	p.Functions = append(p.Functions, Function{FirstStatement: -1, SName: 0})
	vm := NewVM(p)
	gotArgc := -1
	vm.RegisterBuiltin(1, func(v *VM) error {
		gotArgc = v.Argc()
		return nil
	})
	_ = vm.SetGlobalInt(10, 2)
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if gotArgc != 3 {
		t.Errorf("Argc: got %d want 3", gotArgc)
	}
}

func TestRun_CALL_QuakeCFunctionDispatch(t *testing.T) {
	// QuakeC function call: Function 2 is a non-builtin starting at
	// Statement 3 which just RETURNs the vector in slot 50. The
	// caller's final DONE uses A=OfsReturn so its A->OfsReturn copy
	// is an identity write (matches what QuakeC compilers emit for
	// "the function we just called returned a value; preserve it").
	p := progsForVM(withStatements(
		Statement{Op: OP_CALL0, A: 10},       // 1
		Statement{Op: OP_DONE, A: OfsReturn}, // 2 -- caller's DONE (identity write)
		Statement{Op: OP_RETURN, A: 50},      // 3 -- callee body
	))
	p.Functions = append(p.Functions, Function{FirstStatement: 3, SName: 0})
	vm := NewVM(p)
	_ = vm.SetGlobalInt(10, 2) // call function 2
	_ = vm.SetGlobalVector(50, [3]float32{1, 2, 3})
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalVector(OfsReturn); v != [3]float32{1, 2, 3} {
		t.Errorf("OfsReturn: %v want {1,2,3}", v)
	}
}

func TestRun_CALL_NullFunction(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_CALL0, A: 10}))
	vm := NewVM(p)
	_ = vm.SetGlobalInt(10, 0) // null function index
	if err := vm.Run(1); !errors.Is(err, ErrNullCall) {
		t.Errorf("got %v want ErrNullCall", err)
	}
}

func TestRun_CALL_BadFunctionIndex(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_CALL0, A: 10}))
	vm := NewVM(p)
	_ = vm.SetGlobalInt(10, 9999) // out of range
	if err := vm.Run(1); !errors.Is(err, ErrBadFunctionIndex) {
		t.Errorf("got %v want ErrBadFunctionIndex", err)
	}
	_ = vm.SetGlobalInt(10, -1)
	if err := vm.Run(1); !errors.Is(err, ErrBadFunctionIndex) {
		t.Errorf("neg fn: got %v want ErrBadFunctionIndex", err)
	}
}

func TestRun_CALL_UnregisteredBuiltin(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_CALL0, A: 10}))
	p.Functions = append(p.Functions, Function{FirstStatement: -99, SName: 0})
	vm := NewVM(p)
	_ = vm.SetGlobalInt(10, 2)
	if err := vm.Run(1); !errors.Is(err, ErrBadBuiltin) {
		t.Errorf("got %v want ErrBadBuiltin", err)
	}
}

func TestRun_CALL_BuiltinErrorPropagates(t *testing.T) {
	customErr := errors.New("builtin blew up")
	p := progsForVM(withStatements(Statement{Op: OP_CALL0, A: 10}))
	p.Functions = append(p.Functions, Function{FirstStatement: -1, SName: 0})
	vm := NewVM(p)
	vm.RegisterBuiltin(1, func(*VM) error { return customErr })
	_ = vm.SetGlobalInt(10, 2)
	if err := vm.Run(1); !errors.Is(err, customErr) {
		t.Errorf("got %v want propagated builtin error", err)
	}
}

func TestRegisterBuiltin_ZeroIgnored(t *testing.T) {
	vm := NewVM(progsForVM(nil))
	vm.RegisterBuiltin(0, func(*VM) error { return nil })
	vm.RegisterBuiltin(-5, func(*VM) error { return nil })
	if vm.builtins != nil && (vm.builtins[0] != nil || vm.builtins[-5] != nil) {
		t.Error("zero / negative indices should not register")
	}
}

// --- arena resolver tests (edict.go FieldBytes / MakePointer / ResolvePointer / PointerForEdict)

func TestArena_PointerRoundTrip(t *testing.T) {
	p := progsForVM(nil)
	a := NewEdictArena(p, 4)
	a.Reset()
	if a.FieldBytes() != int(p.Header.EntityFields)*4 {
		t.Errorf("FieldBytes: %d", a.FieldBytes())
	}
	// Edict 2, slot 5 -> pointer -> resolve back.
	ptr := a.MakePointer(2, 5)
	ed, off, err := a.ResolvePointer(ptr)
	if err != nil {
		t.Fatal(err)
	}
	if ed != &a.edicts[2] {
		t.Errorf("ResolvePointer: wrong edict")
	}
	if off != 5*4 {
		t.Errorf("ResolvePointer offset: got %d want %d", off, 5*4)
	}
}

func TestArena_PointerForEdict(t *testing.T) {
	p := progsForVM(nil)
	a := NewEdictArena(p, 4)
	if a.PointerForEdict(&a.edicts[1]) != int32(a.FieldBytes()) {
		t.Errorf("PointerForEdict(e1)")
	}
	if a.PointerForEdict(&Edict{}) != -1 {
		t.Errorf("foreign edict should return -1")
	}
}

func TestArena_ResolvePointer_OutOfRange(t *testing.T) {
	p := progsForVM(nil)
	a := NewEdictArena(p, 2)
	if _, _, err := a.ResolvePointer(-1); !errors.Is(err, ErrEdictIndex) {
		t.Error("negative ptr should fail")
	}
	if _, _, err := a.ResolvePointer(int32(a.FieldBytes() * 100)); !errors.Is(err, ErrEdictIndex) {
		t.Error("ptr past arena should fail")
	}
}

func TestArena_ResolvePointer_ZeroFieldBytes(t *testing.T) {
	// EntityFields=0 makes FieldBytes=0; ResolvePointer must
	// refuse rather than divide by zero.
	p := &Progs{Header: Header{EntityFields: 0}, Globals: make([]byte, 4)}
	a := NewEdictArena(p, 2)
	if _, _, err := a.ResolvePointer(0); !errors.Is(err, ErrEdictIndex) {
		t.Errorf("got %v want ErrEdictIndex on zero fieldbytes", err)
	}
}

// --- VM with arena: LOAD_F, LOAD_V, STOREP_F, STOREP_V, ADDRESS -----------

func vmWithArena(stmts []Statement) (*VM, *EdictArena) {
	p := progsForVM(stmts)
	a := NewEdictArena(p, 4)
	a.Reset()
	vm := NewVM(p)
	vm.SetArena(a)
	return vm, a
}

func TestRun_LOAD_F(t *testing.T) {
	vm, a := vmWithArena(withStatements(Statement{Op: OP_LOAD_F, A: 10, B: 11, C: 12}))
	// Pre-fill edict 1, field slot 2 = 99.5
	e1, _ := a.Get(1)
	_ = e1.FieldSetFloat(2, 99.5)
	// Global 10 = pointer to edict 1; global 11 = field slot 2.
	_ = vm.SetGlobalInt(10, a.PointerForEdict(e1))
	_ = vm.SetGlobalInt(11, 2)
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalFloat(12); v != 99.5 {
		t.Errorf("LOAD_F: got %v want 99.5", v)
	}
}

func TestRun_LOAD_V(t *testing.T) {
	vm, a := vmWithArena(withStatements(Statement{Op: OP_LOAD_V, A: 10, B: 11, C: 30}))
	e1, _ := a.Get(1)
	want := [3]float32{1, 2, 3}
	_ = e1.FieldSetVector(4, want)
	_ = vm.SetGlobalInt(10, a.PointerForEdict(e1))
	_ = vm.SetGlobalInt(11, 4)
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := vm.GlobalVector(30); v != want {
		t.Errorf("LOAD_V: %v want %v", v, want)
	}
}

func TestRun_STOREP_F(t *testing.T) {
	vm, a := vmWithArena(withStatements(Statement{Op: OP_STOREP_F, A: 10, B: 11}))
	e2, _ := a.Get(2)
	_ = vm.SetGlobalFloat(10, 42)
	_ = vm.SetGlobalInt(11, a.MakePointer(2, 5))
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := e2.FieldFloat(5); v != 42 {
		t.Errorf("STOREP_F: %v want 42", v)
	}
}

func TestRun_STOREP_V(t *testing.T) {
	vm, a := vmWithArena(withStatements(Statement{Op: OP_STOREP_V, A: 30, B: 11}))
	e2, _ := a.Get(2)
	want := [3]float32{7, 8, 9}
	_ = vm.SetGlobalVector(30, want)
	_ = vm.SetGlobalInt(11, a.MakePointer(2, 4))
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	if v, _ := e2.FieldVector(4); v != want {
		t.Errorf("STOREP_V: %v", v)
	}
}

func TestRun_ADDRESS(t *testing.T) {
	vm, a := vmWithArena(withStatements(Statement{Op: OP_ADDRESS, A: 10, B: 11, C: 12}))
	e3, _ := a.Get(3)
	_ = vm.SetGlobalInt(10, a.PointerForEdict(e3))
	_ = vm.SetGlobalInt(11, 7) // slot 7 within e3
	if err := vm.Run(1); err != nil {
		t.Fatal(err)
	}
	got, _ := vm.GlobalInt(12)
	want := a.MakePointer(3, 7)
	if got != want {
		t.Errorf("ADDRESS: got %d want %d", got, want)
	}
}

// --- error paths for arena-coupled opcodes --------------------------------

func TestRun_LOAD_F_NoArena(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_LOAD_F, A: 10, B: 11, C: 12}))
	vm := NewVM(p)
	if err := vm.Run(1); !errors.Is(err, ErrNoArena) {
		t.Errorf("got %v want ErrNoArena", err)
	}
}

func TestRun_LOAD_V_NoArena(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_LOAD_V, A: 10, B: 11, C: 30}))
	vm := NewVM(p)
	if err := vm.Run(1); !errors.Is(err, ErrNoArena) {
		t.Errorf("got %v want ErrNoArena", err)
	}
}

func TestRun_STOREP_F_NoArena(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_STOREP_F, A: 10, B: 11}))
	vm := NewVM(p)
	if err := vm.Run(1); !errors.Is(err, ErrNoArena) {
		t.Errorf("got %v want ErrNoArena", err)
	}
}

func TestRun_STOREP_V_NoArena(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_STOREP_V, A: 30, B: 11}))
	vm := NewVM(p)
	if err := vm.Run(1); !errors.Is(err, ErrNoArena) {
		t.Errorf("got %v want ErrNoArena", err)
	}
}

func TestRun_ADDRESS_NoArena(t *testing.T) {
	p := progsForVM(withStatements(Statement{Op: OP_ADDRESS, A: 10, B: 11, C: 12}))
	vm := NewVM(p)
	if err := vm.Run(1); !errors.Is(err, ErrNoArena) {
		t.Errorf("got %v want ErrNoArena", err)
	}
}

func TestRun_LOAD_F_BadPointer(t *testing.T) {
	vm, _ := vmWithArena(withStatements(Statement{Op: OP_LOAD_F, A: 10, B: 11, C: 12}))
	_ = vm.SetGlobalInt(10, -1) // negative -> ResolvePointer fails
	if err := vm.Run(1); !errors.Is(err, ErrEdictIndex) {
		t.Errorf("got %v want ErrEdictIndex", err)
	}
}

func TestRun_LOAD_V_BadPointer(t *testing.T) {
	vm, _ := vmWithArena(withStatements(Statement{Op: OP_LOAD_V, A: 10, B: 11, C: 30}))
	_ = vm.SetGlobalInt(10, -1)
	if err := vm.Run(1); !errors.Is(err, ErrEdictIndex) {
		t.Errorf("got %v want ErrEdictIndex", err)
	}
}

func TestRun_LOAD_F_BadFieldOffset(t *testing.T) {
	vm, a := vmWithArena(withStatements(Statement{Op: OP_LOAD_F, A: 10, B: 11, C: 12}))
	e1, _ := a.Get(1)
	_ = vm.SetGlobalInt(10, a.PointerForEdict(e1))
	_ = vm.SetGlobalInt(11, 1<<20) // slot far past field block
	if err := vm.Run(1); !errors.Is(err, ErrFieldOffset) {
		t.Errorf("got %v want ErrFieldOffset", err)
	}
}

func TestRun_LOAD_V_BadFieldOffset(t *testing.T) {
	vm, a := vmWithArena(withStatements(Statement{Op: OP_LOAD_V, A: 10, B: 11, C: 30}))
	e1, _ := a.Get(1)
	_ = vm.SetGlobalInt(10, a.PointerForEdict(e1))
	_ = vm.SetGlobalInt(11, 1<<20)
	if err := vm.Run(1); !errors.Is(err, ErrFieldOffset) {
		t.Errorf("got %v want ErrFieldOffset", err)
	}
}

func TestRun_STOREP_F_BadPointer(t *testing.T) {
	vm, _ := vmWithArena(withStatements(Statement{Op: OP_STOREP_F, A: 10, B: 11}))
	_ = vm.SetGlobalInt(11, -1)
	if err := vm.Run(1); !errors.Is(err, ErrEdictIndex) {
		t.Errorf("got %v want ErrEdictIndex", err)
	}
}

func TestRun_STOREP_V_BadPointer(t *testing.T) {
	vm, _ := vmWithArena(withStatements(Statement{Op: OP_STOREP_V, A: 30, B: 11}))
	_ = vm.SetGlobalInt(11, -1)
	if err := vm.Run(1); !errors.Is(err, ErrEdictIndex) {
		t.Errorf("got %v want ErrEdictIndex", err)
	}
}

func TestRun_STOREP_F_MisalignedFieldOfs(t *testing.T) {
	vm, a := vmWithArena(withStatements(Statement{Op: OP_STOREP_F, A: 10, B: 11}))
	// Make a pointer that lands one byte into a field (not a 4-byte slot boundary).
	_ = vm.SetGlobalInt(11, a.MakePointer(1, 0)+1)
	if err := vm.Run(1); !errors.Is(err, ErrFieldOffset) {
		t.Errorf("got %v want ErrFieldOffset", err)
	}
}

func TestRun_STOREP_V_MisalignedFieldOfs(t *testing.T) {
	vm, a := vmWithArena(withStatements(Statement{Op: OP_STOREP_V, A: 30, B: 11}))
	_ = vm.SetGlobalInt(11, a.MakePointer(1, 0)+1)
	if err := vm.Run(1); !errors.Is(err, ErrFieldOffset) {
		t.Errorf("got %v want ErrFieldOffset", err)
	}
}

func TestRun_ADDRESS_BadPointer(t *testing.T) {
	vm, _ := vmWithArena(withStatements(Statement{Op: OP_ADDRESS, A: 10, B: 11, C: 12}))
	_ = vm.SetGlobalInt(10, -1)
	if err := vm.Run(1); !errors.Is(err, ErrEdictIndex) {
		t.Errorf("got %v want ErrEdictIndex", err)
	}
}

// --- Reset clears per-execution state, preserves wiring -------------------

func TestVM_Reset(t *testing.T) {
	// Drive the VM into a non-zero-depth state by calling
	// enterFunction without a matching leaveFunction (simulates the
	// posture after a Run() that errored mid-flight).
	vm := NewVM(progsForVM(nil))
	if err := vm.enterFunction(1); err != nil {
		t.Fatal(err)
	}
	if vm.Depth() == 0 {
		t.Fatal("setup: depth should be non-zero after enterFunction")
	}

	// Wire some bits that Reset MUST preserve: builtins, arena,
	// state hooks, and a globals write.
	vm.RegisterBuiltin(1, func(*VM) error { return nil })
	a := NewEdictArena(vm.progs, 4)
	vm.SetArena(a)
	vm.SetStateHooks(func() float32 { return 0 }, func() int32 { return 0 })
	vm.SetStateFieldOffsets(0, 1, 2)
	_ = vm.SetGlobalFloat(50, 7.5)

	vm.Reset()

	// Per-execution state cleared.
	if vm.Depth() != 0 {
		t.Errorf("Depth after Reset: got %d want 0", vm.Depth())
	}
	if vm.XFunction() != 0 {
		t.Errorf("XFunction after Reset: got %d want 0", vm.XFunction())
	}
	if vm.XStatement() != 0 {
		t.Errorf("XStatement after Reset: got %d want 0", vm.XStatement())
	}
	if vm.Argc() != 0 {
		t.Errorf("Argc after Reset: got %d want 0", vm.Argc())
	}

	// Wiring preserved.
	if vm.builtins[1] == nil {
		t.Error("Reset dropped builtins")
	}
	if vm.arena != a {
		t.Error("Reset dropped arena")
	}
	if vm.timeSource == nil || vm.selfEdict == nil || !vm.stateFieldsSet {
		t.Error("Reset dropped state hooks")
	}
	if v, _ := vm.GlobalFloat(50); v != 7.5 {
		t.Errorf("Reset clobbered globals: got %v want 7.5", v)
	}

	// After Reset the VM is usable again: a fresh Run works.
	p2 := progsForVM(withStatements(Statement{Op: OP_STORE_F, A: 10, B: 11}))
	vm2 := NewVM(p2)
	if err := vm2.enterFunction(1); err != nil {
		t.Fatal(err)
	}
	vm2.Reset()
	_ = vm2.SetGlobalFloat(10, 11)
	if err := vm2.Run(1); err != nil {
		t.Fatalf("Run after Reset: %v", err)
	}
	if v, _ := vm2.GlobalFloat(11); v != 11 {
		t.Errorf("Run after Reset: got %v want 11", v)
	}
}

// --- expose XFunction/XStatement/Depth/Globals accessors -------------------

func TestVM_Accessors(t *testing.T) {
	vm := NewVM(progsForVM(nil))
	if vm.Depth() != 0 || vm.XFunction() != 0 || vm.XStatement() != 0 {
		t.Errorf("initial state: %d %d %d", vm.Depth(), vm.XFunction(), vm.XStatement())
	}
	if len(vm.Globals()) != 256*globalSlotSize {
		t.Errorf("Globals len: %d", len(vm.Globals()))
	}
}

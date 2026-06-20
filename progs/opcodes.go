// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

// Op is the QuakeC bytecode opcode. tyrquake: enum in pr_comp.h
// (~62 opcodes). Each dstatement_t carries one op + three operands
// (a, b, c) whose meaning depends on the op.
type Op uint16

// QuakeC opcodes, verbatim from tyrquake/include/pr_comp.h.
//
//nolint:revive // OP_ prefix preserved for byte-equal trace parity with upstream debug dumps.
const (
	OP_DONE Op = iota
	OP_MUL_F
	OP_MUL_V
	OP_MUL_FV
	OP_MUL_VF
	OP_DIV_F
	OP_ADD_F
	OP_ADD_V
	OP_SUB_F
	OP_SUB_V

	OP_EQ_F
	OP_EQ_V
	OP_EQ_S
	OP_EQ_E
	OP_EQ_FNC

	OP_NE_F
	OP_NE_V
	OP_NE_S
	OP_NE_E
	OP_NE_FNC

	OP_LE
	OP_GE
	OP_LT
	OP_GT

	OP_LOAD_F
	OP_LOAD_V
	OP_LOAD_S
	OP_LOAD_ENT
	OP_LOAD_FLD
	OP_LOAD_FNC

	OP_ADDRESS

	OP_STORE_F
	OP_STORE_V
	OP_STORE_S
	OP_STORE_ENT
	OP_STORE_FLD
	OP_STORE_FNC

	OP_STOREP_F
	OP_STOREP_V
	OP_STOREP_S
	OP_STOREP_ENT
	OP_STOREP_FLD
	OP_STOREP_FNC

	OP_RETURN
	OP_NOT_F
	OP_NOT_V
	OP_NOT_S
	OP_NOT_ENT
	OP_NOT_FNC
	OP_IF
	OP_IFNOT
	OP_CALL0
	OP_CALL1
	OP_CALL2
	OP_CALL3
	OP_CALL4
	OP_CALL5
	OP_CALL6
	OP_CALL7
	OP_CALL8
	OP_STATE
	OP_GOTO
	OP_AND
	OP_OR

	OP_BITAND
	OP_BITOR
)

// Etype is the QuakeC type tag carried by every global / field /
// function-parameter slot. tyrquake: etype_t.
type Etype uint16

const (
	EvVoid     Etype = iota // ev_void
	EvString                // ev_string
	EvFloat                 // ev_float
	EvVector                // ev_vector (3x float)
	EvEntity                // ev_entity
	EvField                 // ev_field
	EvFunction              // ev_function
	EvPointer               // ev_pointer
)

// Fixed global-pool offsets used by every QuakeC function. The
// caller-side conventions: arg N goes into OfsParm0 + 3*N (each parm
// reserves 3 slots so it can carry a vec3); return value sits at
// OfsReturn. tyrquake: OFS_* macros in pr_comp.h.
const (
	OfsNull   = 0
	OfsReturn = 1
	OfsParm0  = 4
	OfsParm1  = 7
	OfsParm2  = 10
	OfsParm3  = 13
	OfsParm4  = 16
	OfsParm5  = 19
	OfsParm6  = 22
	OfsParm7  = 25
	OfsReserved = 28
)

// DefSaveGlobal is the bit set in a ddef_t.Type field when the value
// should persist into savegames. The interpreter strips it when
// switching on the underlying Etype. tyrquake: DEF_SAVEGLOBAL.
const DefSaveGlobal = 1 << 15

// MaxParms is the upper bound on a QuakeC function's parameter count.
// tyrquake: MAX_PARMS.
const MaxParms = 8

// ProgVersion is the on-disk version number expected in a progs.dat
// header. Both NetQuake and QuakeWorld use 6. tyrquake: PROG_VERSION.
const ProgVersion = 6

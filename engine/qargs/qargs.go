// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qargs

// MaxNumArgs is the upstream MAX_NUM_ARGVS cap. argv beyond this
// index is silently dropped, matching tyrquake's largv[] sizing.
const MaxNumArgs = 50

// safeModeReplacements is the literal switch sequence tyrquake
// appends when "-safe" is in argv. The names are intentional sentinels
// for the matching subsystems (sound, joy, mouse, cdaudio, stdout)
// to detect-and-skip; do NOT rename.
var safeModeReplacements = []string{
	"-stdvid", "-nolan", "-nosound", "-nocdaudio", "-nojoy", "-nomouse",
	"-dibonly",
}

// Registry holds the parsed argv plus the safe-mode expansion.
// tyrquake: com_argv + com_argc + safeargvs.
type Registry struct {
	args []string
}

// New returns an empty Registry. tyrquake call sequence is roughly
// `COM_InitArgv` then immediately consult; the Go form lets tests
// construct a Registry from a literal []string without re-implementing
// InitArgv's bookkeeping.
func New() *Registry { return &Registry{} }

// Init seeds the registry from argv. Capped at MaxNumArgs. If "-safe"
// is present, the safe-mode replacement switches are appended.
// tyrquake: COM_InitArgv.
func (r *Registry) Init(argv []string) {
	n := len(argv)
	if n > MaxNumArgs {
		n = MaxNumArgs
	}
	r.args = append(r.args[:0], argv[:n]...)
	if r.CheckParm("-safe") != 0 {
		for _, s := range safeModeReplacements {
			if len(r.args) >= MaxNumArgs {
				break
			}
			r.args = append(r.args, s)
		}
	}
}

// Argc returns the number of arguments. tyrquake: com_argc / COM_Argc.
func (r *Registry) Argc() int { return len(r.args) }

// Argv returns the n-th argument, or "" when n is out of range.
// tyrquake: COM_Argv (which returns the empty-string sentinel
// argvdummy when n is out of bounds).
func (r *Registry) Argv(n int) string {
	if n < 0 || n >= len(r.args) {
		return ""
	}
	return r.args[n]
}

// CheckParm returns the 1-based index of the first arg matching parm,
// or 0 when not found. The scan starts at index 1 (argv[0] is the
// executable name and is never matched). tyrquake: COM_CheckParm.
func (r *Registry) CheckParm(parm string) int {
	for i := 1; i < len(r.args); i++ {
		if r.args[i] == parm {
			return i
		}
	}
	return 0
}

// AddParm appends parm to the registry. Silently drops the addition
// when MaxNumArgs would be exceeded. tyrquake: COM_AddParm.
func (r *Registry) AddParm(parm string) {
	if len(r.args) >= MaxNumArgs {
		return
	}
	r.args = append(r.args, parm)
}

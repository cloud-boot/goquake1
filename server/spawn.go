// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"fmt"
	"math"

	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/sizebuf"
)

// MapBSPPath returns the path the worldmodel BSP lives at for a
// given map slug -- "maps/<name>.bsp". tyrquake: the qsnprintf
// inside SV_SpawnServer that writes sv.modelname.
//
// Returns ErrEmptyMapName when name is empty (the upstream's
// implicit reliance on hostname being non-empty turns into an
// explicit guard here).
func MapBSPPath(name string) (string, error) {
	if name == "" {
		return "", ErrEmptyMapName
	}
	return "maps/" + name + ".bsp", nil
}

// ErrEmptyMapName fires when callers try to spawn a map with an
// empty slug -- the C upstream would format "maps/.bsp", read it
// as missing, then SV_SendDisconnect. The Go port surfaces it
// earlier.
var ErrEmptyMapName = errors.New("server: empty map name")

// ClampSkill takes a raw skill value (the `skill` cvar's float
// reading) and returns the int skill level in [0, 3] the rest of
// the engine + QC expects. tyrquake: the three-line clamp block
// inside SV_SpawnServer (`current_skill = (int)(skill.value + 0.5);
// if (current_skill < 0) ...; if (current_skill > 3) ...`).
func ClampSkill(value float32) int {
	s := int(math.Round(float64(value)))
	switch {
	case s < 0:
		return 0
	case s > 3:
		return 3
	default:
		return s
	}
}

// LocalModelName returns the QuakeC submodel reference string for a
// given submodel index -- "*1", "*2", ..., up to "*MAX_MODELS-1".
// tyrquake: the localmodels[MAX_MODELS][MODSTRLEN] table populated
// in SV_Init via `qsnprintf(localmodels[i], ..., "*%i", i)`.
//
// idx 0 is the world model and traditionally uses the literal map
// name ("maps/<name>.bsp"), NOT "*0"; callers shouldn't pass 0 here
// but the function returns "*0" anyway for completeness (the
// upstream table has a "*0" entry that's just never read).
//
// Returns ErrLocalModelIndex on idx < 0 or idx >= [MaxModels].
func LocalModelName(idx int) (string, error) {
	if idx < 0 || idx >= MaxModels {
		return "", fmt.Errorf("%w: idx=%d outside [0, %d)", ErrLocalModelIndex, idx, MaxModels)
	}
	return fmt.Sprintf("*%d", idx), nil
}

// ErrLocalModelIndex fires when LocalModelName is given an out-of-
// range index. The C upstream wouldn't bounds-check; the Go port
// guards because LocalModelName is a public helper.
var ErrLocalModelIndex = errors.New("server: local model index out of range")

// Reset clears the Server's per-map state in place: precaches and
// buffers go back to empty, Edicts is re-allocated to MaxEdicts
// slots (with each slot populated by a fresh [progs.Edict]), Name
// and ModelName are set from server, and State drops to StateLoading.
// tyrquake: the memset(&sv, 0, sizeof(sv)) + strcpy(sv.name, server)
// + buffer-pointer-restore block at the top of SV_SpawnServer.
//
// Reset does NOT touch the [WorldModel] field -- the caller is
// responsible for the Mod_ForName-equivalent lookup and assignment.
// It also does NOT touch [Static] (the cross-map state); use
// Static.ResetServerSlots separately if needed.
//
// Returns [ErrEmptyMapName] if name is empty. protocol must be one
// of the protocol.Version* constants; Reset does not validate it
// (the constructor / config layer is responsible).
func (s *Server) Reset(name string, protocol int) error {
	if name == "" {
		return ErrEmptyMapName
	}
	// MapBSPPath only fails on empty name (guarded above), so the
	// error return here is unreachable in Reset's input domain.
	// Same posture as bsptrace's backoff loop -- inherited C
	// defensive code dropped on the Go side.
	bspPath, _ := MapBSPPath(name)

	// Wipe precaches by re-zeroing the existing slices in place;
	// re-allocating would defeat NewServer's pool preservation.
	for i := range s.ModelPrecache {
		s.ModelPrecache[i] = ""
	}
	for i := range s.Models {
		s.Models[i] = nil
	}
	for i := range s.SoundPrecache {
		s.SoundPrecache[i] = ""
	}
	for i := range s.LightStyles {
		s.LightStyles[i] = ""
	}

	// Wipe buffers (cursize back to 0, capacity preserved).
	if s.Datagram == nil {
		s.Datagram = sizebuf.New(make([]byte, MaxDatagram))
	} else {
		s.Datagram.Clear()
	}
	if s.ReliableDatagram == nil {
		s.ReliableDatagram = sizebuf.New(make([]byte, MaxMsgLen))
	} else {
		s.ReliableDatagram.Clear()
	}
	if s.Signon == nil {
		s.Signon = sizebuf.New(make([]byte, MaxMsgLen))
	} else {
		s.Signon.Clear()
	}

	// (Re-)allocate the entity pool.
	if s.MaxEdicts == 0 {
		s.MaxEdicts = MaxEdicts
	}
	if cap(s.Edicts) < s.MaxEdicts {
		s.Edicts = make([]*progs.Edict, s.MaxEdicts)
	} else {
		s.Edicts = s.Edicts[:s.MaxEdicts]
		for i := range s.Edicts {
			s.Edicts[i] = nil
		}
	}
	s.NumEdicts = 0

	// Per-map scalars.
	s.Name = name
	s.ModelName = bspPath
	s.Protocol = protocol
	s.State = StateLoading
	s.Active = false
	s.Paused = false
	s.LoadGame = false
	s.Time = 0
	s.LastCheck = 0
	s.LastCheckTime = 0
	s.WorldModel = nil

	return nil
}

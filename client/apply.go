// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package client

import (
	"errors"
	"fmt"
)

// ErrApplyNilState is returned by [Apply] when state == nil.
var ErrApplyNilState = errors.New("client.Apply: nil state")

// ErrApplyNilMessage is returned by [Apply] when msg == nil.
var ErrApplyNilMessage = errors.New("client.Apply: nil message")

// ErrApplyUnknown is returned by [Apply] when the message's runtime
// type is not one of the [Decoded] variants Apply knows about. It is
// a forward-compatibility guard: if a new svc_* decoder ships a new
// Decoded* shape and the matching Apply arm is forgotten, callers
// see this sentinel instead of a silent no-op.
var ErrApplyUnknown = errors.New("client.Apply: unknown decoded message type")

// ErrApplyBadState is returned by [Apply] when a lifecycle message
// (DecodedSignonNum stage 4, DecodedDisconnect) maps to a state
// transition that the [State] helper rejects (e.g. MarkSpawned when
// not in StateConnecting). The underlying [State] sentinel
// ([ErrAlreadyConnected] / [ErrNotConnecting]) is wrapped so callers
// can do both [errors.Is](err, ErrApplyBadState) and
// [errors.Unwrap](err) to recover it.
var ErrApplyBadState = errors.New("client.Apply: state transition rejected")

// applyBadStateErr wraps an underlying State.* sentinel so that
//
//	errors.Is(err, ErrApplyBadState)         == true
//	errors.Is(err, ErrNotConnecting)         == true (when underlying)
//	errors.Unwrap(err)                       == the underlying sentinel
//
// both succeed. The wrapper is purely internal -- callers use the
// errors.Is / errors.Unwrap protocol to interrogate.
type applyBadStateErr struct{ underlying error }

func (e *applyBadStateErr) Error() string {
	return fmt.Sprintf("%s: %v", ErrApplyBadState.Error(), e.underlying)
}

// Is matches ErrApplyBadState (the category sentinel) so the wrapped
// error can be inspected without an Unwrap dance.
func (e *applyBadStateErr) Is(target error) bool { return target == ErrApplyBadState }

// Unwrap returns the underlying State sentinel so errors.Is also
// matches the specific transition error (ErrNotConnecting /
// ErrAlreadyConnected).
func (e *applyBadStateErr) Unwrap() error { return e.underlying }

// Apply mutates state to reflect the decoded message. Each Decoded*
// type drives a specific state mutation (precache fill from
// DecodedServerInfo, player stat write from DecodedClientData,
// lifecycle transition from DecodedSignonNum, ...).
//
// tyrquake: the per-svc_* mutation arms inside CL_ParseServerMessage
// (NQ/cl_parse.c). Apply is the Go-side dispatcher that pairs each
// Decoded* shape with its mutation, factored out of the wire layer
// so the decoder stays pure.
//
// nowSec is the current wall-clock-like server time -- used for
// MsgTime updates on every message (the C upstream's cl.mtime[0]
// update inside CL_ParseServerMessage's main loop). MsgTime is
// updated on EVERY call, including the no-op arms.
//
// Returns:
//
//   - nil                  on success
//   - ErrApplyNilState     if state == nil
//   - ErrApplyNilMessage   if msg == nil
//   - ErrApplyUnknown      if msg's runtime type is not a known
//     Decoded* variant
//   - ErrApplyBadState     wrapping the underlying State.* sentinel
//     when a lifecycle transition is rejected
func Apply(state *State, msg Decoded, nowSec float32) error {
	if state == nil {
		return ErrApplyNilState
	}
	if msg == nil {
		return ErrApplyNilMessage
	}

	// Always-on: the upstream's per-message cl.mtime[0] update.
	state.MsgTime = nowSec

	switch m := msg.(type) {
	case DecodedServerInfo:
		return applyServerInfo(state, m)
	case DecodedSignonNum:
		return applySignonNum(state, m)
	case DecodedDisconnect:
		state.Disconnect()
		return nil
	case DecodedSetView:
		state.PlayerNum = m.EntityNum
		return nil
	case DecodedClientData:
		applyClientData(state, m)
		return nil
	case DecodedUpdateStat:
		applyUpdateStat(state, m)
		return nil
	case DecodedUpdateName:
		// TODO: add Names[16]string once scoreboard rendering lands.
		// For now the message is intentionally acknowledged and
		// discarded; the wire layer has already validated the body.
		return nil
	case DecodedUpdateColors:
		// TODO: add Colors[16]int once scoreboard rendering lands.
		// Upstream cl.scores[i].colors has no [State] field yet;
		// intentional acknowledged-and-discarded.
		return nil
	case DecodedUpdateFrags:
		applyUpdateFrags(state, m)
		return nil
	case DecodedBaseline:
		applyBaseline(state, m)
		return nil
	case DecodedNop,
		DecodedPrint,
		DecodedStuffText,
		DecodedFinale,
		DecodedCutscene,
		DecodedSellScreen,
		DecodedKilledMonster,
		DecodedFoundSecret,
		DecodedParticle,
		DecodedSound,
		DecodedUpdate:
		// Documented no-op arms:
		//   - Nop:                       connection-alive heartbeat
		//   - Print / StuffText:         renderer/UI + console concern
		//   - Finale / Cutscene / SellScreen: UI-state transitions
		//   - KilledMonster / FoundSecret:    gameplay sound triggers
		//   - Particle / Sound:          particle pool + sound mixer (separate layers)
		//   - Update:                    per-tic delta cache (separate layer)
		return nil
	}
	return fmt.Errorf("%w: %T", ErrApplyUnknown, msg)
}

// applyServerInfo handles svc_serverinfo: fill precaches + level
// banner, clear local-player slot. The SetConnecting transition
// itself happens in the caller's establish-connection path; Apply
// must NOT touch state.Connection here. tyrquake: CL_ParseServerInfo.
func applyServerInfo(state *State, m DecodedServerInfo) error {
	state.MapName = m.LevelName
	state.LevelName = m.LevelName
	state.ModelPrecache = append([]string(nil), m.ModelPrecache...)
	state.SoundPrecache = append([]string(nil), m.SoundPrecache...)
	state.PlayerNum = 0
	return nil
}

// applySignonNum handles the signon-handshake stage byte. Stage 1
// is the wire-driven equivalent of CL_EstablishConnection: when the
// client sees the first signonnum byte from a server, it knows the
// handshake has started and transitions itself into [StateConnecting]
// (a no-op when already past Disconnected, e.g. when the caller pre-
// drove the transition via [State.SetConnecting]). Stages 2 and 3
// are caller-side handshake markers Apply just acknowledges. Stage 4
// (post-spawn signon) drives the final transition to [StateConnected]
// via [State.MarkSpawned]. tyrquake: CL_SignonReply -- the upstream
// C engine sets cls.state = ca_connected in CL_EstablishConnection
// BEFORE any wire bytes arrive; the Go port collapses that into the
// stage-1 handler so a server emitting svc_signonnum(1) drives the
// client's lifecycle directly, with no caller-side pre-step needed.
func applySignonNum(state *State, m DecodedSignonNum) error {
	switch m.Stage {
	case 1:
		// Wire-driven establish: bring an undriven (StateDisconnected)
		// state into StateConnecting so the upcoming stage-4 byte can
		// MarkSpawned cleanly. Already-connecting / already-connected
		// states are left untouched -- this is idempotent on retransmit.
		if state.Connection == StateDisconnected {
			// SetConnecting's only failure path is "not in
			// StateDisconnected"; the guard above rules it out, so the
			// error return is structurally unreachable here.
			_ = state.SetConnecting()
		}
		return nil
	case 4:
		if err := state.MarkSpawned(); err != nil {
			return &applyBadStateErr{underlying: err}
		}
		return nil
	}
	// Stages 2 + 3 are no-op acknowledgements -- the C upstream uses
	// them as triggers for outbound clc_stringcmd commands (prespawn /
	// spawn), which the Go port doesn't yet emit.
	return nil
}

// applyClientData handles svc_clientdata: copy the per-tic player
// snapshot into the matching State fields. tyrquake:
// CL_ParseClientdata.
func applyClientData(state *State, m DecodedClientData) {
	state.ViewHeightOffset = m.ViewHeightOffset
	state.IdealPitch = m.IdealPitch
	state.PunchAngle = m.PunchAngle
	state.Velocity = m.Velocity
	state.Health = m.Health
	state.Items = m.Items
	state.Ammo = [4]int{m.Ammo[0], m.Ammo[1], m.Ammo[2], m.Ammo[3]}
	state.CurrentAmmo = m.CurrentAmmo
	state.OnGround = m.OnGround
	state.InWater = m.InWater
}

// applyUpdateStat handles svc_updatestat: write into Stats[Stat] if
// in range, silently skip otherwise. The upstream Sys_Error's on
// out-of-range stat ids; the Go port treats unknown stat ids as
// future-extension data and ignores them.
func applyUpdateStat(state *State, m DecodedUpdateStat) {
	if m.Stat < 0 || m.Stat >= len(state.Stats) {
		return
	}
	state.Stats[m.Stat] = m.Value
}

// applyUpdateFrags handles svc_updatefrags: write into Frags[Slot]
// if in range, silently skip otherwise.
func applyUpdateFrags(state *State, m DecodedUpdateFrags) {
	if m.Slot < 0 || m.Slot >= len(state.Frags) {
		return
	}
	state.Frags[m.Slot] = m.Frags
}

// applyBaseline handles svc_spawnbaseline: cache the per-entity
// snapshot into [State.Baselines] keyed by EntityNum so per-tic
// svc_update deltas (a follow-up batch) can resolve their omitted
// fields against the entity's last-known-good state. Allocates the
// map lazily so callers that constructed a State without going through
// [NewState] don't crash on the first arm.
// tyrquake: CL_ParseBaseline -- the per-entity body that copies the
// decoded entity_state_t into cl_entities[entnum].baseline.
func applyBaseline(state *State, m DecodedBaseline) {
	if state.Baselines == nil {
		state.Baselines = make(map[int]EntityBaseline)
	}
	state.Baselines[m.EntityNum] = EntityBaseline{
		ModelIdx: m.ModelIdx,
		Frame:    m.Frame,
		ColorMap: m.ColorMap,
		SkinNum:  m.SkinNum,
		Origin:   m.Origin,
		Angles:   m.Angles,
		Alpha:    m.Alpha,
	}
}

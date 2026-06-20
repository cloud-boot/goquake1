// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"testing"

	"github.com/go-quake1/engine/server"
)

const pusherKeyTest Key = 1

// Empty riders: the pusher's origin advances and NewRiderOrigins is
// a (non-nil) empty slice so the caller can index into it without an
// extra "if len == 0" guard.
func TestPushMove_EmptyRiders(t *testing.T) {
	out, err := PushMove(
		pusherKeyTest,
		[3]float32{0, 0, 0},
		[3]float32{-16, -16, -16},
		[3]float32{16, 16, 16},
		[3]float32{5, 0, 0},
		nil,
		makeEmptyWorld(),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.NewPusherOrigin != ([3]float32{5, 0, 0}) {
		t.Errorf("NewPusherOrigin: got %v want (5,0,0)", out.NewPusherOrigin)
	}
	if len(out.NewRiderOrigins) != 0 {
		t.Errorf("NewRiderOrigins: got %v want []", out.NewRiderOrigins)
	}
	if out.Blocked {
		t.Error("Blocked should be false")
	}
	if out.BlockedRider != -1 {
		t.Errorf("BlockedRider: got %d want -1", out.BlockedRider)
	}
}

// A rider standing on the pusher (GroundKey == pusher AND FlagOnGround
// set) is moved UNCONDITIONALLY by pushMove, even if its bounds don't
// overlap the pusher's new absbounds. We place the rider 100 units
// above the pusher so the bounds-overlap test would say "no" -- the
// rider still moves because the "riding" predicate short-circuits the
// overlap check.
func TestPushMove_RiderStandingOnPusher(t *testing.T) {
	riders := []PushMoveRider{{
		Key:       2,
		Origin:    [3]float32{0, 0, 100},
		Mins:      [3]float32{-1, -1, -1},
		Maxs:      [3]float32{1, 1, 1},
		Solid:     server.SolidBBox,
		MoveType:  server.MoveTypeWalk,
		Flags:     int32(server.FlagOnGround),
		GroundKey: pusherKeyTest,
	}}
	out, err := PushMove(
		pusherKeyTest,
		[3]float32{0, 0, 0},
		[3]float32{-16, -16, -16},
		[3]float32{16, 16, 16},
		[3]float32{0, 0, 5},
		riders,
		makeEmptyWorld(),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := [3]float32{0, 0, 105}
	if out.NewRiderOrigins[0] != want {
		t.Errorf("rider position: got %v want %v", out.NewRiderOrigins[0], want)
	}
	if out.Blocked {
		t.Error("clean push should not block")
	}
}

// Rider's GroundKey matches the pusher but FlagOnGround is NOT set:
// the riding short-circuit must NOT trigger. With the rider far above
// the pusher's new absbounds, the overlap test rules "no" and the
// rider stays put. This exercises the "GroundKey match but no
// FlagOnGround" branch of the riding predicate.
func TestPushMove_GroundKeyMatchButNoFlagOnGround(t *testing.T) {
	riders := []PushMoveRider{{
		Key:       2,
		Origin:    [3]float32{0, 0, 100},
		Mins:      [3]float32{-1, -1, -1},
		Maxs:      [3]float32{1, 1, 1},
		Solid:     server.SolidBBox,
		MoveType:  server.MoveTypeWalk,
		Flags:     0, // FlagOnGround missing
		GroundKey: pusherKeyTest,
	}}
	out, err := PushMove(
		pusherKeyTest,
		[3]float32{0, 0, 0},
		[3]float32{-16, -16, -16},
		[3]float32{16, 16, 16},
		[3]float32{0, 0, 5},
		riders,
		makeEmptyWorld(),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// rider far above the pusher's NEW absbounds (z up to 21) -> no overlap -> skipped
	if out.NewRiderOrigins[0] != ([3]float32{0, 0, 100}) {
		t.Errorf("rider should NOT have moved: got %v", out.NewRiderOrigins[0])
	}
}

// A rider that's NOT riding the pusher but whose bounds overlap the
// pusher's NEW absbounds gets pushed by pushMove. The rider sits at
// (10,0,0) with +-2 bounds; pusher moves from origin (0,0,0) by
// (+5,0,0) so its new absbounds become roughly (-11..21) on x --
// rider's (8..12) box is well inside -> overlap -> pushed.
func TestPushMove_RiderInOverlapZone(t *testing.T) {
	riders := []PushMoveRider{{
		Key:    2,
		Origin: [3]float32{10, 0, 0},
		// Point bounds keep HullForBounds on hull[0] (the only hull
		// the empty-world brushmodel populates). The "overlapping the
		// pusher's new absbounds" test still triggers: rider absmin =
		// (10,0,0), pusher new absmax = (21,16,16), so the strict
		// disjoint predicate fails on every axis -> overlap.
		Mins:     [3]float32{0, 0, 0},
		Maxs:     [3]float32{0, 0, 0},
		Solid:    server.SolidBBox,
		MoveType: server.MoveTypeWalk,
		// no riding: GroundKey != pusherKey, FlagOnGround clear
	}}
	out, err := PushMove(
		pusherKeyTest,
		[3]float32{0, 0, 0},
		[3]float32{-16, -16, -16},
		[3]float32{16, 16, 16},
		[3]float32{5, 0, 0},
		riders,
		makeEmptyWorld(),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := [3]float32{15, 0, 0}
	if out.NewRiderOrigins[0] != want {
		t.Errorf("overlap rider should be pushed: got %v want %v", out.NewRiderOrigins[0], want)
	}
}

// A rider whose bounds are JUST outside the pusher's new absbounds
// (touching edges count as non-overlap because the C upstream uses
// the strict >= / <= disjoint predicate) is NOT pushed.
//
// Pusher new absmax x = 0 + 5 + 16 = 21; rider absmin x = 21 + 0 = 21.
// riderAbsMin[0] >= pusherAbsMax[0] (21 >= 21) -> disjoint -> skipped.
func TestPushMove_RiderTouchingEdgeNotPushed(t *testing.T) {
	riders := []PushMoveRider{{
		Key:      2,
		Origin:   [3]float32{21, 0, 0},
		Mins:     [3]float32{0, 0, 0},
		Maxs:     [3]float32{2, 2, 2},
		Solid:    server.SolidBBox,
		MoveType: server.MoveTypeWalk,
	}}
	out, err := PushMove(
		pusherKeyTest,
		[3]float32{0, 0, 0},
		[3]float32{-16, -16, -16},
		[3]float32{16, 16, 16},
		[3]float32{5, 0, 0},
		riders,
		makeEmptyWorld(),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.NewRiderOrigins[0] != ([3]float32{21, 0, 0}) {
		t.Errorf("touching-edge rider should NOT move: got %v", out.NewRiderOrigins[0])
	}
}

// Solid == SolidNot: skipped at the top of the loop; NewRiderOrigins
// holds the pre-move origin unchanged.
func TestPushMove_SkipSolidNot(t *testing.T) {
	riders := []PushMoveRider{{
		Key:       2,
		Origin:    [3]float32{0, 0, 100},
		Mins:      [3]float32{-1, -1, -1},
		Maxs:      [3]float32{1, 1, 1},
		Solid:     server.SolidNot,
		MoveType:  server.MoveTypeWalk,
		Flags:     int32(server.FlagOnGround),
		GroundKey: pusherKeyTest, // would otherwise be "riding"
	}}
	out, err := PushMove(
		pusherKeyTest,
		[3]float32{0, 0, 0},
		[3]float32{-16, -16, -16},
		[3]float32{16, 16, 16},
		[3]float32{0, 0, 5},
		riders,
		makeEmptyWorld(),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.NewRiderOrigins[0] != ([3]float32{0, 0, 100}) {
		t.Errorf("SolidNot rider should NOT move: got %v", out.NewRiderOrigins[0])
	}
}

// MoveType None / NoClip / Push: each skipped at the top of the loop.
// Drives all three branches of the MoveType-skip condition.
func TestPushMove_SkipMoveTypes(t *testing.T) {
	cases := []struct {
		name string
		mt   server.MoveType
	}{
		{"None", server.MoveTypeNone},
		{"NoClip", server.MoveTypeNoClip},
		{"Push", server.MoveTypePush},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			riders := []PushMoveRider{{
				Key:       2,
				Origin:    [3]float32{0, 0, 100},
				Mins:      [3]float32{-1, -1, -1},
				Maxs:      [3]float32{1, 1, 1},
				Solid:     server.SolidBBox,
				MoveType:  tc.mt,
				Flags:     int32(server.FlagOnGround),
				GroundKey: pusherKeyTest, // would otherwise ride
			}}
			out, err := PushMove(
				pusherKeyTest,
				[3]float32{0, 0, 0},
				[3]float32{-16, -16, -16},
				[3]float32{16, 16, 16},
				[3]float32{0, 0, 5},
				riders,
				makeEmptyWorld(),
			)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if out.NewRiderOrigins[0] != ([3]float32{0, 0, 100}) {
				t.Errorf("%s rider should NOT move: got %v", tc.name, out.NewRiderOrigins[0])
			}
		})
	}
}

// Rider blocked by a world wall: pushMove drives the rider straight
// into the +x wall built by [makeWorldWithWall]; PushEntity reports
// Fraction < 1 -> Blocked, BlockedRider = 0.
func TestPushMove_BlockedByWorldWall(t *testing.T) {
	riders := []PushMoveRider{{
		Key:       2,
		Origin:    [3]float32{-5, 0, 0}, // just west of the wall at x=0
		Mins:      [3]float32{0, 0, 0},
		Maxs:      [3]float32{0, 0, 0},
		Solid:     server.SolidBBox,
		MoveType:  server.MoveTypeWalk,
		Flags:     int32(server.FlagOnGround),
		GroundKey: pusherKeyTest, // riding -> processed
	}}
	out, err := PushMove(
		pusherKeyTest,
		[3]float32{-10, 0, 0},
		[3]float32{-16, -16, -16},
		[3]float32{16, 16, 16},
		[3]float32{20, 0, 0}, // pusher (and rider) move +20 -> rider would land at x=15, past the wall
		riders,
		makeWorldWithWall(),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Blocked {
		t.Error("expected Blocked=true (rider hits wall)")
	}
	if out.BlockedRider != 0 {
		t.Errorf("BlockedRider: got %d want 0", out.BlockedRider)
	}
	// Pusher's NEW origin is still reported (the caller decides how
	// to roll back).
	if out.NewPusherOrigin != ([3]float32{10, 0, 0}) {
		t.Errorf("NewPusherOrigin: got %v want (10,0,0)", out.NewPusherOrigin)
	}
}

// Multiple riders, the SECOND one blocks. After the blocker, later
// riders keep their pre-move Origin in NewRiderOrigins (the early
// return doesn't process them). The first rider was processed
// successfully so its entry reflects the post-push position.
func TestPushMove_MultipleRidersOneBlocks(t *testing.T) {
	riders := []PushMoveRider{
		// Rider 0: rides cleanly through empty space.
		{
			Key:       2,
			Origin:    [3]float32{-50, 0, 0},
			Mins:      [3]float32{0, 0, 0},
			Maxs:      [3]float32{0, 0, 0},
			Solid:     server.SolidBBox,
			MoveType:  server.MoveTypeWalk,
			Flags:     int32(server.FlagOnGround),
			GroundKey: pusherKeyTest,
		},
		// Rider 1: gets driven into the +x wall -> blocks.
		{
			Key:       3,
			Origin:    [3]float32{-5, 0, 0},
			Mins:      [3]float32{0, 0, 0},
			Maxs:      [3]float32{0, 0, 0},
			Solid:     server.SolidBBox,
			MoveType:  server.MoveTypeWalk,
			Flags:     int32(server.FlagOnGround),
			GroundKey: pusherKeyTest,
		},
		// Rider 2: never reached.
		{
			Key:       4,
			Origin:    [3]float32{99, 99, 99},
			Mins:      [3]float32{-1, -1, -1},
			Maxs:      [3]float32{1, 1, 1},
			Solid:     server.SolidBBox,
			MoveType:  server.MoveTypeWalk,
			Flags:     int32(server.FlagOnGround),
			GroundKey: pusherKeyTest,
		},
	}
	out, err := PushMove(
		pusherKeyTest,
		[3]float32{-100, 0, 0},
		[3]float32{-16, -16, -16},
		[3]float32{16, 16, 16},
		[3]float32{20, 0, 0},
		riders,
		makeWorldWithWall(),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Blocked || out.BlockedRider != 1 {
		t.Errorf("Blocked=%v BlockedRider=%d want true / 1", out.Blocked, out.BlockedRider)
	}
	// Rider 0 was successfully pushed by (+20,0,0) -> (-30,0,0).
	if out.NewRiderOrigins[0] != ([3]float32{-30, 0, 0}) {
		t.Errorf("rider 0: got %v want (-30,0,0)", out.NewRiderOrigins[0])
	}
	// Rider 2 was never processed -> still at its pre-move Origin.
	if out.NewRiderOrigins[2] != ([3]float32{99, 99, 99}) {
		t.Errorf("rider 2 (post-blocker) should be unchanged: got %v", out.NewRiderOrigins[2])
	}
}

// Two riders, the OTHER rider is SolidNot: buildRiderCandidates drops
// it from the candidate list so it never blocks the moving rider's
// PushEntity trace. This drives the `r.Solid == SolidNot` branch of
// buildRiderCandidates.
//
// Rider 0 sits at x=-10 with FlagOnGround riding the pusher; pushed
// (+10,0,0) toward x=0. Rider 1 is right at x=0 but SolidNot -- it
// would clip if included. Expect rider 0 to move cleanly to (0,0,0).
func TestPushMove_SolidNotOtherRiderDropped(t *testing.T) {
	riders := []PushMoveRider{
		{
			Key:       2,
			Origin:    [3]float32{-10, 0, 0},
			Mins:      [3]float32{0, 0, 0},
			Maxs:      [3]float32{0, 0, 0},
			Solid:     server.SolidBBox,
			MoveType:  server.MoveTypeWalk,
			Flags:     int32(server.FlagOnGround),
			GroundKey: pusherKeyTest,
		},
		{
			Key:      3,
			Origin:   [3]float32{0, 0, 0},
			Mins:     [3]float32{-5, -5, -5},
			Maxs:     [3]float32{5, 5, 5},
			Solid:    server.SolidNot, // dropped from candidates
			MoveType: server.MoveTypeNone,
		},
	}
	out, err := PushMove(
		pusherKeyTest,
		[3]float32{-20, 0, 0},
		[3]float32{-16, -16, -16},
		[3]float32{16, 16, 16},
		[3]float32{10, 0, 0},
		riders,
		makeEmptyWorld(),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Blocked {
		t.Error("SolidNot other-rider should not block")
	}
	if out.NewRiderOrigins[0] != ([3]float32{0, 0, 0}) {
		t.Errorf("rider 0: got %v want (0,0,0)", out.NewRiderOrigins[0])
	}
	if out.NewRiderOrigins[1] != ([3]float32{0, 0, 0}) {
		t.Errorf("rider 1 (skipped): got %v want (0,0,0)", out.NewRiderOrigins[1])
	}
}

// Two SOLID riders where the OTHER rider is a wall-in-the-path: the
// moving rider's PushEntity trace clips against that other rider and
// reports a non-clean trace -> blocked. Confirms the buildRiderCandidates
// happy path (rider 1 is INCLUDED as a candidate for rider 0's push).
func TestPushMove_BlockedByOtherRider(t *testing.T) {
	riders := []PushMoveRider{
		{
			Key:       2,
			Origin:    [3]float32{-50, 0, 0},
			Mins:      [3]float32{0, 0, 0},
			Maxs:      [3]float32{0, 0, 0},
			Solid:     server.SolidBBox,
			MoveType:  server.MoveTypeWalk,
			Flags:     int32(server.FlagOnGround),
			GroundKey: pusherKeyTest,
		},
		{
			Key:      3,
			Origin:   [3]float32{0, 0, 0}, // squarely in rider 0's push path
			Mins:     [3]float32{-10, -10, -10},
			Maxs:     [3]float32{10, 10, 10},
			Solid:    server.SolidBBox,
			MoveType: server.MoveTypeNone, // doesn't ride; just a wall
		},
	}
	out, err := PushMove(
		pusherKeyTest,
		[3]float32{-100, 0, 0},
		[3]float32{-16, -16, -16},
		[3]float32{16, 16, 16},
		[3]float32{60, 0, 0},
		riders,
		makeEmptyWorld(),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Blocked {
		t.Error("expected Blocked=true (rider 0 hits rider 1)")
	}
	if out.BlockedRider != 0 {
		t.Errorf("BlockedRider: got %d want 0", out.BlockedRider)
	}
}

// Error path: PushEntity propagates a TraceMove error from a corrupt
// world hull. PushMove must surface that error and return a zero
// PushMoveOut (no partial state).
func TestPushMove_PushEntityErrorPropagates(t *testing.T) {
	riders := []PushMoveRider{{
		Key:       2,
		Origin:    [3]float32{0, 0, 0},
		Mins:      [3]float32{0, 0, 0},
		Maxs:      [3]float32{0, 0, 0},
		Solid:     server.SolidBBox,
		MoveType:  server.MoveTypeWalk,
		Flags:     int32(server.FlagOnGround),
		GroundKey: pusherKeyTest,
	}}
	out, err := PushMove(
		pusherKeyTest,
		[3]float32{0, 0, 0},
		[3]float32{-16, -16, -16},
		[3]float32{16, 16, 16},
		[3]float32{5, 0, 0},
		riders,
		pushEntityCorruptWorld(),
	)
	if err == nil {
		t.Fatal("expected error from corrupt world hull")
	}
	if out.Blocked || out.BlockedRider != 0 || out.NewPusherOrigin != ([3]float32{}) {
		t.Errorf("on error, expected zero PushMoveOut: got %+v", out)
	}
	if out.NewRiderOrigins != nil {
		t.Errorf("on error, NewRiderOrigins should be nil: got %v", out.NewRiderOrigins)
	}
}

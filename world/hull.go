// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package world

import (
	"errors"

	"github.com/go-quake1/engine/boxhull"
	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/bsptrace"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/server"
)

// Target identifies the entity the trace is clipping AGAINST. Callers
// build it from a progs.Edict's bounds + origin + solid + (for
// SOLID_BSP) the BrushModel sv.models[modelindex] points at.
//
// The shape is what the server glue layer extracts from each
// candidate entity during a trace; world doesn't reach back into
// progs to fetch fields.
type Target struct {
	Origin [3]float32
	Mins   [3]float32 // local mins (relative to Origin)
	Maxs   [3]float32 // local maxs (relative to Origin)
	Solid  server.Solid
	// BrushModel is required iff Solid == SolidBSP. Pick it via
	// sv.models[ent.v.modelindex] in the server glue layer.
	BrushModel *model.BrushModel
}

// ErrSolidBSPNeedsBrushModel is returned by HullForBounds when a
// SOLID_BSP target arrives without a BrushModel attached -- caller's
// modelindex lookup failed. The C upstream SV_Errors on the
// equivalent path ("MOVETYPE_PUSH with a non bsp model").
var ErrSolidBSPNeedsBrushModel = errors.New("world: SOLID_BSP target requires Target.BrushModel")

// HullForBounds returns the collision hull (and origin offset) the
// trace walker should use to clip the (mins, maxs)-sized test object
// against target. tyrquake: SV_HullForEntity.
//
// SOLID_BSP picks one of target.BrushModel.Hulls[0..2] by the test
// object's size on the X axis:
//
//   - size[0] < 3       -> hulls[0] (the BSP draw tree)
//   - size[0] <= 32     -> hulls[1] (player size: -16,-16,-24 .. 16,16,32)
//   - size[0] > 32      -> hulls[2] (monster size: -32,-32,-24 .. 32,32,64)
//
// The offset translates the test object's coordinates into the
// hull's local frame: offset = hull.ClipMins - mins + target.Origin.
//
// Every other Solid* value (Not / Trigger / BBox / SlideBox) builds
// a temp boxhull from the Minkowski-difference bounds:
//
//	hullmins = target.Mins - maxs
//	hullmaxs = target.Maxs - mins
//
// and the offset is the target's origin (no further translation
// needed because the boxhull is already in target-relative space).
//
// SOLID_NOT is treated the same as SOLID_BBOX here -- the caller is
// responsible for filtering SOLID_NOT entities OUT of the trace
// candidate list (sv_world's LinkBounds / AreaQuery already does).
func HullForBounds(mins, maxs [3]float32, target Target) (bsptrace.Hull, [3]float32, error) {
	if target.Solid == server.SolidBSP {
		if target.BrushModel == nil {
			return bsptrace.Hull{}, [3]float32{}, ErrSolidBSPNeedsBrushModel
		}
		size := [3]float32{maxs[0] - mins[0], maxs[1] - mins[1], maxs[2] - mins[2]}
		var idx int
		switch {
		case size[0] < 3:
			idx = 0
		case size[0] <= 32:
			idx = 1
		default:
			idx = 2
		}
		hull := target.BrushModel.Hulls[idx]
		offset := [3]float32{
			hull.ClipMins[0] - mins[0] + target.Origin[0],
			hull.ClipMins[1] - mins[1] + target.Origin[1],
			hull.ClipMins[2] - mins[2] + target.Origin[2],
		}
		return hull, offset, nil
	}

	// Non-BSP target: build a temp boxhull from Minkowski-diff bounds.
	hullmins := [3]float32{
		target.Mins[0] - maxs[0],
		target.Mins[1] - maxs[1],
		target.Mins[2] - maxs[2],
	}
	hullmaxs := [3]float32{
		target.Maxs[0] - mins[0],
		target.Maxs[1] - mins[1],
		target.Maxs[2] - mins[2],
	}
	return boxhull.Create(hullmins, hullmaxs), target.Origin, nil
}

// PointContents returns the CONTENTS_* tag of the leaf the worldmodel
// classifies point into. tyrquake: SV_PointContents (the NQ_HACK
// branch). Returns ErrWorldModelNil when worldmodel is nil.
//
// The CONTENTS_CURRENT_* values (-9 through -14, encoding water
// current directions for QuakeC dynamics) are remapped to
// CONTENTS_WATER (-3) so callers don't need to special-case the
// six current variants for "is this point in water?" tests.
// tyrquake: the if-current-then-water remap inside SV_PointContents.
func PointContents(worldmodel *model.BrushModel, point [3]float32) (int32, error) {
	if worldmodel == nil {
		return 0, ErrWorldModelNil
	}
	hull := worldmodel.Hulls[0]
	contents, err := bsptrace.HullPointContents(&hull, hull.FirstClipNode, point)
	if err != nil {
		return 0, err
	}
	if contents <= bspfile.ContentsCurrent0 && contents >= bspfile.ContentsCurrentDn {
		contents = bspfile.ContentsWater
	}
	return contents, nil
}

// ErrWorldModelNil fires when a query is dispatched against a nil
// worldmodel -- a misconfiguration the C upstream crashes on; the
// Go port surfaces it as a recoverable error.
var ErrWorldModelNil = errors.New("world: nil worldmodel")

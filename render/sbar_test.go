// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package render

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/client"
)

// sentinelPic returns a w*h Pic whose every pixel is `mark`. The
// fill value is deliberately not the transparent index so DrawTransPic
// commits every byte and the tests can assert exact destination
// values.
func sentinelPic(w, h int, mark byte) *Pic {
	if mark == TransparentIndex {
		panic("sentinelPic mark must not be TransparentIndex")
	}
	p := &Pic{Width: w, Height: h, Pixels: make([]byte, w*h)}
	for i := range p.Pixels {
		p.Pixels[i] = mark
	}
	return p
}

// fullAssets returns a SBarAssets with every slot populated by a
// unique sentinel Pic. The base mark for each group is offset so
// pixel-value assertions can identify which Pic landed where.
func fullAssets() *SBarAssets {
	a := &SBarAssets{}
	for i := 0; i < numDigits; i++ {
		a.Nums[i] = sentinelPic(numDigitWidth, SBarHeight, byte(0x10+i))
		a.AltNums[i] = sentinelPic(numDigitWidth, SBarHeight, byte(0x20+i))
	}
	for r := 0; r < numFaces; r++ {
		for c := 0; c < numFaceStates; c++ {
			a.Faces[r][c] = sentinelPic(24, SBarHeight, byte(0x30+r*numFaceStates+c))
		}
	}
	for i := 0; i < numAmmoIcons; i++ {
		a.Ammo[i] = sentinelPic(24, SBarHeight, byte(0x40+i))
	}
	a.BG = sentinelPic(320, SBarHeight, 0x50)
	a.IBar = sentinelPic(320, SBarHeight, 0x51)
	for i := 0; i < numWeaponSlots; i++ {
		a.Weapons[i] = sentinelPic(24, 16, byte(0x60+i))
	}
	for i := 0; i < numArmorTiers; i++ {
		a.Armor[i] = sentinelPic(24, SBarHeight, byte(0x70+i))
	}
	for i := 0; i < numSigils; i++ {
		a.Sigil[i] = sentinelPic(8, 16, byte(0x80+i))
	}
	for i := 0; i < numKeys; i++ {
		a.Key[i] = sentinelPic(16, 16, byte(0x90+i))
	}
	a.Invis = sentinelPic(16, 16, 0xA0)
	a.Invuln = sentinelPic(16, 16, 0xA1)
	a.Suit = sentinelPic(16, 16, 0xA2)
	a.Quad = sentinelPic(16, 16, 0xA3)
	a.Disc = sentinelPic(24, SBarHeight, 0xA4)
	a.Colon = sentinelPic(numDigitWidth, SBarHeight, 0xA5)
	a.Slash = sentinelPic(numDigitWidth, SBarHeight, 0xA6)
	return a
}

// newSBarFB returns a tight-packed framebuffer wide enough for the
// rightmost item icon (288+16 = 304 ; round up to 320) and tall
// enough that the IBar strip + main bar both fit (SBarHeight * 2 +
// slack).
func newSBarFB(t *testing.T) *FrameBuffer {
	t.Helper()
	fb, err := NewFrameBuffer(320, 64)
	if err != nil {
		t.Fatalf("NewFrameBuffer: %v", err)
	}
	fb.Clear(0)
	return fb
}

// ----- DrawNumber --------------------------------------------------

func TestDrawNumberHappy(t *testing.T) {
	fb := newSBarFB(t)
	assets := fullAssets()
	if err := DrawNumber(fb, assets, 0, 0, 123, 3, false); err != nil {
		t.Fatalf("DrawNumber: %v", err)
	}
	// First column (x=0..23) -> Nums[1] (mark 0x11). Second (24..47)
	// -> Nums[2] (0x12). Third (48..71) -> Nums[3] (0x13).
	cases := []struct {
		x    int
		want byte
	}{{0, 0x11}, {24, 0x12}, {48, 0x13}}
	for _, c := range cases {
		got := fb.Pixels[0*fb.Pitch+c.x]
		if got != c.want {
			t.Fatalf("x=%d got %#x want %#x", c.x, got, c.want)
		}
	}
}

func TestDrawNumberRightJustifiesShortInput(t *testing.T) {
	fb := newSBarFB(t)
	assets := fullAssets()
	if err := DrawNumber(fb, assets, 0, 0, 7, 3, false); err != nil {
		t.Fatalf("DrawNumber: %v", err)
	}
	// "7" with digits=3 -> x advances by (3-1)*24 = 48 first; only
	// the third column is touched.
	if fb.Pixels[0] != 0 {
		t.Fatalf("leading column should be untouched, got %#x", fb.Pixels[0])
	}
	if got := fb.Pixels[48]; got != 0x17 {
		t.Fatalf("third column got %#x want 0x17", got)
	}
}

func TestDrawNumberZeroRendersOneDigit(t *testing.T) {
	fb := newSBarFB(t)
	assets := fullAssets()
	if err := DrawNumber(fb, assets, 0, 0, 0, 1, false); err != nil {
		t.Fatalf("DrawNumber: %v", err)
	}
	if got := fb.Pixels[0]; got != 0x10 {
		t.Fatalf("got %#x want 0x10 (Nums[0])", got)
	}
}

func TestDrawNumberNegativeClampsToZero(t *testing.T) {
	fb := newSBarFB(t)
	assets := fullAssets()
	if err := DrawNumber(fb, assets, 0, 0, -42, 1, false); err != nil {
		t.Fatalf("DrawNumber: %v", err)
	}
	if got := fb.Pixels[0]; got != 0x10 {
		t.Fatalf("got %#x want 0x10 (Nums[0]); negatives must clamp", got)
	}
}

func TestDrawNumberOverflowTruncates(t *testing.T) {
	fb := newSBarFB(t)
	assets := fullAssets()
	if err := DrawNumber(fb, assets, 0, 0, 12345, 3, false); err != nil {
		t.Fatalf("DrawNumber: %v", err)
	}
	// "12345" truncated to "345" -> Nums[3] / Nums[4] / Nums[5].
	cases := []struct {
		x    int
		want byte
	}{{0, 0x13}, {24, 0x14}, {48, 0x15}}
	for _, c := range cases {
		got := fb.Pixels[0*fb.Pitch+c.x]
		if got != c.want {
			t.Fatalf("x=%d got %#x want %#x", c.x, got, c.want)
		}
	}
}

func TestDrawNumberAltPicksAltNums(t *testing.T) {
	fb := newSBarFB(t)
	assets := fullAssets()
	if err := DrawNumber(fb, assets, 0, 0, 9, 1, true); err != nil {
		t.Fatalf("DrawNumber: %v", err)
	}
	if got := fb.Pixels[0]; got != 0x29 {
		t.Fatalf("got %#x want 0x29 (AltNums[9])", got)
	}
}

func TestDrawNumberNilFB(t *testing.T) {
	if err := DrawNumber(nil, fullAssets(), 0, 0, 0, 1, false); !errors.Is(err, ErrSbarNilFB) {
		t.Fatalf("nil fb: %v want ErrSbarNilFB", err)
	}
}

func TestDrawNumberNilAssets(t *testing.T) {
	if err := DrawNumber(newSBarFB(t), nil, 0, 0, 0, 1, false); !errors.Is(err, ErrSbarNilAssets) {
		t.Fatalf("nil assets: %v want ErrSbarNilAssets", err)
	}
}

func TestDrawNumberNilDigitPic(t *testing.T) {
	assets := fullAssets()
	assets.Nums[5] = nil
	if err := DrawNumber(newSBarFB(t), assets, 0, 0, 5, 1, false); !errors.Is(err, ErrSbarNilAssets) {
		t.Fatalf("nil Nums[5]: %v want ErrSbarNilAssets", err)
	}
}

func TestDrawNumberPropagatesDrawError(t *testing.T) {
	// Pic with mismatched Pixels length triggers ErrPicShape inside
	// DrawTransPic; DrawNumber must surface it.
	assets := fullAssets()
	assets.Nums[1] = &Pic{Width: 24, Height: SBarHeight, Pixels: make([]byte, 3)}
	err := DrawNumber(newSBarFB(t), assets, 0, 0, 1, 1, false)
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("got %v want ErrPicShape", err)
	}
}

// ----- PickFaceFrame -----------------------------------------------

func TestPickFaceFrameBands(t *testing.T) {
	cases := []struct {
		health  int
		wantRow int
	}{
		{200, 4}, {100, 4},
		{99, 4}, {80, 4},
		{79, 3}, {60, 3},
		{59, 2}, {40, 2},
		{39, 1}, {20, 1},
		{19, 0}, {0, 0},
		{-5, 0},
	}
	for _, c := range cases {
		row, col := PickFaceFrame(c.health, false)
		if row != c.wantRow || col != 0 {
			t.Fatalf("health=%d got (row=%d,col=%d) want (row=%d,col=0)", c.health, row, col, c.wantRow)
		}
	}
}

func TestPickFaceFrameDamagedSelectsPainedColumn(t *testing.T) {
	row, col := PickFaceFrame(50, true)
	if row != 2 || col != 1 {
		t.Fatalf("got (row=%d,col=%d) want (2,1)", row, col)
	}
}

// ----- DrawFace ----------------------------------------------------

func TestDrawFaceHappy(t *testing.T) {
	fb := newSBarFB(t)
	assets := fullAssets()
	if err := DrawFace(fb, assets, 0, 0, 100, false); err != nil {
		t.Fatalf("DrawFace: %v", err)
	}
	// Faces[4][0] mark = 0x30 + 4*2 + 0 = 0x38.
	if got := fb.Pixels[0]; got != 0x38 {
		t.Fatalf("got %#x want 0x38", got)
	}
}

func TestDrawFaceDamagedUsesPainedFrame(t *testing.T) {
	fb := newSBarFB(t)
	assets := fullAssets()
	if err := DrawFace(fb, assets, 0, 0, 100, true); err != nil {
		t.Fatalf("DrawFace: %v", err)
	}
	// Faces[4][1] mark = 0x30 + 4*2 + 1 = 0x39.
	if got := fb.Pixels[0]; got != 0x39 {
		t.Fatalf("got %#x want 0x39", got)
	}
}

func TestDrawFaceNilFB(t *testing.T) {
	if err := DrawFace(nil, fullAssets(), 0, 0, 100, false); !errors.Is(err, ErrSbarNilFB) {
		t.Fatalf("got %v want ErrSbarNilFB", err)
	}
}

func TestDrawFaceNilAssets(t *testing.T) {
	if err := DrawFace(newSBarFB(t), nil, 0, 0, 100, false); !errors.Is(err, ErrSbarNilAssets) {
		t.Fatalf("got %v want ErrSbarNilAssets", err)
	}
}

func TestDrawFaceNilFacePic(t *testing.T) {
	assets := fullAssets()
	assets.Faces[4][0] = nil
	if err := DrawFace(newSBarFB(t), assets, 0, 0, 100, false); !errors.Is(err, ErrSbarNilAssets) {
		t.Fatalf("got %v want ErrSbarNilAssets", err)
	}
}

// ----- DrawSBar ----------------------------------------------------

// drawSBarState builds a fully-populated client.State suitable for
// the happy-path DrawSBar test: 100 HP / 75 armor / 42 cells active /
// every item flag set / every weapon owned. Items=^int32(0) drives
// the inventory + item rows to draw every slot so the test exercises
// every branch in one call.
func drawSBarState() *client.State {
	s := client.NewState()
	s.Health = 100
	s.CurrentAmmo = 42
	s.Items = ^int32(0)
	s.Stats[statArmor] = 75
	return s
}

func TestDrawSBarHappy(t *testing.T) {
	fb := newSBarFB(t)
	assets := fullAssets()
	state := drawSBarState()
	if err := DrawSBar(fb, state, assets); err != nil {
		t.Fatalf("DrawSBar: %v", err)
	}
	baseY := fb.Height - SBarHeight

	// Sample a handful of well-known positions:
	//   - BG mark 0x50 at (0, baseY) is overwritten by the armor
	//     icon (mark 0x72 since all three armor bits are set ->
	//     red).
	//   - Face Pic at (112, baseY) -> Faces[4][0] mark 0x38.
	//   - Health digit at (136, baseY) -> Nums[1] mark 0x11.
	//   - Ammo icon at (224, baseY) -> Ammo[0] mark 0x40 (shells
	//     wins the else-if when every flag is set).
	cases := []struct {
		x, y int
		want byte
		desc string
	}{
		// With every item flag set, IT_INVULNERABILITY wins the
		// armor branch -> the Disc overlay (mark 0xA4) lands at
		// (0, baseY) on top of the BG + armor icon.
		{0, baseY, 0xA4, "disc overlay (invulnerable)"},
		{112, baseY, 0x38, "face icon row=4 col=0"},
		{136, baseY, 0x11, "health digit 1 of 100"},
		{224, baseY, 0x40, "ammo icon (shells)"},
		// Inventory strip is at baseY - 24; the IBar covers x=0
		// (mark 0x51). The first weapon icon overdraws at y =
		// baseY - 16.
		{0, baseY - SBarHeight, 0x51, "IBar background"},
		{0, baseY - 16, 0x60, "weapon icon 0 overdraw"},
		// Key icon at (192, baseY-16) -> Key[0] mark 0x90.
		{192, baseY - 16, 0x90, "key 0"},
		// Sigil at (320-32, baseY-16) -> Sigil[0] mark 0x80.
		{320 - 32, baseY - 16, 0x80, "sigil 0"},
	}
	for _, c := range cases {
		got := fb.Pixels[c.y*fb.Pitch+c.x]
		if got != c.want {
			t.Fatalf("%s @(%d,%d): got %#x want %#x", c.desc, c.x, c.y, got, c.want)
		}
	}
}

func TestDrawSBarLowHealthShowsPainedAltDigits(t *testing.T) {
	fb := newSBarFB(t)
	assets := fullAssets()
	state := client.NewState()
	state.Health = 15
	state.CurrentAmmo = 5
	state.Items = itemNails // ammo icon = nails (Ammo[1])
	state.Stats[statArmor] = 10
	if err := DrawSBar(fb, state, assets); err != nil {
		t.Fatalf("DrawSBar: %v", err)
	}
	baseY := fb.Height - SBarHeight

	// Health=15 -> band 0 face (mark 0x30) at (112, baseY).
	if got := fb.Pixels[baseY*fb.Pitch+112]; got != 0x30 {
		t.Fatalf("face: got %#x want 0x30 (Faces[0][0])", got)
	}
	// Health digits use AltNums; "15" right-justified to 3 digits
	// lands at (136 + 24, baseY) for '1' and (136 + 48, baseY) for
	// '5'.
	if got := fb.Pixels[baseY*fb.Pitch+136+24]; got != 0x21 {
		t.Fatalf("health tens: got %#x want 0x21 (AltNums[1])", got)
	}
	if got := fb.Pixels[baseY*fb.Pitch+136+48]; got != 0x25 {
		t.Fatalf("health units: got %#x want 0x25 (AltNums[5])", got)
	}
	// Ammo icon at (224, baseY) -> Ammo[1] mark 0x41.
	if got := fb.Pixels[baseY*fb.Pitch+224]; got != 0x41 {
		t.Fatalf("ammo icon: got %#x want 0x41 (Ammo[1])", got)
	}
}

func TestDrawSBarInvulnerableShowsDisc(t *testing.T) {
	fb := newSBarFB(t)
	assets := fullAssets()
	state := client.NewState()
	state.Health = 100
	state.Items = itemInvulnerability
	state.Stats[statArmor] = 0
	if err := DrawSBar(fb, state, assets); err != nil {
		t.Fatalf("DrawSBar: %v", err)
	}
	baseY := fb.Height - SBarHeight
	// Disc overlays at (0, baseY) -> mark 0xA4.
	if got := fb.Pixels[baseY*fb.Pitch+0]; got != 0xA4 {
		t.Fatalf("disc: got %#x want 0xA4", got)
	}
	// Armor digits land at (24, baseY) showing "666" in AltNums.
	// Last column (x = 24 + 48) -> AltNums[6] mark 0x26.
	if got := fb.Pixels[baseY*fb.Pitch+24+48]; got != 0x26 {
		t.Fatalf("armor 6: got %#x want 0x26", got)
	}
}

func TestDrawSBarRogerThroughArmorTiers(t *testing.T) {
	// Verify the green / yellow / red picker walks the correct
	// else-if order (red wins over yellow wins over green).
	cases := []struct {
		items int32
		mark  byte
	}{
		{itemArmor1, 0x70},
		{itemArmor2, 0x71},
		{itemArmor3, 0x72},
		{itemArmor1 | itemArmor2, 0x71},              // yellow wins
		{itemArmor1 | itemArmor2 | itemArmor3, 0x72}, // red wins
	}
	for _, c := range cases {
		fb := newSBarFB(t)
		assets := fullAssets()
		state := client.NewState()
		state.Health = 100
		state.Items = c.items
		if err := DrawSBar(fb, state, assets); err != nil {
			t.Fatalf("DrawSBar items=%#x: %v", c.items, err)
		}
		baseY := fb.Height - SBarHeight
		if got := fb.Pixels[baseY*fb.Pitch+0]; got != c.mark {
			t.Fatalf("items=%#x: armor got %#x want %#x", c.items, got, c.mark)
		}
	}
}

func TestDrawSBarAmmoIconElseIfChain(t *testing.T) {
	// Each ammo flag in isolation must select the matching icon.
	cases := []struct {
		items int32
		mark  byte
	}{
		{itemShells, 0x40},
		{itemNails, 0x41},
		{itemRockets, 0x42},
		{itemCells, 0x43},
	}
	for _, c := range cases {
		fb := newSBarFB(t)
		assets := fullAssets()
		state := client.NewState()
		state.Health = 100
		state.Items = c.items
		if err := DrawSBar(fb, state, assets); err != nil {
			t.Fatalf("DrawSBar items=%#x: %v", c.items, err)
		}
		baseY := fb.Height - SBarHeight
		if got := fb.Pixels[baseY*fb.Pitch+224]; got != c.mark {
			t.Fatalf("items=%#x: ammo got %#x want %#x", c.items, got, c.mark)
		}
	}
}

func TestDrawSBarNoAmmoFlagSkipsIcon(t *testing.T) {
	// Build a partial asset set: NO ammo icons + NO BG so that when
	// no ammo flag is set, position (224, baseY) is left at the
	// framebuffer clear byte (0). If pickAmmoIcon returned a hit
	// despite items==0 the test would fail with an out-of-range
	// nil-pic dereference from drawIfNotNil.
	fb := newSBarFB(t)
	assets := &SBarAssets{}
	for i := 0; i < numDigits; i++ {
		assets.Nums[i] = sentinelPic(numDigitWidth, SBarHeight, byte(0x10+i))
		assets.AltNums[i] = sentinelPic(numDigitWidth, SBarHeight, byte(0x20+i))
	}
	for r := 0; r < numFaces; r++ {
		for c := 0; c < numFaceStates; c++ {
			assets.Faces[r][c] = sentinelPic(24, SBarHeight, byte(0x30+r*numFaceStates+c))
		}
	}
	state := client.NewState()
	state.Health = 100
	state.Items = 0
	state.CurrentAmmo = 0
	if err := DrawSBar(fb, state, assets); err != nil {
		t.Fatalf("DrawSBar: %v", err)
	}
	baseY := fb.Height - SBarHeight
	// (224, baseY) should still hold the framebuffer clear byte 0
	// because no ammo flag was set and BG is nil.
	if got := fb.Pixels[baseY*fb.Pitch+224]; got != 0 {
		t.Fatalf("got %#x want 0 (no ammo icon expected)", got)
	}
}

func TestDrawSBarNilBGAssetsSkipsBackground(t *testing.T) {
	// Bare-minimum assets: enough to draw the digits + face for the
	// non-nil paths but with BG / IBar / armor icons left nil, so
	// the drawIfNotNil + nil-BG branches all execute.
	fb := newSBarFB(t)
	assets := &SBarAssets{}
	for i := 0; i < numDigits; i++ {
		assets.Nums[i] = sentinelPic(numDigitWidth, SBarHeight, byte(0x10+i))
		assets.AltNums[i] = sentinelPic(numDigitWidth, SBarHeight, byte(0x20+i))
	}
	for r := 0; r < numFaces; r++ {
		for c := 0; c < numFaceStates; c++ {
			assets.Faces[r][c] = sentinelPic(24, SBarHeight, byte(0x30+r*numFaceStates+c))
		}
	}
	state := client.NewState()
	state.Health = 50
	state.Items = itemKey1 | itemQuad | itemSigil0 // exercises nil-skip in drawItemRow + sigil loop
	if err := DrawSBar(fb, state, assets); err != nil {
		t.Fatalf("DrawSBar: %v", err)
	}
}

func TestDrawSBarNilFB(t *testing.T) {
	if err := DrawSBar(nil, drawSBarState(), fullAssets()); !errors.Is(err, ErrSbarNilFB) {
		t.Fatalf("got %v want ErrSbarNilFB", err)
	}
}

func TestDrawSBarNilState(t *testing.T) {
	if err := DrawSBar(newSBarFB(t), nil, fullAssets()); !errors.Is(err, ErrSbarNilState) {
		t.Fatalf("got %v want ErrSbarNilState", err)
	}
}

func TestDrawSBarNilAssets(t *testing.T) {
	if err := DrawSBar(newSBarFB(t), drawSBarState(), nil); !errors.Is(err, ErrSbarNilAssets) {
		t.Fatalf("got %v want ErrSbarNilAssets", err)
	}
}

// ----- error propagation through DrawSBar's inner helpers ---------

func TestDrawSBarPropagatesIBarShapeError(t *testing.T) {
	assets := fullAssets()
	assets.IBar = &Pic{Width: 4, Height: 4, Pixels: make([]byte, 3)}
	err := DrawSBar(newSBarFB(t), drawSBarState(), assets)
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("got %v want ErrPicShape", err)
	}
}

func TestDrawSBarPropagatesWeaponShapeError(t *testing.T) {
	assets := fullAssets()
	assets.Weapons[0] = &Pic{Width: 4, Height: 4, Pixels: make([]byte, 3)}
	state := client.NewState()
	state.Health = 100
	state.Items = 1 << 0 // own weapon 0 so the loop drains its icon
	err := DrawSBar(newSBarFB(t), state, assets)
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("got %v want ErrPicShape", err)
	}
}

func TestDrawSBarPropagatesItemShapeError(t *testing.T) {
	assets := fullAssets()
	assets.Key[0] = &Pic{Width: 4, Height: 4, Pixels: make([]byte, 3)}
	state := client.NewState()
	state.Health = 100
	state.Items = itemKey1
	err := DrawSBar(newSBarFB(t), state, assets)
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("got %v want ErrPicShape", err)
	}
}

func TestDrawSBarPropagatesSigilShapeError(t *testing.T) {
	assets := fullAssets()
	assets.Sigil[1] = &Pic{Width: 4, Height: 4, Pixels: make([]byte, 3)}
	state := client.NewState()
	state.Health = 100
	state.Items = itemSigil0 << 1
	err := DrawSBar(newSBarFB(t), state, assets)
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("got %v want ErrPicShape", err)
	}
}

func TestDrawSBarPropagatesBGShapeError(t *testing.T) {
	assets := fullAssets()
	assets.BG = &Pic{Width: 4, Height: 4, Pixels: make([]byte, 3)}
	err := DrawSBar(newSBarFB(t), drawSBarState(), assets)
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("got %v want ErrPicShape", err)
	}
}

func TestDrawSBarPropagatesArmorIconShapeError(t *testing.T) {
	assets := fullAssets()
	assets.Armor[2] = &Pic{Width: 4, Height: 4, Pixels: make([]byte, 3)}
	state := client.NewState()
	state.Health = 100
	state.Items = itemArmor3
	state.Stats[statArmor] = 50
	err := DrawSBar(newSBarFB(t), state, assets)
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("got %v want ErrPicShape", err)
	}
}

func TestDrawSBarPropagatesArmorDigitError(t *testing.T) {
	assets := fullAssets()
	assets.Nums[5] = nil
	state := client.NewState()
	state.Health = 100
	state.Items = 0
	state.Stats[statArmor] = 500 // forces a "5" digit -> Nums[5]
	err := DrawSBar(newSBarFB(t), state, assets)
	if !errors.Is(err, ErrSbarNilAssets) {
		t.Fatalf("got %v want ErrSbarNilAssets", err)
	}
}

func TestDrawSBarPropagatesInvulnDigitError(t *testing.T) {
	// Invuln triggers the 666 path which uses AltNums; nil that
	// slot to drain the error branch.
	assets := fullAssets()
	assets.AltNums[6] = nil
	state := client.NewState()
	state.Health = 100
	state.Items = itemInvulnerability
	err := DrawSBar(newSBarFB(t), state, assets)
	if !errors.Is(err, ErrSbarNilAssets) {
		t.Fatalf("got %v want ErrSbarNilAssets", err)
	}
}

func TestDrawSBarPropagatesInvulnDiscError(t *testing.T) {
	assets := fullAssets()
	assets.Disc = &Pic{Width: 4, Height: 4, Pixels: make([]byte, 3)}
	state := client.NewState()
	state.Health = 100
	state.Items = itemInvulnerability
	err := DrawSBar(newSBarFB(t), state, assets)
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("got %v want ErrPicShape", err)
	}
}

func TestDrawSBarPropagatesFaceError(t *testing.T) {
	assets := fullAssets()
	assets.Faces[4][0] = nil
	err := DrawSBar(newSBarFB(t), drawSBarState(), assets)
	if !errors.Is(err, ErrSbarNilAssets) {
		t.Fatalf("got %v want ErrSbarNilAssets", err)
	}
}

func TestDrawSBarPropagatesHealthDigitError(t *testing.T) {
	assets := fullAssets()
	assets.Nums[1] = nil
	state := client.NewState()
	state.Health = 100 // "100" -> first digit needs Nums[1]
	state.Items = 0
	err := DrawSBar(newSBarFB(t), state, assets)
	if !errors.Is(err, ErrSbarNilAssets) {
		t.Fatalf("got %v want ErrSbarNilAssets", err)
	}
}

func TestDrawSBarPropagatesAmmoIconError(t *testing.T) {
	assets := fullAssets()
	assets.Ammo[2] = &Pic{Width: 4, Height: 4, Pixels: make([]byte, 3)}
	state := client.NewState()
	state.Health = 100
	state.Items = itemRockets
	err := DrawSBar(newSBarFB(t), state, assets)
	if !errors.Is(err, ErrPicShape) {
		t.Fatalf("got %v want ErrPicShape", err)
	}
}

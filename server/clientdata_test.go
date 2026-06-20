// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
	"testing"

	"github.com/go-quake1/engine/msg"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/sizebuf"
)

// Protocol-constant exact values used here (audit trail):
//
//	protocol.SvcClientData     = 15
//	protocol.SUViewHeight      = 1 << 0
//	protocol.SUIdealPitch      = 1 << 1
//	protocol.SUPunch1          = 1 << 2
//	protocol.SUVelocity1       = 1 << 5
//	protocol.SUItems           = 1 << 9
//	protocol.SUOnGround        = 1 << 10
//	protocol.SUInWater         = 1 << 11
//	protocol.SUWeaponFrame     = 1 << 12
//	protocol.SUArmor           = 1 << 13
//	protocol.SUWeapon          = 1 << 14
//	protocol.DefaultViewHeight = 22

// defaultState returns the minimal-wire baseline: SUItems is forced
// (always set by the encoder), every other SU bit is suppressed.
// In particular ViewHeightOffset is set to DefaultViewHeight so the
// SUViewHeight bit stays unset.
func defaultState() ClientDataState {
	return ClientDataState{ViewHeightOffset: protocol.DefaultViewHeight}
}

// minWireSize is the byte count of the minimum-shape svc_clientdata
// message: cmd(1) + bits(2) + items(4) + health(2) + currentammo(1)
// + ammo[0..3](4) + activeweapon(1) = 15.
const minWireSize = 15

func TestEncodeClientData_MinimalWire(t *testing.T) {
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, defaultState()); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != minWireSize {
		t.Errorf("wire size: got %d want %d", buf.Len(), minWireSize)
	}
	r := msg.NewReader(buf.Bytes())
	if cmd := r.ReadU8(); cmd != protocol.SvcClientData {
		t.Errorf("cmd byte: got %d want %d (SvcClientData)", cmd, protocol.SvcClientData)
	}
	// bits is a signed short; SUItems = 512 fits, no sign-extension.
	if bits := r.ReadShort(); bits != protocol.SUItems {
		t.Errorf("bits: got %#x want %#x (SUItems only)", bits, protocol.SUItems)
	}
	if items := r.ReadLong(); items != 0 {
		t.Errorf("items: got %d want 0", items)
	}
	if h := r.ReadShort(); h != 0 {
		t.Errorf("health: got %d want 0", h)
	}
	for i := 0; i < 1+4+1; i++ {
		if v := r.ReadU8(); v != 0 {
			t.Errorf("trailing byte %d: got %d want 0", i, v)
		}
	}
}

func TestEncodeClientData_ViewHeightBit(t *testing.T) {
	s := defaultState()
	s.ViewHeightOffset = 16 // != 22 -> SUViewHeight set
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, s); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != minWireSize+1 {
		t.Errorf("wire size: got %d want %d", buf.Len(), minWireSize+1)
	}
	r := msg.NewReader(buf.Bytes())
	r.ReadU8()
	bits := r.ReadShort()
	if bits&protocol.SUViewHeight == 0 {
		t.Errorf("SUViewHeight not set in bits %#x", bits)
	}
	if got := r.ReadChar(); got != 16 {
		t.Errorf("view-height char: got %d want 16", got)
	}
}

func TestEncodeClientData_IdealPitchBit(t *testing.T) {
	s := defaultState()
	s.IdealPitch = -12
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, s); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != minWireSize+1 {
		t.Errorf("wire size: got %d want %d", buf.Len(), minWireSize+1)
	}
	r := msg.NewReader(buf.Bytes())
	r.ReadU8()
	bits := r.ReadShort()
	if bits&protocol.SUIdealPitch == 0 {
		t.Errorf("SUIdealPitch not set in bits %#x", bits)
	}
	if got := r.ReadChar(); got != -12 {
		t.Errorf("ideal-pitch char: got %d want -12", got)
	}
}

// Per-axis PunchAngle: only the axis with a non-zero value sets its
// SU bit + writes its char. Drives all three SUPunch{1,2,3} branches.
func TestEncodeClientData_PunchAnglePerAxis(t *testing.T) {
	for axis := 0; axis < 3; axis++ {
		s := defaultState()
		s.PunchAngle[axis] = float32(-1 - axis) // -1, -2, -3
		buf := sizebuf.New(make([]byte, 64))
		if err := EncodeClientData(buf, s); err != nil {
			t.Fatalf("axis=%d: %v", axis, err)
		}
		if buf.Len() != minWireSize+1 {
			t.Errorf("axis=%d: wire size got %d want %d", axis, buf.Len(), minWireSize+1)
		}
		r := msg.NewReader(buf.Bytes())
		r.ReadU8()
		bits := r.ReadShort()
		// Only this axis's SUPunch bit is set, none of the others.
		for o := 0; o < 3; o++ {
			masked := bits & (protocol.SUPunch1 << o)
			if (o == axis) != (masked != 0) {
				t.Errorf("axis=%d: punch bit for axis %d: got %#x", axis, o, masked)
			}
		}
		if got := r.ReadChar(); got != -1-axis {
			t.Errorf("axis=%d: punch char got %d want %d", axis, got, -1-axis)
		}
	}
}

// Per-axis Velocity: only that axis's SUVelocity bit is set + its
// quantised char (value/16) is emitted. Drives all three branches.
func TestEncodeClientData_VelocityPerAxis(t *testing.T) {
	for axis := 0; axis < 3; axis++ {
		s := defaultState()
		s.Velocity[axis] = 320 // 320 / 16 = 20
		buf := sizebuf.New(make([]byte, 64))
		if err := EncodeClientData(buf, s); err != nil {
			t.Fatalf("axis=%d: %v", axis, err)
		}
		if buf.Len() != minWireSize+1 {
			t.Errorf("axis=%d: wire size got %d want %d", axis, buf.Len(), minWireSize+1)
		}
		r := msg.NewReader(buf.Bytes())
		r.ReadU8()
		bits := r.ReadShort()
		for o := 0; o < 3; o++ {
			masked := bits & (protocol.SUVelocity1 << o)
			if (o == axis) != (masked != 0) {
				t.Errorf("axis=%d: velocity bit for axis %d: got %#x", axis, o, masked)
			}
		}
		if got := r.ReadChar(); got != 20 {
			t.Errorf("axis=%d: velocity char got %d want 20 (320/16)", axis, got)
		}
	}
}

// Combined punch+velocity on the same axis exercises BOTH branches of
// the per-axis loop body in sequence.
func TestEncodeClientData_PunchAndVelocitySameAxis(t *testing.T) {
	s := defaultState()
	s.PunchAngle[1] = 5
	s.Velocity[1] = 16 // /16 = 1
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, s); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != minWireSize+2 {
		t.Errorf("wire size: got %d want %d", buf.Len(), minWireSize+2)
	}
	r := msg.NewReader(buf.Bytes())
	r.ReadU8()
	bits := r.ReadShort()
	if bits&(protocol.SUPunch1<<1) == 0 {
		t.Errorf("SUPunch2 not set in %#x", bits)
	}
	if bits&(protocol.SUVelocity1<<1) == 0 {
		t.Errorf("SUVelocity2 not set in %#x", bits)
	}
	if got := r.ReadChar(); got != 5 {
		t.Errorf("punch char: got %d want 5", got)
	}
	if got := r.ReadChar(); got != 1 {
		t.Errorf("velocity char: got %d want 1", got)
	}
}

func TestEncodeClientData_ItemsRoundtripsAsLong(t *testing.T) {
	s := defaultState()
	const want = int32(-559038737) // 0xDEADBEEF reinterpreted as int32
	s.Items = want
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, s); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	r.ReadU8()
	r.ReadShort()
	if got := r.ReadLong(); got != want {
		t.Errorf("items round-trip: got %d want %d", got, want)
	}
}

func TestEncodeClientData_OnGroundInWaterBits(t *testing.T) {
	s := defaultState()
	s.OnGround = true
	s.InWater = true
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, s); err != nil {
		t.Fatal(err)
	}
	// No extra payload bytes: these bits never gate an extra write.
	if buf.Len() != minWireSize {
		t.Errorf("wire size: got %d want %d", buf.Len(), minWireSize)
	}
	r := msg.NewReader(buf.Bytes())
	r.ReadU8()
	bits := r.ReadShort()
	if bits&protocol.SUOnGround == 0 {
		t.Errorf("SUOnGround not set in %#x", bits)
	}
	if bits&protocol.SUInWater == 0 {
		t.Errorf("SUInWater not set in %#x", bits)
	}
}

func TestEncodeClientData_WeaponFrameBit(t *testing.T) {
	s := defaultState()
	s.WeaponFrame = 7
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, s); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != minWireSize+1 {
		t.Errorf("wire size: got %d want %d", buf.Len(), minWireSize+1)
	}
	r := msg.NewReader(buf.Bytes())
	r.ReadU8()
	if bits := r.ReadShort(); bits&protocol.SUWeaponFrame == 0 {
		t.Errorf("SUWeaponFrame not set in %#x", bits)
	}
	r.ReadLong() // items
	if got := r.ReadU8(); got != 7 {
		t.Errorf("weaponframe byte: got %d want 7", got)
	}
}

func TestEncodeClientData_ArmorBit(t *testing.T) {
	s := defaultState()
	s.ArmorValue = 150
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, s); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != minWireSize+1 {
		t.Errorf("wire size: got %d want %d", buf.Len(), minWireSize+1)
	}
	r := msg.NewReader(buf.Bytes())
	r.ReadU8()
	if bits := r.ReadShort(); bits&protocol.SUArmor == 0 {
		t.Errorf("SUArmor not set")
	}
	r.ReadLong()
	if got := r.ReadU8(); got != 150 {
		t.Errorf("armor byte: got %d want 150", got)
	}
}

func TestEncodeClientData_WeaponBit(t *testing.T) {
	s := defaultState()
	s.WeaponModel = 3
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, s); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != minWireSize+1 {
		t.Errorf("wire size: got %d want %d", buf.Len(), minWireSize+1)
	}
	r := msg.NewReader(buf.Bytes())
	r.ReadU8()
	if bits := r.ReadShort(); bits&protocol.SUWeapon == 0 {
		t.Errorf("SUWeapon not set")
	}
	r.ReadLong()
	if got := r.ReadU8(); got != 3 {
		t.Errorf("weapon model: got %d want 3", got)
	}
}

func TestEncodeClientData_HealthSigned(t *testing.T) {
	s := defaultState()
	s.Health = -27
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, s); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	r.ReadU8()
	r.ReadShort()
	r.ReadLong()
	if got := r.ReadShort(); got != -27 {
		t.Errorf("health: got %d want -27", got)
	}
}

func TestEncodeClientData_AmmoCurrentAndArray(t *testing.T) {
	s := defaultState()
	s.CurrentAmmo = 99
	s.Ammo = [4]int{10, 20, 30, 40}
	s.ActiveWeapon = 5
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, s); err != nil {
		t.Fatal(err)
	}
	r := msg.NewReader(buf.Bytes())
	r.ReadU8()
	r.ReadShort()
	r.ReadLong()
	r.ReadShort() // health
	if got := r.ReadU8(); got != 99 {
		t.Errorf("currentammo: got %d want 99", got)
	}
	for i, want := range []int{10, 20, 30, 40} {
		if got := r.ReadU8(); got != want {
			t.Errorf("ammo[%d]: got %d want %d", i, got, want)
		}
	}
	if got := r.ReadU8(); got != 5 {
		t.Errorf("activeweapon: got %d want 5", got)
	}
}

func TestEncodeClientData_NilBuf(t *testing.T) {
	if err := EncodeClientData(nil, defaultState()); !errors.Is(err, ErrNilBuf) {
		t.Errorf("got %v want ErrNilBuf", err)
	}
}

// Per-write overflow propagation. The state is engineered to gate
// EVERY conditional write on, so capping the buffer at each running
// byte-count fails at one write site. The table walks the wire shape
// in order and asserts each Write* path returns an error.
//
// Wire shape with all conditional bits ON:
//
//	cmd(1) bits(2)            -> running 0,1
//	view_height char(1)       -> 3
//	ideal_pitch  char(1)      -> 4
//	for axis 0..2 { punch char(1), vel char(1) } -> 5,6 7,8 9,10
//	items long(4)             -> 11..14
//	weaponframe byte(1)       -> 15
//	armor       byte(1)       -> 16
//	weaponmodel byte(1)       -> 17
//	health short(2)           -> 18,19
//	currentammo byte(1)       -> 20
//	ammo[0..3]  byte*4        -> 21,22,23,24
//	activeweapon byte(1)      -> 25
//
// Capping the buffer at K bytes makes the (K+1)-th byte's Write fail.
func TestEncodeClientData_OverflowAtEachWriteSite(t *testing.T) {
	// Force every conditional bit on + at least one byte per write.
	full := ClientDataState{
		ViewHeightOffset: 16, // != 22 -> SUViewHeight
		IdealPitch:       1,
		PunchAngle:       [3]float32{1, 1, 1},
		Velocity:         [3]float32{16, 16, 16},
		Items:            0x12345678,
		OnGround:         true,
		InWater:          true,
		WeaponFrame:      1,
		ArmorValue:       1,
		WeaponModel:      1,
		Health:           1,
		CurrentAmmo:      1,
		Ammo:             [4]int{1, 1, 1, 1},
		ActiveWeapon:     1,
	}
	// Total successful wire size with all bits on: see above.
	const fullSize = 26
	// Sanity-check the size on a generous buffer first.
	buf := sizebuf.New(make([]byte, 64))
	if err := EncodeClientData(buf, full); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if buf.Len() != fullSize {
		t.Fatalf("setup: full-shape wire size got %d want %d", buf.Len(), fullSize)
	}
	for cap := 0; cap < fullSize; cap++ {
		buf := sizebuf.New(make([]byte, cap))
		err := EncodeClientData(buf, full)
		if err == nil {
			t.Errorf("cap=%d: expected overflow error, got nil", cap)
		}
	}
}

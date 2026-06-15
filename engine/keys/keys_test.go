// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package keys

import "testing"

// --- numeric layout: pin a representative subset ----------------------------

func TestKeyLayout_ASCII(t *testing.T) {
	cases := map[Key]int{
		KSpace: 32, K0: 48, K9: 57, KA: 97, KZ: 122, KDel: 127,
		KBackspace: 8, KReturn: 13,
	}
	for k, want := range cases {
		if int(k) != want {
			t.Errorf("%v: got %d want %d", k, int(k), want)
		}
	}
	// KEnter is an alias of KReturn (both 13).
	if KEnter != KReturn {
		t.Error("KEnter should alias KReturn")
	}
}

func TestKeyLayout_Kp(t *testing.T) {
	if int(KKp0) != 256 || int(KKpEquals) != 272 {
		t.Errorf("keypad drift: %d %d", KKp0, KKpEquals)
	}
}

func TestKeyLayout_Arrows(t *testing.T) {
	if int(KUpArrow) != 273 || int(KPgDn) != 281 {
		t.Errorf("arrow drift: %d %d", KUpArrow, KPgDn)
	}
}

func TestKeyLayout_F(t *testing.T) {
	if int(KF1) != 282 || int(KF15) != 296 {
		t.Errorf("F-key drift: F1=%d F15=%d", KF1, KF15)
	}
}

func TestKeyLayout_Modifiers(t *testing.T) {
	if int(KNumLock) != 300 || int(KUndo) != 322 {
		t.Errorf("modifier-key drift: %d %d", KNumLock, KUndo)
	}
}

func TestKeyLayout_MouseAndJoy(t *testing.T) {
	if int(KMouse1) != 323 || int(KJoy4) != 334 {
		t.Errorf("mouse/joy drift: %d %d", KMouse1, KJoy4)
	}
}

func TestKeyLayout_Aux(t *testing.T) {
	if int(KAux1) != 335 || int(KAux32) != 366 {
		t.Errorf("aux drift: %d %d", KAux1, KAux32)
	}
	if int(KLast) != 367 {
		t.Errorf("KLast: %d", KLast)
	}
}

func TestAliases(t *testing.T) {
	if KShift != KLShift || KCtrl != KLCtrl || KAlt != KLAlt {
		t.Error("modifier aliases drift")
	}
	if KMWheelUp != KMouse4 || KMWheelDown != KMouse5 {
		t.Error("mouse-wheel aliases drift")
	}
}

// --- Dest enum --------------------------------------------------------------

func TestDestLayout(t *testing.T) {
	if DestGame != 0 || DestConsole != 1 || DestMessage != 2 || DestMenu != 3 || DestNone != 4 {
		t.Error("Dest layout drift")
	}
}

// --- Name + KeyForName round-trip ------------------------------------------

func TestName_KnownKeys(t *testing.T) {
	cases := []struct {
		k    Key
		want string
	}{
		{KEnter, "enter"},
		{KTab, "tab"},
		{KEscape, "escape"},
		{KUpArrow, "uparrow"},
		{KMouse1, "mouse1"},
		{KMouse4, "mwheelup"},  // alias
		{KMouse5, "mwheeldown"},
		{KSemicolon, "semicolon"},
	}
	for _, c := range cases {
		if got := Name(c.k); got != c.want {
			t.Errorf("Name(%v): got %q want %q", c.k, got, c.want)
		}
	}
}

func TestName_PrintableAscii(t *testing.T) {
	if got := Name(KA); got != "a" {
		t.Errorf("Name(KA): %q", got)
	}
	if got := Name(K0); got != "0" {
		t.Errorf("Name(K0): %q", got)
	}
	if got := Name(KSpace); got != " " {
		t.Errorf("Name(KSpace): %q", got)
	}
}

func TestName_Unknown(t *testing.T) {
	if got := Name(KUnknown); got != "(unknown)" {
		t.Errorf("Name(KUnknown): %q", got)
	}
}

func TestName_HighKeyFallback(t *testing.T) {
	// A key value past the nameTable + outside ASCII falls back to "key###".
	if got := Name(Key(500)); got != "key500" {
		t.Errorf("Name(500): %q", got)
	}
}

func TestKeyForName_ASCII(t *testing.T) {
	if got := KeyForName("a"); got != KA {
		t.Errorf("a: %v", got)
	}
	if got := KeyForName("0"); got != K0 {
		t.Errorf("0: %v", got)
	}
	if got := KeyForName("?"); got != KQuestion {
		t.Errorf("?: %v", got)
	}
}

func TestKeyForName_KnownTokens(t *testing.T) {
	for _, c := range []struct {
		name string
		want Key
	}{
		{"enter", KEnter},
		{"tab", KTab},
		{"escape", KEscape},
		{"uparrow", KUpArrow},
		{"semicolon", KSemicolon},
		{"f1", KF1},
		{"f12", KF12},
		{"mwheelup", KMouse4}, // alias points back to KMouse4
	} {
		if got := KeyForName(c.name); got != c.want {
			t.Errorf("KeyForName(%q): %v want %v", c.name, got, c.want)
		}
	}
}

func TestKeyForName_Empty(t *testing.T) {
	if got := KeyForName(""); got != KUnknown {
		t.Errorf("empty: %v want KUnknown", got)
	}
}

func TestKeyForName_Unknown(t *testing.T) {
	if got := KeyForName("zzzznotakey"); got != KUnknown {
		t.Errorf("unknown: %v", got)
	}
}

// Round-trip via Name + KeyForName for every nameTable entry.
// Note: mwheelup / mwheeldown / shift / ctrl / alt deliberately map
// multiple Keys to one name, so the inverse picks the canonical
// table entry. Just assert the round-trip resolves to SOMETHING
// non-zero rather than the exact original.
func TestNameRoundTrip_AllTableEntries(t *testing.T) {
	for k := range nameTable {
		n := Name(k)
		if n == "" {
			t.Errorf("Name(%v) empty", k)
			continue
		}
		if got := KeyForName(n); got == KUnknown {
			t.Errorf("KeyForName(%q) unknown (from %v)", n, k)
		}
	}
}

// ASCII single-character round-trip for every printable byte.
func TestNameRoundTrip_AllPrintableASCII(t *testing.T) {
	for c := 32; c <= 126; c++ {
		// Skip ';' because it has a special name "semicolon" so the
		// round-trip is asymmetric (Name(';') returns ';', but
		// KeyForName(";") falls through to printable -> 59).
		// Actually both yield 59 either way -- so include it.
		k := Key(c)
		n := Name(k)
		if got := KeyForName(n); got != k {
			t.Errorf("ASCII %d (%q): name=%q parsed back as %v", c, string([]byte{byte(c)}), n, got)
		}
	}
}

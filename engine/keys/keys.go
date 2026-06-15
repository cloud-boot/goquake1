// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the cloud-boot/goquake1 authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package keys

import "fmt"

// Key is a keysym ID matching tyrquake's knum_t enum value-for-value.
type Key int

// Dest is the key-event routing tag (tyrquake's keydest_t).
type Dest int

const (
	DestGame    Dest = iota // forwarded to client's input layer
	DestConsole             // in-game console capturing the keystroke
	DestMessage             // in-game "say" message editor
	DestMenu                // main menu / pause menu navigation
	DestNone                // dropped (between subsystem handoffs)
)

// ASCII-aligned keys (values 8..127). Values match tyrquake's
// SDL-like layout because that's the layout cloud-boot's virtio-
// input HID translator maps onto.
const (
	KUnknown    Key = 0
	KBackspace  Key = 8
	KTab        Key = 9
	KClear      Key = 12
	KReturn     Key = 13
	KEnter      Key = 13 // alias of KReturn (tyrquake convention)
	KPause      Key = 19
	KEscape     Key = 27
	KSpace      Key = 32
	KExclaim    Key = 33
	KQuoteDbl   Key = 34
	KHash       Key = 35
	KDollar     Key = 36
	KPercent    Key = 37
	KAmpersand  Key = 38
	KQuote      Key = 39
	KLeftParen  Key = 40
	KRightParen Key = 41
	KAsterisk   Key = 42
	KPlus       Key = 43
	KComma      Key = 44
	KMinus      Key = 45
	KPeriod     Key = 46
	KSlash      Key = 47
	K0          Key = 48
	K1          Key = 49
	K2          Key = 50
	K3          Key = 51
	K4          Key = 52
	K5          Key = 53
	K6          Key = 54
	K7          Key = 55
	K8          Key = 56
	K9          Key = 57
	KColon      Key = 58
	KSemicolon  Key = 59
	KLess       Key = 60
	KEquals     Key = 61
	KGreater    Key = 62
	KQuestion   Key = 63
	KAt         Key = 64
	// 65..90 -- skip uppercase; alpha keys delivered as lowercase.
	KLeftBracket  Key = 91
	KBackslash    Key = 92
	KRightBracket Key = 93
	KCaret        Key = 94
	KUnderscore   Key = 95
	KBackquote    Key = 96
	// Lowercase alpha keys (the "skip uppercase, alpha keys passed
	// as lowercase" upstream rule).
	KA Key = 97
	KB Key = 98
	KC Key = 99
	KD Key = 100
	KE Key = 101
	KF Key = 102
	KG Key = 103
	KH Key = 104
	KI Key = 105
	KJ Key = 106
	KK Key = 107
	KL Key = 108
	KM Key = 109
	KN Key = 110
	KO Key = 111
	KP Key = 112
	KQ Key = 113
	KR Key = 114
	KS Key = 115
	KT Key = 116
	KU Key = 117
	KV Key = 118
	KW Key = 119
	KX Key = 120
	KY Key = 121
	KZ Key = 122
	KBraceLeft   Key = 123
	KBar         Key = 124
	KBraceRight  Key = 125
	KAsciiTilde  Key = 126
	KDel         Key = 127

	// Numeric keypad (256..272).
	KKp0        Key = 256
	KKp1        Key = 257
	KKp2        Key = 258
	KKp3        Key = 259
	KKp4        Key = 260
	KKp5        Key = 261
	KKp6        Key = 262
	KKp7        Key = 263
	KKp8        Key = 264
	KKp9        Key = 265
	KKpPeriod   Key = 266
	KKpDivide   Key = 267
	KKpMultiply Key = 268
	KKpMinus    Key = 269
	KKpPlus     Key = 270
	KKpEnter    Key = 271
	KKpEquals   Key = 272

	// Arrow / Home / End pad (273..281).
	KUpArrow    Key = 273
	KDownArrow  Key = 274
	KLeftArrow  Key = 275
	KRightArrow Key = 276
	KIns        Key = 277
	KHome       Key = 278
	KEnd        Key = 279
	KPgUp       Key = 280
	KPgDn       Key = 281

	// Function keys (282..296).
	KF1  Key = 282
	KF2  Key = 283
	KF3  Key = 284
	KF4  Key = 285
	KF5  Key = 286
	KF6  Key = 287
	KF7  Key = 288
	KF8  Key = 289
	KF9  Key = 290
	KF10 Key = 291
	KF11 Key = 292
	KF12 Key = 293
	KF13 Key = 294
	KF14 Key = 295
	KF15 Key = 296

	// Modifier keys (300..314).
	KNumLock   Key = 300
	KCapsLock  Key = 301
	KScrollLock Key = 302
	KRShift    Key = 303
	KLShift    Key = 304
	KRCtrl     Key = 305
	KLCtrl     Key = 306
	KRAlt      Key = 307
	KLAlt      Key = 308
	KRMeta     Key = 309
	KLMeta     Key = 310
	KLSuper    Key = 311
	KRSuper    Key = 312
	KMode      Key = 313
	KCompose   Key = 314

	// Misc function keys (315..322).
	KHelp   Key = 315
	KPrint  Key = 316
	KSysReq Key = 317
	KBreak  Key = 318
	KMenu   Key = 319
	KPower  Key = 320
	KEuro   Key = 321
	KUndo   Key = 322

	// Mouse buttons (sequential after the misc keys).
	KMouse1 Key = 323
	KMouse2 Key = 324
	KMouse3 Key = 325
	KMouse4 Key = 326
	KMouse5 Key = 327
	KMouse6 Key = 328
	KMouse7 Key = 329
	KMouse8 Key = 330

	// Joystick buttons (4 base + 32 aux).
	KJoy1 Key = 331
	KJoy2 Key = 332
	KJoy3 Key = 333
	KJoy4 Key = 334
)

// Aux buttons are auto-numbered after KJoy4.
const (
	KAux1 Key = 335 + iota
	KAux2
	KAux3
	KAux4
	KAux5
	KAux6
	KAux7
	KAux8
	KAux9
	KAux10
	KAux11
	KAux12
	KAux13
	KAux14
	KAux15
	KAux16
	KAux17
	KAux18
	KAux19
	KAux20
	KAux21
	KAux22
	KAux23
	KAux24
	KAux25
	KAux26
	KAux27
	KAux28
	KAux29
	KAux30
	KAux31
	KAux32

	// KLast is the upper bound -- everything past this is unused.
	KLast
)

// Backward-compat aliases tyrquake exposes via #define.
const (
	KShift      = KLShift
	KCtrl       = KLCtrl
	KAlt        = KLAlt
	KMWheelUp   = KMouse4
	KMWheelDown = KMouse5
)

// nameTable maps a key to its console-printable token (lowercase).
// Only keys with explicit token names land here; ASCII printables
// outside 32..126 + unmapped keys fall through to the generic
// "(unknown)" / "key###" fallbacks in Name().
var nameTable = map[Key]string{
	KTab:        "tab",
	KEnter:      "enter",
	KEscape:     "escape",
	KSpace:      "space",
	KBackspace:  "backspace",
	KPause:      "pause",
	KUpArrow:    "uparrow",
	KDownArrow:  "downarrow",
	KLeftArrow:  "leftarrow",
	KRightArrow: "rightarrow",
	KIns:        "ins",
	KHome:       "home",
	KEnd:        "end",
	KPgUp:       "pgup",
	KPgDn:       "pgdn",
	KF1:         "f1", KF2: "f2", KF3: "f3", KF4: "f4", KF5: "f5",
	KF6: "f6", KF7: "f7", KF8: "f8", KF9: "f9", KF10: "f10",
	KF11: "f11", KF12: "f12",
	KLShift: "shift", KRShift: "shift",
	KLCtrl: "ctrl", KRCtrl: "ctrl",
	KLAlt: "alt", KRAlt: "alt",
	KMouse1: "mouse1", KMouse2: "mouse2", KMouse3: "mouse3",
	KMouse4: "mwheelup", KMouse5: "mwheeldown",
	KMouse6: "mouse6", KMouse7: "mouse7", KMouse8: "mouse8",
	KJoy1: "joy1", KJoy2: "joy2", KJoy3: "joy3", KJoy4: "joy4",
	KSemicolon: "semicolon", // not stored as the literal ';' so the
	                          // bind parser doesn't trip on it
}

// Name returns the console-printable token for k (e.g. "enter",
// "mouse1", "a", "1"). Unmapped keys yield "(unknown)"; high
// keysym values that fall outside any name and outside ASCII yield
// "key###" using the numeric ID. tyrquake: Key_KeynumToString.
func Name(k Key) string {
	if k == KUnknown {
		return "(unknown)"
	}
	if k == KSemicolon {
		return "semicolon"
	}
	if k >= 32 && k <= 126 {
		// Printable ASCII -- the upstream prints the character itself,
		// stripping the special case for ';' above.
		return string([]byte{byte(k)})
	}
	if s, ok := nameTable[k]; ok {
		return s
	}
	return fmt.Sprintf("key%d", int(k))
}

// KeyForName parses a name back to its Key value. Returns KUnknown
// when name is unknown. ASCII single-character names yield the
// corresponding printable. tyrquake: Key_StringToKeynum.
func KeyForName(name string) Key {
	if name == "" {
		return KUnknown
	}
	if len(name) == 1 {
		c := name[0]
		if c >= 32 && c <= 126 {
			return Key(c)
		}
	}
	for k, s := range nameTable {
		if s == name {
			return k
		}
	}
	return KUnknown
}

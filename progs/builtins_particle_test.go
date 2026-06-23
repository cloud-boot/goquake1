// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package progs

import "testing"

func TestBuiltinParticle_NilVM_NoPanic(t *testing.T) {
	if err := BuiltinFnParticle(nil); err != nil {
		t.Fatalf("nil VM err = %v, want nil", err)
	}
}

func TestBuiltinParticle_NoSink_NoOp(t *testing.T) {
	vm := builtinVM()
	// Without SetParticleSink the builtin must succeed silently --
	// matches the random() pattern of "no source -> error", inverted
	// because particle() is a side-effect builtin (no return value
	// for QC code to react to).
	if err := BuiltinFnParticle(vm); err != nil {
		t.Fatalf("no-sink err = %v, want nil", err)
	}
}

func TestBuiltinParticle_DispatchesArgsToSink(t *testing.T) {
	vm := builtinVM()
	var got struct {
		origin [3]float32
		dir    [3]float32
		color  int
		count  int
		calls  int
	}
	vm.SetParticleSink(func(org, dir [3]float32, color int, count int) {
		got.origin = org
		got.dir = dir
		got.color = color
		got.count = count
		got.calls++
	})

	_ = vm.SetGlobalVector(OfsParm0, [3]float32{10, 20, 30})
	_ = vm.SetGlobalVector(OfsParm1, [3]float32{-1, 0, 1})
	_ = vm.SetGlobalFloat(OfsParm2, 0x47) // 71 -- blood band
	_ = vm.SetGlobalFloat(OfsParm3, 32)

	if err := BuiltinFnParticle(vm); err != nil {
		t.Fatalf("BuiltinFnParticle err = %v", err)
	}
	if got.calls != 1 {
		t.Fatalf("sink calls = %d, want 1", got.calls)
	}
	if got.origin != [3]float32{10, 20, 30} {
		t.Fatalf("origin = %v, want {10,20,30}", got.origin)
	}
	if got.dir != [3]float32{-1, 0, 1} {
		t.Fatalf("dir = %v, want {-1,0,1}", got.dir)
	}
	if got.color != 0x47 {
		t.Fatalf("color = %d, want 71", got.color)
	}
	if got.count != 32 {
		t.Fatalf("count = %d, want 32", got.count)
	}
}

func TestBuiltinParticle_NegativeCountClampedToZero(t *testing.T) {
	vm := builtinVM()
	var gotCount int = -1
	vm.SetParticleSink(func(_, _ [3]float32, _ int, count int) {
		gotCount = count
	})
	_ = vm.SetGlobalFloat(OfsParm3, -10)
	if err := BuiltinFnParticle(vm); err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotCount != 0 {
		t.Fatalf("negative count: got %d, want 0", gotCount)
	}
}

func TestBuiltinParticle_FloatColorTruncatedToInt(t *testing.T) {
	vm := builtinVM()
	var gotColor int
	vm.SetParticleSink(func(_, _ [3]float32, color int, _ int) {
		gotColor = color
	})
	// Float 70.9 -- C-style int cast truncates toward zero -> 70.
	_ = vm.SetGlobalFloat(OfsParm2, 70.9)
	if err := BuiltinFnParticle(vm); err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotColor != 70 {
		t.Fatalf("color trunc: got %d, want 70", gotColor)
	}
}

func TestRegisterEffectsBuiltins_WiresParticle(t *testing.T) {
	vm := builtinVM()
	vm.RegisterEffectsBuiltins()
	fn, ok := vm.builtins[BuiltinParticle]
	if !ok {
		t.Fatalf("BuiltinParticle (%d) not registered", BuiltinParticle)
	}
	// And the registered fn is callable + matches the named export.
	if err := fn(vm); err != nil {
		t.Fatalf("registered particle fn err = %v", err)
	}
}

func TestSetParticleSink_NilUnwires(t *testing.T) {
	vm := builtinVM()
	called := 0
	vm.SetParticleSink(func(_, _ [3]float32, _ int, _ int) { called++ })
	_ = BuiltinFnParticle(vm)
	if called != 1 {
		t.Fatalf("setup sink not invoked: called=%d", called)
	}
	vm.SetParticleSink(nil)
	if err := BuiltinFnParticle(vm); err != nil {
		t.Fatalf("unwired err = %v", err)
	}
	if called != 1 {
		t.Fatalf("sink invoked after unwire: called=%d", called)
	}
}

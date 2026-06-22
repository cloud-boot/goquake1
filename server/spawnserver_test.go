// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/protocol"
)

// --- test fixtures --------------------------------------------------------

// fakeWorld is the test stand-in for *world.World; it records the
// (mins, maxs) it was last Clear'd with so tests can assert against
// the worldmodel bounds.
type fakeWorld struct {
	cleared bool
	mins    [3]float32
	maxs    [3]float32
}

func (w *fakeWorld) Clear(mins, maxs [3]float32) {
	w.cleared = true
	w.mins = mins
	w.maxs = maxs
}

// buildSpawnBSP synthesises a minimal valid Q1 BSP for SpawnServer
// tests. It mirrors model.buildMinimalBSPForBrushModel but adds an
// entities lump (the SpawnServer-only difference). The entities
// payload defaults to a one-entity worldspawn block; callers can
// override via entityBlob.
func buildSpawnBSP(t *testing.T, entityBlob string, modelCount int) []byte {
	t.Helper()
	if modelCount < 1 {
		modelCount = 1
	}

	planes := []bspfile.Plane{
		{Normal: [3]float32{1, 0, 0}, Dist: 0, Type: bspfile.PlaneX},
	}
	nodes := []bspfile.Node{
		{PlaneNum: 0, Children: [2]int16{^int16(0), ^int16(1)}},
	}
	leafs := []bspfile.Leaf{
		{Contents: bspfile.ContentsEmpty},
		{Contents: bspfile.ContentsSolid},
	}
	clipnodes := []bspfile.ClipNode{
		{PlaneNum: 0, Children: [2]int16{bspfile.ContentsEmpty, bspfile.ContentsSolid}},
	}
	models := make([]bspfile.Model, modelCount)
	for i := range models {
		models[i] = bspfile.Model{
			Mins:     [3]float32{-100 - float32(i), -100 - float32(i), -100 - float32(i)},
			Maxs:     [3]float32{100 + float32(i), 100 + float32(i), 100 + float32(i)},
			Headnode: [bspfile.MaxMapHulls]int32{0, 0, 0, 0},
		}
	}

	pb := &bytes.Buffer{}
	for _, p := range planes {
		_ = binary.Write(pb, binary.LittleEndian, p.Normal[0])
		_ = binary.Write(pb, binary.LittleEndian, p.Normal[1])
		_ = binary.Write(pb, binary.LittleEndian, p.Normal[2])
		_ = binary.Write(pb, binary.LittleEndian, p.Dist)
		_ = binary.Write(pb, binary.LittleEndian, p.Type)
	}
	nb := &bytes.Buffer{}
	for _, n := range nodes {
		_ = binary.Write(nb, binary.LittleEndian, n.PlaneNum)
		_ = binary.Write(nb, binary.LittleEndian, n.Children[0])
		_ = binary.Write(nb, binary.LittleEndian, n.Children[1])
		for j := 0; j < 3; j++ {
			_ = binary.Write(nb, binary.LittleEndian, n.Mins[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(nb, binary.LittleEndian, n.Maxs[j])
		}
		_ = binary.Write(nb, binary.LittleEndian, n.FirstFace)
		_ = binary.Write(nb, binary.LittleEndian, n.NumFaces)
	}
	lb := &bytes.Buffer{}
	for _, l := range leafs {
		_ = binary.Write(lb, binary.LittleEndian, l.Contents)
		_ = binary.Write(lb, binary.LittleEndian, l.VisOfs)
		for j := 0; j < 3; j++ {
			_ = binary.Write(lb, binary.LittleEndian, l.Mins[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(lb, binary.LittleEndian, l.Maxs[j])
		}
		_ = binary.Write(lb, binary.LittleEndian, l.FirstMarkSurface)
		_ = binary.Write(lb, binary.LittleEndian, l.NumMarkSurfaces)
		lb.Write(l.AmbientLevel[:])
	}
	cnb := &bytes.Buffer{}
	for _, c := range clipnodes {
		_ = binary.Write(cnb, binary.LittleEndian, c.PlaneNum)
		_ = binary.Write(cnb, binary.LittleEndian, c.Children[0])
		_ = binary.Write(cnb, binary.LittleEndian, c.Children[1])
	}
	mb := &bytes.Buffer{}
	for _, m := range models {
		for j := 0; j < 3; j++ {
			_ = binary.Write(mb, binary.LittleEndian, m.Mins[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(mb, binary.LittleEndian, m.Maxs[j])
		}
		for j := 0; j < 3; j++ {
			_ = binary.Write(mb, binary.LittleEndian, m.Origin[j])
		}
		for j := 0; j < bspfile.MaxMapHulls; j++ {
			_ = binary.Write(mb, binary.LittleEndian, m.Headnode[j])
		}
		_ = binary.Write(mb, binary.LittleEndian, m.VisLeafs)
		_ = binary.Write(mb, binary.LittleEndian, m.FirstFace)
		_ = binary.Write(mb, binary.LittleEndian, m.NumFaces)
	}

	const headerSize = 4 + 15*8
	body := &bytes.Buffer{}

	type lumpInfo struct {
		kind   bspfile.LumpKind
		data   []byte
		offset int32
	}
	entBytes := append([]byte(entityBlob), 0)
	lumps := []lumpInfo{
		{kind: bspfile.LumpEntities, data: entBytes},
		{kind: bspfile.LumpPlanes, data: pb.Bytes()},
		{kind: bspfile.LumpNodes, data: nb.Bytes()},
		{kind: bspfile.LumpLeafs, data: lb.Bytes()},
		{kind: bspfile.LumpClipnodes, data: cnb.Bytes()},
		{kind: bspfile.LumpModels, data: mb.Bytes()},
	}
	offsetByKind := map[bspfile.LumpKind]int32{}
	lenByKind := map[bspfile.LumpKind]int32{}
	for i := range lumps {
		lumps[i].offset = int32(headerSize) + int32(body.Len())
		body.Write(lumps[i].data)
		offsetByKind[lumps[i].kind] = lumps[i].offset
		lenByKind[lumps[i].kind] = int32(len(lumps[i].data))
	}

	hdr := &bytes.Buffer{}
	_ = binary.Write(hdr, binary.LittleEndian, int32(bspfile.Version29))
	for k := bspfile.LumpKind(0); int(k) < bspfile.HeaderLumps; k++ {
		_ = binary.Write(hdr, binary.LittleEndian, offsetByKind[k])
		_ = binary.Write(hdr, binary.LittleEndian, lenByKind[k])
	}
	return append(hdr.Bytes(), body.Bytes()...)
}

// progsForSpawn builds a Progs stub with two fields the entity-spawn
// pass uses: "classname" (EvString) + "origin" (EvVector). The string
// table seeds an empty-string at offset 0 (matches tyrquake's
// pr_strings layout).
func progsForSpawn() *progs.Progs {
	strs := []byte{0}
	add := func(s string) int32 {
		ofs := int32(len(strs))
		strs = append(strs, []byte(s)...)
		strs = append(strs, 0)
		return ofs
	}
	classnameName := add("classname")
	originName := add("origin")
	return &progs.Progs{
		Header:  progs.Header{EntityFields: 8},
		Strings: strs,
		FieldDefs: []progs.Def{
			{Type: uint16(progs.EvString), Ofs: 1, SName: classnameName},
			{Type: uint16(progs.EvVector), Ofs: 2, SName: originName},
		},
	}
}

// resolverFor returns a FileResolver that hands back data for any
// requested name. Tests that need name-aware behaviour use a custom
// closure.
func resolverFor(data []byte) FileResolver {
	return func(name string) (int64, io.ReaderAt, error) {
		return int64(len(data)), bytes.NewReader(data), nil
	}
}

// makeDeps assembles a happy-path SpawnDeps with all required fields
// populated. Callers override individual fields per test.
func makeDeps(t *testing.T, bspBytes []byte) SpawnDeps {
	t.Helper()
	return SpawnDeps{
		Cache:    model.NewCache(),
		Resolver: resolverFor(bspBytes),
		Progs:    progsForSpawn(),
		Static:   NewStatic(4),
		World:    &fakeWorld{},
		Interner: func(s string) int32 { return 0 },
	}
}

// --- happy path -----------------------------------------------------------

func TestSpawnServer_HappyPath(t *testing.T) {
	bspBytes := buildSpawnBSP(t, `{ "classname" "worldspawn" "origin" "0 0 0" }`, 1)
	deps := makeDeps(t, bspBytes)

	spawnCalls := 0
	deps.SpawnFn = func(ent *progs.Edict, classname string) {
		spawnCalls++
		if classname != "worldspawn" {
			t.Errorf("spawn classname=%q want worldspawn", classname)
		}
	}

	s := NewServer()
	if err := s.SpawnServer("test", protocol.VersionNQ, deps); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}

	if !s.Active {
		t.Error("Server.Active should be true after SpawnServer")
	}
	if s.State != StateActive {
		t.Errorf("Server.State got %d want StateActive", s.State)
	}
	if s.WorldModel == nil {
		t.Error("Server.WorldModel should be set")
	}
	if s.ModelPrecache[0] != "" {
		t.Errorf("ModelPrecache[0] got %q want empty", s.ModelPrecache[0])
	}
	if s.ModelPrecache[1] != "maps/test.bsp" {
		t.Errorf("ModelPrecache[1] got %q want maps/test.bsp", s.ModelPrecache[1])
	}
	if s.Models[1] == nil {
		t.Error("Models[1] should reference the worldmodel")
	}
	if s.NumEdicts != deps.Static.MaxClients+1 {
		t.Errorf("NumEdicts got %d want %d", s.NumEdicts, deps.Static.MaxClients+1)
	}
	// Area tree must have been cleared with the worldmodel bounds.
	fw := deps.World.(*fakeWorld)
	if !fw.cleared {
		t.Error("World.Clear was not invoked")
	}
	if fw.mins != ([3]float32{-100, -100, -100}) || fw.maxs != ([3]float32{100, 100, 100}) {
		t.Errorf("World.Clear bounds got (%v, %v) want ((-100,-100,-100), (100,100,100))", fw.mins, fw.maxs)
	}
	// SpawnFn fires once for the single entity in the lump.
	if spawnCalls != 1 {
		t.Errorf("SpawnFn calls got %d want 1", spawnCalls)
	}
	// Every edict slot must be backed by a real *Edict.
	if s.Edicts[0] == nil || s.Edicts[s.MaxEdicts-1] == nil {
		t.Error("Edicts pool must be fully populated")
	}
}

// Multi-submodel BSP: every submodel slot beyond the worldmodel must
// be precached under its "*N" alias.
func TestSpawnServer_PopulatesSubmodels(t *testing.T) {
	bspBytes := buildSpawnBSP(t, `{ "classname" "worldspawn" }`, 3) // world + 2 submodels
	deps := makeDeps(t, bspBytes)
	s := NewServer()
	if err := s.SpawnServer("test", protocol.VersionNQ, deps); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if s.ModelPrecache[2] != "*1" {
		t.Errorf("ModelPrecache[2] got %q want *1", s.ModelPrecache[2])
	}
	if s.ModelPrecache[3] != "*2" {
		t.Errorf("ModelPrecache[3] got %q want *2", s.ModelPrecache[3])
	}
	if s.Models[2] == nil || s.Models[3] == nil {
		t.Error("submodel slots Models[2..3] should be non-nil")
	}
}

// Multi-entity lump: each entity past the world must land in a new
// slot allocated by edictAt's NumEdicts++ branch. Hits the i != 0
// path that the single-entity test does not exercise.
func TestSpawnServer_AllocatesPostWorldSlots(t *testing.T) {
	bspBytes := buildSpawnBSP(t, `{ "classname" "worldspawn" }`+
		`{ "classname" "info_player_start" "origin" "1 2 3" }`+
		`{ "classname" "monster_army" "origin" "10 20 30" }`, 1)
	deps := makeDeps(t, bspBytes)
	s := NewServer()
	if err := s.SpawnServer("test", protocol.VersionNQ, deps); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	// Two non-world entities; reserve = MaxClients+1; allocator
	// should bump NumEdicts twice past the reserve.
	want := deps.Static.MaxClients + 1 + 2
	if s.NumEdicts != want {
		t.Errorf("NumEdicts got %d want %d (reserve+2 post-world entities)", s.NumEdicts, want)
	}
}

// MaxEdicts cap: when entities exceed the cap, SpawnEntities surfaces
// the nil-edict from edictAt as an error. Hits the slot >= MaxEdicts
// guard in edictAt.
func TestSpawnServer_OverMaxEdictsReturnsNil(t *testing.T) {
	bspBytes := buildSpawnBSP(t, `{ "classname" "worldspawn" }`+
		`{ "classname" "info_player_start" "origin" "1 2 3" }`+
		`{ "classname" "monster_army" "origin" "10 20 30" }`, 1)
	deps := makeDeps(t, bspBytes)
	s := NewServer()
	s.MaxEdicts = deps.Static.MaxClients + 2 // first non-world fits, second doesn't
	err := s.SpawnServer("test", protocol.VersionNQ, deps)
	if err == nil {
		t.Fatalf("SpawnServer over-cap: got nil err, want failure")
	}
}

// SpawnFn nil: entities still parse + assign but no spawn hook fires.
func TestSpawnServer_NilSpawnFn(t *testing.T) {
	bspBytes := buildSpawnBSP(t, `{ "classname" "worldspawn" }`, 1)
	deps := makeDeps(t, bspBytes)
	deps.SpawnFn = nil
	s := NewServer()
	if err := s.SpawnServer("test", protocol.VersionNQ, deps); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if !s.Active {
		t.Error("Server.Active should be true with nil SpawnFn")
	}
}

// Empty entities lump: ParseEntities returns nil + SpawnEntities is a
// no-op.
func TestSpawnServer_EmptyEntitiesLump(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "", 1)
	deps := makeDeps(t, bspBytes)
	s := NewServer()
	if err := s.SpawnServer("test", protocol.VersionNQ, deps); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if !s.Active {
		t.Error("Server.Active should be true with empty entities")
	}
}

// --- ErrSpawnServerNilDeps branches --------------------------------------

func TestSpawnServer_NilCache(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "", 1)
	deps := makeDeps(t, bspBytes)
	deps.Cache = nil
	s := NewServer()
	err := s.SpawnServer("test", protocol.VersionNQ, deps)
	if !errors.Is(err, ErrSpawnServerNilDeps) {
		t.Errorf("got %v want ErrSpawnServerNilDeps", err)
	}
}

func TestSpawnServer_NilResolver(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "", 1)
	deps := makeDeps(t, bspBytes)
	deps.Resolver = nil
	s := NewServer()
	err := s.SpawnServer("test", protocol.VersionNQ, deps)
	if !errors.Is(err, ErrSpawnServerNilDeps) {
		t.Errorf("got %v want ErrSpawnServerNilDeps", err)
	}
}

func TestSpawnServer_NilProgs(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "", 1)
	deps := makeDeps(t, bspBytes)
	deps.Progs = nil
	s := NewServer()
	err := s.SpawnServer("test", protocol.VersionNQ, deps)
	if !errors.Is(err, ErrSpawnServerNilDeps) {
		t.Errorf("got %v want ErrSpawnServerNilDeps", err)
	}
}

func TestSpawnServer_NilStatic(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "", 1)
	deps := makeDeps(t, bspBytes)
	deps.Static = nil
	s := NewServer()
	err := s.SpawnServer("test", protocol.VersionNQ, deps)
	if !errors.Is(err, ErrSpawnServerNilDeps) {
		t.Errorf("got %v want ErrSpawnServerNilDeps", err)
	}
}

func TestSpawnServer_NilWorld(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "", 1)
	deps := makeDeps(t, bspBytes)
	deps.World = nil
	s := NewServer()
	err := s.SpawnServer("test", protocol.VersionNQ, deps)
	if !errors.Is(err, ErrSpawnServerNilDeps) {
		t.Errorf("got %v want ErrSpawnServerNilDeps", err)
	}
}

// --- Reset propagation ---------------------------------------------------

func TestSpawnServer_EmptyMapName(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "", 1)
	deps := makeDeps(t, bspBytes)
	s := NewServer()
	err := s.SpawnServer("", protocol.VersionNQ, deps)
	if !errors.Is(err, ErrEmptyMapName) {
		t.Errorf("got %v want ErrEmptyMapName", err)
	}
}

// --- worldmodel error paths ----------------------------------------------

// Resolver error propagates with the worldmodel path wrapped.
func TestSpawnServer_ResolverError(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "", 1)
	deps := makeDeps(t, bspBytes)
	sentinel := errors.New("disk on fire")
	deps.Resolver = func(name string) (int64, io.ReaderAt, error) {
		return 0, nil, sentinel
	}
	s := NewServer()
	err := s.SpawnServer("test", protocol.VersionNQ, deps)
	if !errors.Is(err, sentinel) {
		t.Errorf("got %v want sentinel", err)
	}
}

// Resolver hands back a non-brush model (an alias .mdl with the IDPO
// magic) -> ErrSpawnServerNotBrush.
func TestSpawnServer_NotBrush(t *testing.T) {
	// IDPO magic + enough bytes to fail past model.Load's header
	// switch but NOT pass mdl.Load. We expect the loader to error
	// rather than return a successful non-brush -- check both
	// possibilities and ensure SpawnServer surfaces one of them.
	bogus := append([]byte("IDPO"), make([]byte, 256)...)
	deps := makeDeps(t, bogus)
	s := NewServer()
	err := s.SpawnServer("test", protocol.VersionNQ, deps)
	if err == nil {
		t.Fatal("expected error for non-brush worldmodel")
	}
	// Either NotBrush (if mdl.Load accepted it) or a wrapped
	// loader-failure -- both are valid surfaces for the
	// non-brush case.
	if !errors.Is(err, ErrSpawnServerNotBrush) && !errors.Is(err, model.ErrLoaderFail) {
		t.Errorf("got %v want ErrSpawnServerNotBrush or model.ErrLoaderFail", err)
	}
}

// Cache-pre-seed with a hand-built non-brush *Model exercises the
// "wrong kind" branch directly (the Mod_ForName cache short-circuit
// bypasses the resolver, letting us inject the typed value).
func TestSpawnServer_CachedNonBrush(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "", 1)
	deps := makeDeps(t, bspBytes)
	// Stuff a sprite-kind entry under the world map name; the cache
	// hit short-circuits the resolver.
	deps.Cache.Load("maps/test.bsp", nil, 0) // returns ErrNotInCache, ignored
	// Real load: poison via the resolver returning a sprite header.
	// Easier path: pre-load a valid brush, then mutate Kind via
	// direct field write -- the cache returns the same *Model.
	bm, err := LoadBytesIntoCache(deps.Cache, "maps/test.bsp", bspBytes)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	bm.Kind = model.KindAlias // simulate kind drift
	s := NewServer()
	err = s.SpawnServer("test", protocol.VersionNQ, deps)
	if !errors.Is(err, ErrSpawnServerNotBrush) {
		t.Errorf("got %v want ErrSpawnServerNotBrush", err)
	}
}

// Cached entry that's KindBrush but with a nil Brush pointer also
// trips the ErrSpawnServerNotBrush guard (the OR-condition's right
// disjunct).
func TestSpawnServer_CachedBrushNilBSP(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "", 1)
	deps := makeDeps(t, bspBytes)
	bm, err := LoadBytesIntoCache(deps.Cache, "maps/test.bsp", bspBytes)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	bm.Brush = nil // simulate a tainted cache entry
	s := NewServer()
	err = s.SpawnServer("test", protocol.VersionNQ, deps)
	if !errors.Is(err, ErrSpawnServerNotBrush) {
		t.Errorf("got %v want ErrSpawnServerNotBrush", err)
	}
}

// LoadBrush failure: corrupt the models lump (length off by 1) so
// model.LoadBrush's bspfile.File.Models() call returns
// ErrSectionMisaligned. SpawnServer wraps it under the "build hulls"
// prefix.
func TestSpawnServer_LoadBrushError(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "", 1)
	// Corrupt the Models lump length (LumpModels is index 14 in the
	// LumpKind enum -- the last lump entry in the header).
	modelsLumpOff := 4 + 14*8
	curLen := int32(binary.LittleEndian.Uint32(bspBytes[modelsLumpOff+4 : modelsLumpOff+8]))
	binary.LittleEndian.PutUint32(bspBytes[modelsLumpOff+4:modelsLumpOff+8], uint32(curLen-1))
	deps := makeDeps(t, bspBytes)
	s := NewServer()
	err := s.SpawnServer("test", protocol.VersionNQ, deps)
	if err == nil {
		t.Fatal("expected error for corrupt models lump")
	}
	// The error chain includes ErrSectionMisaligned from bspfile.
	if !errors.Is(err, bspfile.ErrSectionMisaligned) {
		t.Errorf("got %v want bspfile.ErrSectionMisaligned in chain", err)
	}
}

// --- entity parse + assign error paths -----------------------------------

// ParseEntities error: feed a malformed entities lump (a stray '}'
// without a prior '{') -> ErrUnmatchedClose.
func TestSpawnServer_ParseEntitiesError(t *testing.T) {
	bspBytes := buildSpawnBSP(t, "}", 1)
	deps := makeDeps(t, bspBytes)
	s := NewServer()
	err := s.SpawnServer("test", protocol.VersionNQ, deps)
	if err == nil {
		t.Fatal("expected error for malformed entities lump")
	}
}

// AssignFields error: omit the Interner so the EvString "classname"
// assignment fails with ErrNoInterner. SpawnEntities surfaces the
// FIRST per-entity error, which SpawnServer wraps as "spawn
// entities".
func TestSpawnServer_AssignFieldsError(t *testing.T) {
	bspBytes := buildSpawnBSP(t, `{ "classname" "worldspawn" }`, 1)
	deps := makeDeps(t, bspBytes)
	deps.Interner = nil // strings fail to write
	s := NewServer()
	err := s.SpawnServer("test", protocol.VersionNQ, deps)
	if err == nil {
		t.Fatal("expected error from AssignFields without interner")
	}
	if !errors.Is(err, progs.ErrNoInterner) {
		t.Errorf("got %v want progs.ErrNoInterner in chain", err)
	}
}

// --- Arena publication ----------------------------------------------------

// Server.Arena is populated after a successful SpawnServer + matches
// the cap the SpawnDeps + Server.MaxEdicts agree on. The arena lives
// on Server.Arena so embedders that don't pass an OnArenaReady hook
// can still pick it up post-spawn.
func TestSpawnServer_PublishesArena(t *testing.T) {
	bspBytes := buildSpawnBSP(t, `{ "classname" "worldspawn" }`, 1)
	deps := makeDeps(t, bspBytes)
	s := NewServer()
	if err := s.SpawnServer("test", protocol.VersionNQ, deps); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if s.Arena == nil {
		t.Fatal("Server.Arena should be non-nil after SpawnServer")
	}
	if got := s.Arena.Cap(); got != s.MaxEdicts {
		t.Errorf("Arena.Cap got %d want MaxEdicts (%d)", got, s.MaxEdicts)
	}
	// The arena's slot 0 must be the same *Edict as Server.Edicts[0]
	// -- the per-slot pointer aliasing the spawn-pass relies on.
	e0, err := s.Arena.Get(0)
	if err != nil {
		t.Fatalf("Arena.Get(0): %v", err)
	}
	if e0 != s.Edicts[0] {
		t.Error("Arena.Get(0) should alias Server.Edicts[0]")
	}
}

// OnArenaReady fires once, AFTER the arena is allocated + BEFORE the
// entity-spawn pass dispatches SpawnFn. The order matters: production
// embedders use the hook to wire vm.SetArena so the spawn-time entity-
// pointer opcodes resolve. This test asserts both invariants by
// recording the relative call order.
func TestSpawnServer_OnArenaReadyFiresBeforeSpawnFn(t *testing.T) {
	bspBytes := buildSpawnBSP(t, `{ "classname" "worldspawn" }`, 1)
	deps := makeDeps(t, bspBytes)
	var order []string
	var seenArena *progs.EdictArena
	deps.OnArenaReady = func(a *progs.EdictArena) {
		order = append(order, "arena")
		seenArena = a
	}
	deps.SpawnFn = func(ent *progs.Edict, classname string) {
		order = append(order, "spawn:"+classname)
	}
	s := NewServer()
	if err := s.SpawnServer("test", protocol.VersionNQ, deps); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if len(order) < 2 {
		t.Fatalf("order got %v want [arena, spawn:worldspawn]", order)
	}
	if order[0] != "arena" {
		t.Errorf("first call got %q want %q", order[0], "arena")
	}
	if order[1] != "spawn:worldspawn" {
		t.Errorf("second call got %q want %q", order[1], "spawn:worldspawn")
	}
	if seenArena == nil {
		t.Fatal("OnArenaReady should receive a non-nil arena")
	}
	if seenArena != s.Arena {
		t.Error("OnArenaReady arena should be the one stashed on Server.Arena")
	}
}

// Nil OnArenaReady is the default: SpawnServer must not panic + the
// arena still lands on Server.Arena.
func TestSpawnServer_NilOnArenaReady(t *testing.T) {
	bspBytes := buildSpawnBSP(t, `{ "classname" "worldspawn" }`, 1)
	deps := makeDeps(t, bspBytes)
	deps.OnArenaReady = nil
	s := NewServer()
	if err := s.SpawnServer("test", protocol.VersionNQ, deps); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if s.Arena == nil {
		t.Error("Server.Arena should be non-nil even with nil OnArenaReady")
	}
}

// --- AreaClearer satisfaction --------------------------------------------

// Build-time check that *fakeWorld -- and by structural contract,
// *world.World -- satisfies the AreaClearer interface.
var _ AreaClearer = (*fakeWorld)(nil)

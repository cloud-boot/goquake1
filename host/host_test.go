// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package host

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/go-quake1/engine/bspfile"
	"github.com/go-quake1/engine/model"
	"github.com/go-quake1/engine/progs"
	"github.com/go-quake1/engine/protocol"
	"github.com/go-quake1/engine/server"
	"github.com/go-quake1/engine/sizebuf"
)

// --- test fixtures --------------------------------------------------------

// addStr appends s + a trailing NUL to strs and returns the offset s
// was written at. The first byte stays the upstream's empty-string
// sentinel (offset 0 is reserved).
func addStr(strs *[]byte, s string) int32 {
	ofs := int32(len(*strs))
	*strs = append(*strs, []byte(s)...)
	*strs = append(*strs, 0)
	return ofs
}

// progsForHost builds a Progs stub with:
//   - "classname" + "origin" fields (SpawnServer's spawn pass uses them)
//   - "movetype" + "solid" fields (RunPhysics reads them per-edict)
//   - "nextthink" + "think" fields (RunThink reads them)
//   - "self" + "other" + "time" globals (the thinkCaller bridge writes them)
//   - Functions[0] = null + Functions[1] = a "store 42 to OFS_RETURN" body
//
// EntityFields=8 gives 32 bytes per edict -- enough for the 6 fields
// we define here at distinct offsets.
func progsForHost() *progs.Progs {
	strs := []byte{0}
	classnameName := addStr(&strs, "classname")
	originName := addStr(&strs, "origin")
	movetypeName := addStr(&strs, "movetype")
	solidName := addStr(&strs, "solid")
	nextthinkName := addStr(&strs, "nextthink")
	thinkName := addStr(&strs, "think")
	selfName := addStr(&strs, "self")
	otherName := addStr(&strs, "other")
	timeName := addStr(&strs, "time")
	constName := addStr(&strs, "k42")

	// Globals: pool needs to be large enough for OfsReturn (1..3) +
	// the named globals' slots + a constant slot holding 42.0.
	const numGlobals = 64
	globals := make([]byte, numGlobals*4)
	const k42Slot = 30
	binary.LittleEndian.PutUint32(globals[k42Slot*4:k42Slot*4+4], math.Float32bits(42))

	return &progs.Progs{
		Header:  progs.Header{EntityFields: 8},
		Strings: strs,
		FieldDefs: []progs.Def{
			{Type: uint16(progs.EvString), Ofs: 1, SName: classnameName},
			{Type: uint16(progs.EvVector), Ofs: 2, SName: originName},
			{Type: uint16(progs.EvFloat), Ofs: 5, SName: movetypeName},
			{Type: uint16(progs.EvFloat), Ofs: 6, SName: solidName},
			{Type: uint16(progs.EvFloat), Ofs: 7, SName: nextthinkName},
			{Type: uint16(progs.EvFunction), Ofs: 0, SName: thinkName},
		},
		GlobalDefs: []progs.Def{
			{Type: uint16(progs.EvEntity), Ofs: 40, SName: selfName},
			{Type: uint16(progs.EvEntity), Ofs: 41, SName: otherName},
			{Type: uint16(progs.EvFloat), Ofs: 42, SName: timeName},
			{Type: uint16(progs.EvFloat), Ofs: k42Slot, SName: constName},
		},
		Globals: globals,
		// Functions: index 0 is the null slot; index 1 returns the
		// k42 constant via OP_RETURN (which copies A..A+2 into
		// OfsReturn). Statement layout: Statements[0]=OP_DONE (the
		// pre-roll the runner skips), Statements[1] = OP_RETURN with
		// A = k42Slot.
		Statements: []progs.Statement{
			{Op: progs.OP_DONE},
			{Op: progs.OP_RETURN, A: int16(k42Slot)},
		},
		Functions: []progs.Function{
			{FirstStatement: 0, SName: 0},
			{FirstStatement: 1, SName: 0, NumParms: 0, Locals: 0, ParmStart: 0},
		},
	}
}

// resolverFor returns a FileResolver that hands back data for any
// requested name.
func resolverFor(data []byte) server.FileResolver {
	return func(name string) (int64, io.ReaderAt, error) {
		return int64(len(data)), bytes.NewReader(data), nil
	}
}

// buildHostBSP synthesises a minimal valid Q1 BSP for SpawnServer
// tests. Copy of server/spawnserver_test.go's buildSpawnBSP --
// kept local to keep the host package independent of the server's
// test helpers.
func buildHostBSP(t *testing.T, entityBlob string, modelCount int) []byte {
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
			Mins:     [3]float32{-100, -100, -100},
			Maxs:     [3]float32{100, 100, 100},
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

// makeHost is the happy-path NewHost wrapper: a fresh progs + VM +
// cache + the BSP-bytes resolver. Returns the host and the *Progs
// stashed in it so tests can mutate fields/globals post-construction.
func makeHost(t *testing.T, bspBytes []byte, maxClients int) (*Host, *progs.Progs) {
	t.Helper()
	p := progsForHost()
	vm := progs.NewVM(p)
	cache := model.NewCache()
	resolver := resolverFor(bspBytes)
	h, err := NewHost(vm, cache, resolver, maxClients)
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	h.SetProgs(p)
	return h, p
}

// --- NewHost: nil-dep guards -----------------------------------------------

func TestNewHost_NilVM(t *testing.T) {
	_, err := NewHost(nil, model.NewCache(), resolverFor(nil), 1)
	if !errors.Is(err, ErrNilDep) {
		t.Errorf("got %v want ErrNilDep", err)
	}
}

func TestNewHost_NilCache(t *testing.T) {
	vm := progs.NewVM(progsForHost())
	_, err := NewHost(vm, nil, resolverFor(nil), 1)
	if !errors.Is(err, ErrNilDep) {
		t.Errorf("got %v want ErrNilDep", err)
	}
}

func TestNewHost_NilResolver(t *testing.T) {
	vm := progs.NewVM(progsForHost())
	_, err := NewHost(vm, model.NewCache(), nil, 1)
	if !errors.Is(err, ErrNilDep) {
		t.Errorf("got %v want ErrNilDep", err)
	}
}

// --- NewHost: happy path ---------------------------------------------------

func TestNewHost_PopulatesFields(t *testing.T) {
	p := progsForHost()
	vm := progs.NewVM(p)
	cache := model.NewCache()
	resolver := resolverFor(nil)
	h, err := NewHost(vm, cache, resolver, 4)
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	if h.Server == nil || h.Static == nil || h.VM == nil || h.World == nil {
		t.Error("NewHost should populate Server/Static/VM/World")
	}
	if h.Cache != cache {
		t.Error("Cache not set")
	}
	if h.Resolver == nil {
		t.Error("Resolver not set")
	}
	if h.FrameTime != DefaultFrameTime {
		t.Errorf("FrameTime got %v want %v", h.FrameTime, DefaultFrameTime)
	}
	if h.NowFn == nil {
		t.Error("NowFn must default to a non-nil clock")
	}
	if got := h.NowFn(); got <= 0 {
		t.Errorf("defaultNowFn returned %v; want > 0", got)
	}
	if h.Static.MaxClients != 4 {
		t.Errorf("MaxClients got %d want 4", h.Static.MaxClients)
	}
}

// maxClients <= 0 falls back to 1 (single local-client minimum).
func TestNewHost_ZeroClientsFallsBackToOne(t *testing.T) {
	vm := progs.NewVM(progsForHost())
	h, err := NewHost(vm, model.NewCache(), resolverFor(nil), 0)
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	if h.Static.MaxClients != 1 {
		t.Errorf("MaxClients got %d want 1", h.Static.MaxClients)
	}
}

// --- Frame --------------------------------------------------------------

// Frame on an inactive (un-spawned) server is a no-op.
func TestFrame_InactiveServerNoOp(t *testing.T) {
	h, _ := makeHost(t, nil, 1)
	if err := h.Frame(0.05); err != nil {
		t.Errorf("Frame on inactive server: %v want nil", err)
	}
	if h.Server.Time != 0 {
		t.Errorf("sv.time advanced despite inactive server: %v", h.Server.Time)
	}
}

// Frame on a freshly-spawned server: runs without panic, returns nil.
// The reserved client slots (1..MaxClients) have empty Fields blocks
// but RunPhysics reads movetype/solid which are at offsets 5/6 -- the
// 8-slot EntityFields layout makes that work + every slot starts
// movetype=0 (None) solid=0 (Not) -> skipped (free-entity rule).
func TestFrame_CleanTickReturnsNil(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 2)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	tBefore := h.Server.Time
	if err := h.Frame(0.05); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if h.Server.Time <= tBefore {
		t.Errorf("sv.time did not advance: before=%v after=%v", tBefore, h.Server.Time)
	}
}

// Frame propagates RunPhysics errors. Strategy: spawn the server,
// then corrupt the progs so the per-edict ReadFloat("movetype")
// surfaces ErrFieldNotFound. We replace progsRef + every edict's
// Fields slice in one shot to a Progs that has no movetype field --
// the per-physics ReadFloat on any non-free slot then errors.
func TestFrame_PropagatesRunPhysicsError(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	// Replace the bound Progs with one that has NO field defs at all
	// -- RunPhysics's ReadFloat("movetype") will surface
	// ErrFieldNotFound on the first non-nil edict (slot 0, the world).
	// Bump NumEdicts to 2 so the dispatcher walks slot 0 + 1, and the
	// world's empty-field-defs progs guarantees a movetype-not-found.
	stripped := &progs.Progs{
		Header:  progs.Header{EntityFields: 8},
		Strings: []byte{0},
	}
	h.SetProgs(stripped)
	h.Server.NumEdicts = 2
	err := h.Frame(0.05)
	if err == nil {
		t.Fatal("Frame: got nil; want propagated RunPhysics error")
	}
}

// Frame propagates a SendClientFrames write error. Force this by
// shrinking an active client's Message buffer to 0 capacity, then
// pushing a byte into ReliableDatagram so PreparePerClientMessage's
// Write surfaces a sizebuf overflow.
func TestFrame_PropagatesSendClientFramesError(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	// Wire a client + flip it Active + Spawned so SendClientFrames
	// will actually try to write to its Message buffer.
	_, _, err := h.ConnectLoopback()
	if err != nil {
		t.Fatalf("ConnectLoopback: %v", err)
	}
	c := h.Static.Clients[0]
	c.Spawned = true
	// Replace the client's Message buffer with a zero-cap one so a
	// 1-byte Write overflows. Push a payload into ReliableDatagram
	// (sized to MaxMsgLen so the seed always succeeds) -- the
	// per-client copy then surfaces ErrSizeBufOverflow.
	c.Message = sizebuf.New(nil)
	if err := h.Server.ReliableDatagram.Write([]byte{0x42}); err != nil {
		t.Fatalf("seed reliable_datagram: %v", err)
	}
	if err := h.Frame(0.05); err == nil {
		t.Error("Frame: got nil; want propagated SendClientFrames error")
	}
}

// --- SpawnServer ----------------------------------------------------------

func TestSpawnServer_HappyPath(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 4)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if !h.Server.Active {
		t.Error("Server.Active should be true after SpawnServer")
	}
	if h.Server.WorldModel == nil {
		t.Error("Server.WorldModel should be set")
	}
}

func TestSpawnServer_PropagatesError(t *testing.T) {
	// Empty map name -> server.Reset surfaces ErrEmptyMapName.
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("", protocol.VersionNQ); err == nil {
		t.Error("SpawnServer with empty name should error")
	}
}

// --- ConnectLoopback ------------------------------------------------------

func TestConnectLoopback_HappyPath(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 2)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	clientSide, idx, err := h.ConnectLoopback()
	if err != nil {
		t.Fatalf("ConnectLoopback: %v", err)
	}
	if idx != 0 {
		t.Errorf("first slot index got %d want 0", idx)
	}
	if clientSide == nil {
		t.Error("clientSide NetConn should be non-nil")
	}
	if !h.Static.Clients[0].Active {
		t.Error("Static.Clients[0].Active should be true after ConnectLoopback")
	}
	// The bound server-side conn should be a LoopbackConn.
	if _, ok := h.Static.Clients[0].NetConnection.(*server.LoopbackConn); !ok {
		t.Errorf("NetConnection type got %T want *server.LoopbackConn", h.Static.Clients[0].NetConnection)
	}
	// The bound edict should be Server.Edicts[idx+1].
	if h.Static.Clients[0].Edict != h.Server.Edicts[1] {
		t.Error("client.Edict should reference Server.Edicts[1]")
	}
}

func TestConnectLoopback_NoFreeSlot(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if _, _, err := h.ConnectLoopback(); err != nil {
		t.Fatalf("first ConnectLoopback: %v", err)
	}
	_, idx, err := h.ConnectLoopback()
	if !errors.Is(err, server.ErrNoFreeClientSlot) {
		t.Errorf("got err=%v want ErrNoFreeClientSlot", err)
	}
	if idx != -1 {
		t.Errorf("got idx=%d want -1 on full pool", idx)
	}
}

// ConnectLoopback before SpawnServer: the edict pool is empty (the
// host's Server.Edicts is nil-length until SpawnServer runs), so the
// makeEdict hook returns nil. ConnectClient still succeeds; the
// resulting client just has a nil Edict (which the lifecycle layer
// would normally fix up on the first per-client physics).
func TestConnectLoopback_BeforeSpawnServer(t *testing.T) {
	h, _ := makeHost(t, nil, 1)
	clientSide, idx, err := h.ConnectLoopback()
	if err != nil {
		t.Fatalf("ConnectLoopback: %v", err)
	}
	if idx != 0 || clientSide == nil {
		t.Errorf("idx=%d clientSide=%v want (0, non-nil)", idx, clientSide)
	}
	if h.Static.Clients[0].Edict != nil {
		t.Error("client.Edict should be nil before SpawnServer")
	}
}

// --- thinkCaller bridge ---------------------------------------------------

// The bridge invokes vm.Run with the given funcID. We verify by
// checking the OFS_RETURN slot afterwards -- progsForHost's
// Functions[1] returns 42 from the k42 global into OfsReturn.
func TestThinkCaller_InvokesVMRun(t *testing.T) {
	h, _ := makeHost(t, nil, 1)
	// World edict needs to exist for the worldEdict() path; build
	// one via a manual arena so the bridge can resolve "other".
	if err := h.thinkCaller(nil, 1); err != nil {
		t.Fatalf("thinkCaller: %v", err)
	}
	// The bridge calls vm.Run(1), which executes Functions[1] = OP_RETURN
	// reading from k42Slot. OfsReturn should now hold 42.0.
	got, err := h.VM.GlobalFloat(progs.OfsReturn)
	if err != nil {
		t.Fatalf("GlobalFloat(OfsReturn): %v", err)
	}
	if got != 42 {
		t.Errorf("OfsReturn got %v want 42", got)
	}
}

// The bridge surfaces vm.Run errors verbatim. funcID 0 = the null
// function, which vm.Run rejects with ErrBadFunctionIndex.
func TestThinkCaller_PropagatesVMError(t *testing.T) {
	h, _ := makeHost(t, nil, 1)
	err := h.thinkCaller(nil, 0)
	if !errors.Is(err, progs.ErrBadFunctionIndex) {
		t.Errorf("got %v want ErrBadFunctionIndex", err)
	}
}

// With a nil progsRef the bridge skips the named-global hand-off but
// still dispatches vm.Run. The k42-return path proves the dispatch
// still fires.
func TestThinkCaller_NilProgsRefSkipsGlobals(t *testing.T) {
	h, _ := makeHost(t, nil, 1)
	h.SetProgs(nil)
	if err := h.thinkCaller(nil, 1); err != nil {
		t.Fatalf("thinkCaller: %v", err)
	}
	got, _ := h.VM.GlobalFloat(progs.OfsReturn)
	if got != 42 {
		t.Errorf("OfsReturn got %v want 42 (dispatch should still fire)", got)
	}
}

// Bridge with a Progs that DOES NOT declare self/other/time globals
// skips each write but still dispatches.
func TestThinkCaller_MissingNamedGlobals(t *testing.T) {
	h, _ := makeHost(t, nil, 1)
	// Build a Progs that has only Functions + Statements + the k42
	// constant but no self/other/time globals.
	p := progsForHost()
	p.GlobalDefs = nil // strip every named global
	h.SetProgs(p)
	if err := h.thinkCaller(nil, 1); err != nil {
		t.Fatalf("thinkCaller: %v", err)
	}
}

// Bridge with a non-nil ent + a populated edict pool exercises the
// entityPointer non-nil branch. We can't directly observe the
// SetGlobalInt write target (the bridge writes 0 either way under
// the current scope), but we verify the dispatch still fires.
func TestThinkCaller_NonNilEntPath(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	ent := h.Server.Edicts[0]
	if err := h.thinkCaller(ent, 1); err != nil {
		t.Fatalf("thinkCaller: %v", err)
	}
	got, _ := h.VM.GlobalFloat(progs.OfsReturn)
	if got != 42 {
		t.Errorf("OfsReturn got %v want 42", got)
	}
}

// worldEdict returns nil when Server.Edicts is empty (pre-spawn).
func TestWorldEdict_EmptyEdictsSliceIsNil(t *testing.T) {
	h, _ := makeHost(t, nil, 1)
	if got := h.worldEdict(); got != nil {
		t.Errorf("worldEdict on fresh host: %v want nil", got)
	}
}

// worldEdict returns Edicts[0] post-spawn.
func TestWorldEdict_PostSpawnReturnsWorld(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if got := h.worldEdict(); got != h.Server.Edicts[0] {
		t.Error("worldEdict should return Server.Edicts[0]")
	}
}

// entityPointer returns 0 for nil + for a non-nil ent (current scope).
func TestEntityPointer_NilEntIsZero(t *testing.T) {
	h, _ := makeHost(t, nil, 1)
	if got := h.entityPointer(nil); got != 0 {
		t.Errorf("nil ent -> %d want 0", got)
	}
	// Non-nil branch: any *Edict (we synthesize one here) also
	// returns 0 under the current scope.
	if got := h.entityPointer(&progs.Edict{}); got != 0 {
		t.Errorf("non-nil ent -> %d want 0", got)
	}
}

// --- per-tic resolver branches -------------------------------------------

// edictAt covers: in-range happy path, negative index, past-end index.
func TestEdictAt_AllBranches(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if got := h.edictAt(0); got != h.Server.Edicts[0] {
		t.Error("edictAt(0) should return the world edict")
	}
	if got := h.edictAt(-1); got != nil {
		t.Error("edictAt(-1) should be nil")
	}
	if got := h.edictAt(1 << 30); got != nil {
		t.Error("edictAt(huge) should be nil")
	}
}

// cmdAt covers: out-of-range below, out-of-range above, nil slot,
// happy path with a seeded Cmd.
func TestCmdAt_AllBranches(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 2)
	h, _ := makeHost(t, bsp, 2)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if got := h.cmdAt(0); got != (server.UserCmd{}) {
		t.Error("cmdAt(0) should be zero")
	}
	if got := h.cmdAt(h.Static.MaxClients + 1); got != (server.UserCmd{}) {
		t.Error("cmdAt(>max) should be zero")
	}
	h.Static.Clients[1] = nil
	if got := h.cmdAt(2); got != (server.UserCmd{}) {
		t.Error("cmdAt(2) on nil slot should be zero")
	}
	want := server.UserCmd{ForwardMove: 100}
	h.Static.Clients[0].Cmd = want
	if got := h.cmdAt(1); got != want {
		t.Errorf("cmdAt(1) got %v want %v", got, want)
	}
}

// hostKeyAt is a one-liner identity.
func TestHostKeyAt(t *testing.T) {
	if hostKeyAt(7) != 7 {
		t.Errorf("hostKeyAt(7) want 7")
	}
}

// --- EdictOrigin ----------------------------------------------------------

// Happy path: write origin into the world edict via EntVars, read it
// back through EdictOrigin.
func TestEdictOrigin_HappyPath(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, p := makeHost(t, bsp, 1)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	v, err := progs.NewEntVars(p, h.Server.Edicts[1])
	if err != nil {
		t.Fatalf("NewEntVars: %v", err)
	}
	want := [3]float32{12, 34, 56}
	if err := v.WriteVec3("origin", want); err != nil {
		t.Fatalf("WriteVec3: %v", err)
	}
	got, err := h.EdictOrigin(1)
	if err != nil {
		t.Fatalf("EdictOrigin: %v", err)
	}
	if got != want {
		t.Errorf("EdictOrigin got %v want %v", got, want)
	}
}

// Out-of-range slot indices fail with ErrNoEdict.
func TestEdictOrigin_OutOfRange(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	if _, err := h.EdictOrigin(-1); !errors.Is(err, ErrNoEdict) {
		t.Errorf("EdictOrigin(-1) got %v want ErrNoEdict", err)
	}
	if _, err := h.EdictOrigin(1 << 30); !errors.Is(err, ErrNoEdict) {
		t.Errorf("EdictOrigin(huge) got %v want ErrNoEdict", err)
	}
}

// nil edict in an in-range slot also surfaces ErrNoEdict.
func TestEdictOrigin_NilEdict(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	h.Server.Edicts[0] = nil
	if _, err := h.EdictOrigin(0); !errors.Is(err, ErrNoEdict) {
		t.Errorf("EdictOrigin(nil) got %v want ErrNoEdict", err)
	}
}

// EdictOrigin pre-SpawnServer: the edict pool is empty -> ErrNoEdict.
func TestEdictOrigin_BeforeSpawn(t *testing.T) {
	h, _ := makeHost(t, nil, 1)
	if _, err := h.EdictOrigin(0); !errors.Is(err, ErrNoEdict) {
		t.Errorf("EdictOrigin pre-spawn got %v want ErrNoEdict", err)
	}
}

// Without a bound Progs the helper can't resolve the field name; the
// guard returns ErrNoProgs.
func TestEdictOrigin_NoProgs(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	h.SetProgs(nil)
	if _, err := h.EdictOrigin(0); !errors.Is(err, ErrNoProgs) {
		t.Errorf("EdictOrigin no-progs got %v want ErrNoProgs", err)
	}
}

// EntVars-layer errors propagate verbatim: a Progs that omits the
// "origin" field def surfaces ErrFieldNotFound.
func TestEdictOrigin_FieldNotFound(t *testing.T) {
	bsp := buildHostBSP(t, `{ "classname" "worldspawn" }`, 1)
	h, _ := makeHost(t, bsp, 1)
	if err := h.SpawnServer("test", protocol.VersionNQ); err != nil {
		t.Fatalf("SpawnServer: %v", err)
	}
	stripped := &progs.Progs{
		Header:  progs.Header{EntityFields: 8},
		Strings: []byte{0},
	}
	h.SetProgs(stripped)
	if _, err := h.EdictOrigin(0); !errors.Is(err, progs.ErrFieldNotFound) {
		t.Errorf("EdictOrigin missing-field got %v want ErrFieldNotFound", err)
	}
}

// SetInterner replaces the bound interner.
func TestSetInterner(t *testing.T) {
	h, _ := makeHost(t, nil, 1)
	called := false
	h.SetInterner(func(string) int32 { called = true; return 42 })
	if got := h.interner("foo"); got != 42 || !called {
		t.Errorf("SetInterner override not honoured: got=%d called=%v", got, called)
	}
}

func TestSetSpawnFn(t *testing.T) {
	h, _ := makeHost(t, nil, 1)
	if h.spawnFn != nil {
		t.Errorf("spawnFn default: got non-nil, want nil")
	}
	called := 0
	h.SetSpawnFn(func(_ *progs.Edict, _ string) { called++ })
	if h.spawnFn == nil {
		t.Errorf("SetSpawnFn did not install the hook")
	}
	h.spawnFn(nil, "info_player_start")
	if called != 1 {
		t.Errorf("installed hook not invoked: called=%d want 1", called)
	}
}

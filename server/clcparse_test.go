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

// encodeMove builds a wire-shape clc_move datagram. Mirrors
// [client.EncodeClcMove] so we don't introduce a client->server
// import cycle for the test.
func encodeMove(t *testing.T, sendTime float32, viewAngles [3]float32, fwd, side, up int, buttons, impulse uint8) []byte {
	t.Helper()
	buf := sizebuf.New(make([]byte, 64))
	if err := msg.WriteByte(buf, protocol.ClcMove); err != nil {
		t.Fatalf("WriteByte ClcMove: %v", err)
	}
	if err := msg.WriteFloat(buf, sendTime); err != nil {
		t.Fatalf("WriteFloat: %v", err)
	}
	for _, a := range viewAngles {
		if err := msg.WriteAngle(buf, a); err != nil {
			t.Fatalf("WriteAngle: %v", err)
		}
	}
	if err := msg.WriteShort(buf, fwd); err != nil {
		t.Fatalf("WriteShort fwd: %v", err)
	}
	if err := msg.WriteShort(buf, side); err != nil {
		t.Fatalf("WriteShort side: %v", err)
	}
	if err := msg.WriteShort(buf, up); err != nil {
		t.Fatalf("WriteShort up: %v", err)
	}
	if err := msg.WriteByte(buf, int(buttons)); err != nil {
		t.Fatalf("WriteByte buttons: %v", err)
	}
	if err := msg.WriteByte(buf, int(impulse)); err != nil {
		t.Fatalf("WriteByte impulse: %v", err)
	}
	out := make([]byte, len(buf.Bytes()))
	copy(out, buf.Bytes())
	return out
}

func TestReadClientMoves_NilClient(t *testing.T) {
	n, err := ReadClientMoves(nil)
	if !errors.Is(err, ErrNilNetConn) {
		t.Fatalf("err = %v want ErrNilNetConn", err)
	}
	if n != 0 {
		t.Fatalf("n = %d want 0", n)
	}
}

func TestReadClientMoves_NilNetConn(t *testing.T) {
	c := NewClient()
	c.NetConnection = nil
	n, err := ReadClientMoves(c)
	if !errors.Is(err, ErrNilNetConn) {
		t.Fatalf("err = %v want ErrNilNetConn", err)
	}
	if n != 0 {
		t.Fatalf("n = %d want 0", n)
	}
}

// notANetConn covers the type-assert miss path: NetConnection is
// non-nil but not a NetConn implementation.
type notANetConn struct{}

func TestReadClientMoves_BadType(t *testing.T) {
	c := NewClient()
	c.NetConnection = notANetConn{}
	n, err := ReadClientMoves(c)
	if !errors.Is(err, ErrNilNetConn) {
		t.Fatalf("err = %v want ErrNilNetConn", err)
	}
	if n != 0 {
		t.Fatalf("n = %d want 0", n)
	}
}

func TestReadClientMoves_EmptyInbox(t *testing.T) {
	cli, srv := NewLoopbackConn()
	_ = cli
	c := NewClient()
	c.NetConnection = srv
	n, err := ReadClientMoves(c)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if n != 0 {
		t.Fatalf("n = %d want 0", n)
	}
}

func TestReadClientMoves_OneMove(t *testing.T) {
	cli, srv := NewLoopbackConn()
	want := [3]float32{12, 34, 56}
	payload := encodeMove(t, 1.5, want, 100, -50, 25, 0x80, 7)
	if _, err := cli.SendUnreliable(payload); err != nil {
		t.Fatalf("SendUnreliable: %v", err)
	}

	c := NewClient()
	c.NetConnection = srv
	n, err := ReadClientMoves(c)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if n != 1 {
		t.Fatalf("n = %d want 1", n)
	}
	// ReadAngle round-trips with a small quantization error (360/256 per
	// step). Compare with that tolerance.
	for i := 0; i < 3; i++ {
		delta := c.Cmd.ViewAngles[i] - want[i]
		if delta < 0 {
			delta = -delta
		}
		if delta > 360.0/256.0 {
			t.Errorf("ViewAngles[%d] = %v want ~%v", i, c.Cmd.ViewAngles[i], want[i])
		}
	}
	if c.Cmd.ForwardMove != 100 {
		t.Errorf("ForwardMove = %v want 100", c.Cmd.ForwardMove)
	}
	if c.Cmd.SideMove != -50 {
		t.Errorf("SideMove = %v want -50", c.Cmd.SideMove)
	}
	if c.Cmd.UpMove != 25 {
		t.Errorf("UpMove = %v want 25", c.Cmd.UpMove)
	}
	if c.Cmd.Buttons != 0x80 {
		t.Errorf("Buttons = %v want 0x80", c.Cmd.Buttons)
	}
	if c.Cmd.Impulse != 7 {
		t.Errorf("Impulse = %v want 7", c.Cmd.Impulse)
	}
}

func TestReadClientMoves_NopThenMove(t *testing.T) {
	cli, srv := NewLoopbackConn()
	// Nop datagram (single byte).
	if _, err := cli.SendUnreliable([]byte{protocol.ClcNop}); err != nil {
		t.Fatalf("SendUnreliable nop: %v", err)
	}
	payload := encodeMove(t, 0, [3]float32{0, 90, 0}, 50, 0, 0, 0, 0)
	if _, err := cli.SendUnreliable(payload); err != nil {
		t.Fatalf("SendUnreliable move: %v", err)
	}
	c := NewClient()
	c.NetConnection = srv
	n, err := ReadClientMoves(c)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if n != 1 {
		t.Fatalf("n = %d want 1 (nop is silently skipped)", n)
	}
}

func TestReadClientMoves_BadOpcode(t *testing.T) {
	cli, srv := NewLoopbackConn()
	if _, err := cli.SendUnreliable([]byte{0xFE}); err != nil { // 0xFE not in protocol.Clc*
		t.Fatalf("SendUnreliable: %v", err)
	}
	c := NewClient()
	c.NetConnection = srv
	n, err := ReadClientMoves(c)
	if !errors.Is(err, ErrBadClcOpcode) {
		t.Fatalf("err = %v want ErrBadClcOpcode", err)
	}
	if n != 0 {
		t.Fatalf("n = %d want 0", n)
	}
}

func TestReadClientMoves_ShortPayload(t *testing.T) {
	cli, srv := NewLoopbackConn()
	// Opcode + a truncated payload (only 2 bytes after the opcode).
	if _, err := cli.SendUnreliable([]byte{protocol.ClcMove, 0, 0}); err != nil {
		t.Fatalf("SendUnreliable: %v", err)
	}
	c := NewClient()
	c.NetConnection = srv
	n, err := ReadClientMoves(c)
	if !errors.Is(err, ErrShortClcMove) {
		t.Fatalf("err = %v want ErrShortClcMove", err)
	}
	if n != 0 {
		t.Fatalf("n = %d want 0", n)
	}
}

func TestReadClientMoves_DisconnectStopsLoop(t *testing.T) {
	cli, srv := NewLoopbackConn()
	payload := encodeMove(t, 0, [3]float32{}, 11, 0, 0, 0, 0)
	if _, err := cli.SendUnreliable(payload); err != nil {
		t.Fatalf("SendUnreliable: %v", err)
	}
	if err := cli.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	c := NewClient()
	c.NetConnection = srv
	n, err := ReadClientMoves(c)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if n != 1 {
		t.Fatalf("n = %d want 1 (one applied move, then disconnect terminates)", n)
	}
}

// errConn is a NetConn whose ReadMessage always returns a sentinel
// error. Used to exercise the transport-error propagation path.
type errConn struct {
	NetConn
	err error
}

func (e errConn) ReadMessage() (MessageKind, []byte, error) { return MessageNone, nil, e.err }

func TestReadClientMoves_TransportError(t *testing.T) {
	want := errors.New("transport boom")
	c := NewClient()
	c.NetConnection = errConn{err: want}
	n, err := ReadClientMoves(c)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v want %v", err, want)
	}
	if n != 0 {
		t.Fatalf("n = %d want 0", n)
	}
}

// encodeStringCmd builds a wire-shape clc_stringcmd datagram (opcode
// + NUL-terminated payload). Mirrors client.EncodeClcStringCmd
// without importing the client package (the server test stays
// dependency-free).
func encodeStringCmd(t *testing.T, payload string) []byte {
	t.Helper()
	buf := sizebuf.New(make([]byte, 128))
	if err := msg.WriteByte(buf, protocol.ClcStringCmd); err != nil {
		t.Fatalf("WriteByte ClcStringCmd: %v", err)
	}
	if err := msg.WriteString(buf, payload); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	out := make([]byte, len(buf.Bytes()))
	copy(out, buf.Bytes())
	return out
}

func TestReadClientMoves_StringCmdSpawn(t *testing.T) {
	cli, srv := NewLoopbackConn()
	if _, err := cli.SendReliable(encodeStringCmd(t, "spawn")); err != nil {
		t.Fatalf("SendReliable: %v", err)
	}
	c := NewClient()
	c.Active = true
	c.NetConnection = srv
	n, err := ReadClientMoves(c)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if n != 1 {
		t.Fatalf("n = %d want 1", n)
	}
	if !c.Spawned {
		t.Error("Spawned: got false want true (spawn stringcmd should flip)")
	}
	// Server queued svc_signonnum(4) onto c.Message in response.
	if c.Message.Len() != 2 {
		t.Fatalf("Message.Len() = %d want 2 (signonnum + stage byte)", c.Message.Len())
	}
	bytes := c.Message.Bytes()
	if bytes[0] != protocol.SvcSignonNum || bytes[1] != SignonStageSpawn {
		t.Errorf("queued bytes = %v want [SvcSignonNum(%d) %d]",
			bytes, protocol.SvcSignonNum, SignonStageSpawn)
	}
}

func TestReadClientMoves_StringCmdBegin(t *testing.T) {
	cli, srv := NewLoopbackConn()
	if _, err := cli.SendReliable(encodeStringCmd(t, "begin")); err != nil {
		t.Fatalf("SendReliable: %v", err)
	}
	c := NewClient()
	c.Active = true
	c.NetConnection = srv
	if _, err := ReadClientMoves(c); err != nil {
		t.Fatalf("err = %v", err)
	}
	if !c.Spawned {
		t.Error("Spawned: begin should flip Spawned (alias of spawn)")
	}
}

func TestReadClientMoves_StringCmdParseError(t *testing.T) {
	// "spawn" parser tries to queue svc_signonnum(4); a pre-filled
	// 1-byte Message buffer overflows on the first WriteByte. The
	// error propagates through ReadClientMoves -> ParseClcStringCmd.
	cli, srv := NewLoopbackConn()
	if _, err := cli.SendReliable(encodeStringCmd(t, "spawn")); err != nil {
		t.Fatalf("SendReliable: %v", err)
	}
	c := NewClient()
	c.Active = true
	c.NetConnection = srv
	c.Message = sizebuf.New(make([]byte, 1))
	if err := c.Message.Write([]byte{0xFF}); err != nil {
		t.Fatalf("pre-fill: %v", err)
	}
	n, err := ReadClientMoves(c)
	if err == nil {
		t.Fatal("got nil err, want overflow propagation")
	}
	if n != 0 {
		t.Fatalf("n = %d want 0 (error fired before applied++)", n)
	}
}

func TestReadClientMoves_StringCmdShort(t *testing.T) {
	cli, srv := NewLoopbackConn()
	// Opcode alone -- no NUL terminator; ReadString reads past EOF.
	if _, err := cli.SendReliable([]byte{protocol.ClcStringCmd}); err != nil {
		t.Fatalf("SendReliable: %v", err)
	}
	c := NewClient()
	c.NetConnection = srv
	n, err := ReadClientMoves(c)
	if !errors.Is(err, ErrShortClcStringCmd) {
		t.Fatalf("err = %v want ErrShortClcStringCmd", err)
	}
	if n != 0 {
		t.Fatalf("n = %d want 0", n)
	}
}

// --- ParseClcStringCmd direct unit tests ----------------------------

func TestParseClcStringCmd_NilClient(t *testing.T) {
	if err := ParseClcStringCmd(nil, "spawn"); err != nil {
		t.Errorf("got %v want nil", err)
	}
}

func TestParseClcStringCmd_EmptyPayload(t *testing.T) {
	c := NewClient()
	c.Active = true
	if err := ParseClcStringCmd(c, ""); err != nil {
		t.Errorf("got %v want nil", err)
	}
	if c.Spawned {
		t.Error("Spawned: empty payload should not flip")
	}
}

func TestParseClcStringCmd_WhitespaceOnlyPayload(t *testing.T) {
	c := NewClient()
	c.Active = true
	if err := ParseClcStringCmd(c, "   "); err != nil {
		t.Errorf("got %v want nil", err)
	}
	if c.Spawned {
		t.Error("Spawned: whitespace-only payload should not flip")
	}
}

func TestParseClcStringCmd_Prespawn(t *testing.T) {
	c := NewClient()
	c.Active = true
	if err := ParseClcStringCmd(c, "prespawn 0 123456"); err != nil {
		t.Errorf("got %v want nil", err)
	}
	if c.Spawned {
		t.Error("Spawned: prespawn should NOT flip (it's a no-op in the Go port)")
	}
	if c.Message != nil && c.Message.Len() != 0 {
		t.Errorf("Message: got %d bytes, want 0 (prespawn shouldn't queue)", c.Message.Len())
	}
}

func TestParseClcStringCmd_SpawnFlipsAndQueues(t *testing.T) {
	c := NewClient()
	c.Active = true
	if err := ParseClcStringCmd(c, "spawn"); err != nil {
		t.Fatalf("ParseClcStringCmd: %v", err)
	}
	if !c.Spawned {
		t.Error("Spawned: spawn should flip")
	}
	if c.Message.Len() != 2 {
		t.Fatalf("Message.Len = %d want 2", c.Message.Len())
	}
	if c.Message.Bytes()[1] != SignonStageSpawn {
		t.Errorf("stage byte = %d want %d", c.Message.Bytes()[1], SignonStageSpawn)
	}
}

func TestParseClcStringCmd_SpawnNoMessage(t *testing.T) {
	// spawn arm: nil Message should not panic; Spawned still flips.
	c := NewClient()
	c.Active = true
	c.Message = nil
	if err := ParseClcStringCmd(c, "spawn"); err != nil {
		t.Errorf("got %v want nil", err)
	}
	if !c.Spawned {
		t.Error("Spawned: spawn arm should still flip even with nil Message")
	}
}

func TestParseClcStringCmd_SpawnOverflow(t *testing.T) {
	// Constrain Message to 1 byte so EncodeSignonNum's first WriteByte
	// for SvcSignonNum overflows. Spawned still flips (set BEFORE the
	// encode call -- matches the upstream's eager assignment).
	c := NewClient()
	c.Active = true
	c.Message = sizebuf.New(make([]byte, 1))
	if err := c.Message.Write([]byte{0xFF}); err != nil {
		t.Fatalf("pre-fill: %v", err)
	}
	if err := ParseClcStringCmd(c, "spawn"); err == nil {
		t.Error("got nil err on overflow want propagation")
	}
}

func TestParseClcStringCmd_NameSets(t *testing.T) {
	c := NewClient()
	c.Active = true
	if err := ParseClcStringCmd(c, "name Ranger"); err != nil {
		t.Fatalf("ParseClcStringCmd: %v", err)
	}
	if c.Name != "Ranger" {
		t.Errorf("Name = %q want %q", c.Name, "Ranger")
	}
}

func TestParseClcStringCmd_NameTrims(t *testing.T) {
	c := NewClient()
	c.Active = true
	if err := ParseClcStringCmd(c, "name   Ranger  "); err != nil {
		t.Fatalf("ParseClcStringCmd: %v", err)
	}
	if c.Name != "Ranger" {
		t.Errorf("Name = %q want %q (whitespace trim)", c.Name, "Ranger")
	}
}

func TestParseClcStringCmd_UnknownCommand(t *testing.T) {
	c := NewClient()
	c.Active = true
	if err := ParseClcStringCmd(c, "kill"); err != nil {
		t.Errorf("got %v want nil (unknowns are silent skip)", err)
	}
	if c.Spawned {
		t.Error("Spawned: unknown cmd should not flip")
	}
}

func TestSplitFirstWord_Cases(t *testing.T) {
	cases := []struct {
		in, w, r string
	}{
		{"spawn", "spawn", ""},
		{"name X", "name", "X"},
		{"a b c", "a", "b c"},
	}
	for _, tc := range cases {
		w, r := splitFirstWord(tc.in)
		if w != tc.w || r != tc.r {
			t.Errorf("splitFirstWord(%q) = (%q, %q) want (%q, %q)",
				tc.in, w, r, tc.w, tc.r)
		}
	}
}

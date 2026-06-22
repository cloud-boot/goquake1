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

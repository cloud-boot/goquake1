// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"bytes"
	"testing"
)

// --- LoopbackConn -------------------------------------------------------------

func TestLoopbackConn_Addresses(t *testing.T) {
	cli, srv := NewLoopbackConn()
	if got := cli.Address(); got != "loopback:client" {
		t.Errorf("client addr: got %q want %q", got, "loopback:client")
	}
	if got := srv.Address(); got != "loopback:server" {
		t.Errorf("server addr: got %q want %q", got, "loopback:server")
	}
}

func TestLoopbackConn_SendReliableRoundTrip(t *testing.T) {
	cli, srv := NewLoopbackConn()
	payload := []byte{0x01, 0x02, 0x03, 0x04}

	n, err := cli.SendReliable(payload)
	if err != nil {
		t.Fatalf("SendReliable: %v", err)
	}
	if n != len(payload) {
		t.Errorf("SendReliable n: got %d want %d", n, len(payload))
	}

	kind, data, err := srv.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if kind != MessageReliable {
		t.Errorf("kind: got %v want MessageReliable", kind)
	}
	if !bytes.Equal(data, payload) {
		t.Errorf("data: got %v want %v", data, payload)
	}

	// Mutating the source slice after send must NOT corrupt the queued copy.
	payload[0] = 0xff
	if data[0] != 0x01 {
		t.Errorf("send did not copy: peer saw mutation, data[0]=%#x", data[0])
	}
}

func TestLoopbackConn_SendUnreliableRoundTrip(t *testing.T) {
	cli, srv := NewLoopbackConn()
	payload := []byte("hello")

	if _, err := srv.SendUnreliable(payload); err != nil {
		t.Fatalf("SendUnreliable: %v", err)
	}

	kind, data, err := cli.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if kind != MessageUnreliable {
		t.Errorf("kind: got %v want MessageUnreliable", kind)
	}
	if !bytes.Equal(data, payload) {
		t.Errorf("data: got %q want %q", data, payload)
	}
}

func TestLoopbackConn_ReadMessageNoneWhenEmpty(t *testing.T) {
	cli, _ := NewLoopbackConn()
	kind, data, err := cli.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if kind != MessageNone {
		t.Errorf("kind: got %v want MessageNone", kind)
	}
	if data != nil {
		t.Errorf("data: got %v want nil", data)
	}
}

func TestLoopbackConn_CloseSendReturnsClosed(t *testing.T) {
	cli, srv := NewLoopbackConn()
	if err := cli.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := cli.SendReliable([]byte{0x01}); err != ErrNetConnClosed {
		t.Errorf("post-close SendReliable: got %v want ErrNetConnClosed", err)
	}
	if _, err := srv.SendUnreliable([]byte{0x01}); err != ErrNetConnClosed {
		t.Errorf("post-close SendUnreliable (peer side): got %v want ErrNetConnClosed", err)
	}
}

func TestLoopbackConn_CloseReadReturnsDisconnect(t *testing.T) {
	cli, srv := NewLoopbackConn()

	// Queue one message BEFORE closing so we exercise the drain-then-
	// disconnect ordering.
	if _, err := cli.SendReliable([]byte{0x42}); err != nil {
		t.Fatalf("pre-close send: %v", err)
	}
	if err := cli.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// First read drains the queued message.
	kind, data, err := srv.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage (drain): %v", err)
	}
	if kind != MessageReliable {
		t.Errorf("drain kind: got %v want MessageReliable", kind)
	}
	if !bytes.Equal(data, []byte{0x42}) {
		t.Errorf("drain data: got %v want [0x42]", data)
	}

	// Second read reports the disconnect.
	kind, data, err = srv.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage (disconnect): %v", err)
	}
	if kind != MessageDisconnect {
		t.Errorf("disconnect kind: got %v want MessageDisconnect", kind)
	}
	if data != nil {
		t.Errorf("disconnect data: got %v want nil", data)
	}
}

func TestLoopbackConn_SendUnreliableTooLarge(t *testing.T) {
	cli, _ := NewLoopbackConn()
	oversized := make([]byte, LoopbackMTU+1)
	n, err := cli.SendUnreliable(oversized)
	if err != ErrNetConnPacketTooLarge {
		t.Errorf("err: got %v want ErrNetConnPacketTooLarge", err)
	}
	if n != 0 {
		t.Errorf("n: got %d want 0", n)
	}

	// Exactly MTU is allowed.
	atLimit := make([]byte, LoopbackMTU)
	if _, err := cli.SendUnreliable(atLimit); err != nil {
		t.Errorf("at-MTU SendUnreliable: %v", err)
	}
}

// MessageKind values are part of the public contract; verify the
// zero-value is MessageNone so callers that compare against the zero
// value work as documented.
func TestMessageKind_ZeroIsNone(t *testing.T) {
	var k MessageKind
	if k != MessageNone {
		t.Errorf("zero MessageKind: got %v want MessageNone", k)
	}
}

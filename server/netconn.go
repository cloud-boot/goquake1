// Copyright (c) 1996-1997 Id Software, Inc.
// Copyright (c) 2026 the go-quake1/engine authors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"errors"
)

// NetConn is the netcode abstraction the server uses to send / recv
// per-client messages. Implementations: real UDP, loopback (single-
// player), file-based replay. The Go port decouples sv_main from
// any specific transport.
//
// tyrquake's equivalent is struct qsocket_s * -- a void-pointer
// passed through SV_ConnectClient + handed to the netcode layer.
type NetConn interface {
	// SendReliable queues bytes for in-order delivery. Returns the
	// number of bytes consumed; ErrNetConnFull if the send buffer
	// is full + caller should retry next tick.
	SendReliable(bytes []byte) (int, error)

	// SendUnreliable queues bytes for best-effort delivery (single
	// datagram). Returns ErrNetConnPacketTooLarge if bytes exceed
	// the wire MTU.
	SendUnreliable(bytes []byte) (int, error)

	// ReadMessage returns the next pending message from the client,
	// or (MessageNone, nil, nil) if no message is pending (non-
	// blocking).
	ReadMessage() (kind MessageKind, data []byte, err error)

	// Address returns the remote endpoint identifier (host:port for
	// UDP, "loopback:N" for the in-process loopback).
	Address() string

	// Close terminates the connection.
	Close() error
}

// MessageKind is the receiver-side classification of a read message.
type MessageKind int

const (
	MessageNone       MessageKind = iota // ReadMessage returned (nil, nil)
	MessageReliable                      // ordered + delivered
	MessageUnreliable                    // best-effort datagram
	MessageDisconnect                    // remote closed the connection
)

// LoopbackMTU is the wire-MTU enforced on SendUnreliable for the
// in-process loopback. Matches the NQ datagram cap (NET_MAXMESSAGE
// in tyrquake's NET layer); a single-player path never needs to
// fragment so the cap is just a sanity bound.
const LoopbackMTU = 1500

// ErrNetConnClosed / Full / PacketTooLarge are the sentinel errors
// every NetConn implementation surfaces.
var (
	ErrNetConnClosed         = errors.New("server: net connection closed")
	ErrNetConnFull           = errors.New("server: net connection send buffer full")
	ErrNetConnPacketTooLarge = errors.New("server: packet exceeds MTU")
)

// loopbackMessage is one queued packet on a loopback pair: the
// receiver-side classification (reliable / unreliable) plus the
// raw bytes. Copied on send so the producer can recycle its source
// slice immediately.
type loopbackMessage struct {
	kind MessageKind
	data []byte
}

// loopbackState is the shared half-duplex pair backing a single
// LoopbackConn handle. One state object backs both directions; each
// LoopbackConn closes over the queue it RECEIVES from and the queue
// it SENDS into, so the two sides see opposite views of the same
// state.
//
// Both queues are unbounded slices -- a single-player loop never
// produces back-pressure (the consumer is in the same process and
// drains every tick), so the "full" branch only fires when the conn
// is closed.
type loopbackState struct {
	clientToServer []loopbackMessage // produced by client side, drained by server side
	serverToClient []loopbackMessage // produced by server side, drained by client side
	closed         bool
}

// LoopbackConn is the in-process NetConn -- the single-player path.
// Client + server share one process; sends to the client go into a
// queue the client's network layer reads from. Used by the local
// (non-network) client.
//
// Both halves of a loopback pair share one loopbackState; the role
// flag selects which queue is the inbox and which is the outbox so
// each half presents only its own perspective.
type LoopbackConn struct {
	state  *loopbackState
	server bool // true = the server-side handle; false = the client-side handle
}

// NewLoopbackConn returns a fresh loopback pair: client-side conn
// + server-side conn. Messages SendReliable'd on one side appear on
// the other side's ReadMessage queue. The two conns share state but
// each presents only its own perspective.
func NewLoopbackConn() (clientSide, serverSide NetConn) {
	state := &loopbackState{}
	return &LoopbackConn{state: state, server: false},
		&LoopbackConn{state: state, server: true}
}

// outbox returns the queue this side sends INTO (the other side
// drains it via ReadMessage).
func (c *LoopbackConn) outbox() *[]loopbackMessage {
	if c.server {
		return &c.state.serverToClient
	}
	return &c.state.clientToServer
}

// inbox returns the queue this side reads FROM (the other side
// fills it via SendReliable / SendUnreliable).
func (c *LoopbackConn) inbox() *[]loopbackMessage {
	if c.server {
		return &c.state.clientToServer
	}
	return &c.state.serverToClient
}

// SendReliable queues a copy of bytes on the outbox for the peer
// to consume via ReadMessage. The loopback has no fixed send-buffer
// cap, so ErrNetConnFull is never returned in practice -- the only
// failure path is ErrNetConnClosed.
func (c *LoopbackConn) SendReliable(bytes []byte) (int, error) {
	if c.state.closed {
		return 0, ErrNetConnClosed
	}
	cp := make([]byte, len(bytes))
	copy(cp, bytes)
	*c.outbox() = append(*c.outbox(), loopbackMessage{kind: MessageReliable, data: cp})
	return len(bytes), nil
}

// SendUnreliable queues a copy of bytes on the outbox as a single
// datagram. Returns ErrNetConnPacketTooLarge if bytes exceeds
// LoopbackMTU.
func (c *LoopbackConn) SendUnreliable(bytes []byte) (int, error) {
	if c.state.closed {
		return 0, ErrNetConnClosed
	}
	if len(bytes) > LoopbackMTU {
		return 0, ErrNetConnPacketTooLarge
	}
	cp := make([]byte, len(bytes))
	copy(cp, bytes)
	*c.outbox() = append(*c.outbox(), loopbackMessage{kind: MessageUnreliable, data: cp})
	return len(bytes), nil
}

// ReadMessage pops the oldest pending message off the inbox.
// Returns (MessageNone, nil, nil) when no message is pending.
// After Close, returns (MessageDisconnect, nil, nil) once the inbox
// is drained.
func (c *LoopbackConn) ReadMessage() (MessageKind, []byte, error) {
	inbox := c.inbox()
	if len(*inbox) > 0 {
		m := (*inbox)[0]
		*inbox = (*inbox)[1:]
		return m.kind, m.data, nil
	}
	if c.state.closed {
		return MessageDisconnect, nil, nil
	}
	return MessageNone, nil, nil
}

// Address returns the endpoint identifier ("loopback:client" or
// "loopback:server") so logs + tests can disambiguate the two halves
// of a pair.
func (c *LoopbackConn) Address() string {
	if c.server {
		return "loopback:server"
	}
	return "loopback:client"
}

// Close marks the shared state as closed. Both halves observe the
// closure: subsequent Send* on either side returns ErrNetConnClosed;
// ReadMessage drains any queued messages first, then reports
// MessageDisconnect.
func (c *LoopbackConn) Close() error {
	c.state.closed = true
	return nil
}

// Copyright (c) 2015-2023 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package grid

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minio/minio/internal/logger"
)

const lastPingThreshold = 4 * clientPingInterval

type muxServer struct {
	ID               uint64
	LastPing         int64
	SendSeq, RecvSeq uint32
	Resp             chan []byte
	BaseFlags        Flags
	ctx              context.Context
	cancel           context.CancelFunc
	inbound          chan []byte
	parent           *Connection
	sendMu           sync.Mutex
	recvMu           sync.Mutex
	outBlock         chan struct{}
}

func newMuxStateless(ctx context.Context, msg message, c *Connection, handler StatelessHandler) *muxServer {
	var cancel context.CancelFunc
	ctx = setCaller(ctx, c.remote)
	if msg.DeadlineMS > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(msg.DeadlineMS)*time.Millisecond)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	m := muxServer{
		ID:        msg.MuxID,
		RecvSeq:   msg.Seq + 1,
		SendSeq:   msg.Seq,
		ctx:       ctx,
		cancel:    cancel,
		parent:    c,
		LastPing:  time.Now().Unix(),
		BaseFlags: c.baseFlags,
	}
	go func() {
		// TODO: Handle
	}()

	return &m
}

func newMuxStream(ctx context.Context, msg message, c *Connection, handler StreamHandler) *muxServer {
	var cancel context.CancelFunc
	ctx = setCaller(ctx, c.remote)
	if len(handler.Subroute) > 0 {
		ctx = setSubroute(ctx, handler.Subroute)
	}
	if msg.DeadlineMS > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(msg.DeadlineMS)*time.Millisecond+c.addDeadline)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	send := make(chan []byte)
	inboundCap, outboundCap := handler.InCapacity, handler.OutCapacity
	if outboundCap <= 0 {
		outboundCap = 1
	}

	m := muxServer{
		ID:        msg.MuxID,
		RecvSeq:   msg.Seq + 1,
		SendSeq:   msg.Seq,
		ctx:       ctx,
		cancel:    cancel,
		parent:    c,
		inbound:   nil,
		outBlock:  make(chan struct{}, outboundCap),
		LastPing:  time.Now().Unix(),
		BaseFlags: c.baseFlags,
	}
	// Acknowledge Mux created.
	var ack message
	ack.Op = OpAckMux
	ack.Flags = m.BaseFlags
	ack.MuxID = m.ID
	m.send(ack)
	if debugPrint {
		fmt.Println("connected stream mux:", ack.MuxID)
	}

	// Data inbound to the handler
	var handlerIn chan []byte
	if inboundCap > 0 {
		m.inbound = make(chan []byte, inboundCap)
		handlerIn = make(chan []byte, 1)
		go func(inbound <-chan []byte) {
			defer close(handlerIn)
			// Send unblocks when we have delivered the message to the handler.
			for in := range inbound {
				handlerIn <- in
				m.send(message{Op: OpUnblockClMux, MuxID: m.ID, Flags: c.baseFlags})
			}
		}(m.inbound)
	}
	for i := 0; i < outboundCap; i++ {
		m.outBlock <- struct{}{}
	}

	// Handler goroutine.
	var handlerErr *RemoteErr
	go func() {
		start := time.Now()
		defer func() {
			if debugPrint {
				fmt.Println("Mux", m.ID, "Handler took", time.Since(start).Round(time.Millisecond))
			}
			if r := recover(); r != nil {
				logger.LogIf(ctx, fmt.Errorf("grid handler (%v) panic: %v", msg.Handler, r))
				err := RemoteErr(fmt.Sprintf("panic: %v", r))
				handlerErr = &err
			}
			if debugPrint {
				fmt.Println("muxServer: Mux", m.ID, "Returned with", handlerErr)
			}
			close(send)
		}()
		// handlerErr is guarded by 'send' channel.
		handlerErr = handler.Handle(ctx, msg.Payload, handlerIn, send)
	}()
	// Response sender gorutine...
	go func(outBlock <-chan struct{}) {
		defer m.parent.deleteMux(true, m.ID)
		for {
			// Process outgoing message.
			var payload []byte
			var ok bool
			select {
			case payload, ok = <-send:
			case <-ctx.Done():
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-outBlock:
			}
			msg := message{
				MuxID: m.ID,
				Op:    OpMuxServerMsg,
				Flags: c.baseFlags,
			}
			if !ok {
				if debugPrint {
					fmt.Println("muxServer: Mux", m.ID, "send EOF", handlerErr)
				}
				msg.Flags |= FlagEOF
				if handlerErr != nil {
					msg.Flags |= FlagPayloadIsErr
					msg.Payload = []byte(*handlerErr)
				}
				msg.setZeroPayloadFlag()
				m.send(msg)
				return
			}
			msg.Payload = payload
			msg.setZeroPayloadFlag()
			m.send(msg)
		}
	}(m.outBlock)

	// Remote aliveness check.
	if msg.DeadlineMS == 0 || msg.DeadlineMS > uint32(lastPingThreshold/time.Millisecond) {
		go func() {
			t := time.NewTicker(lastPingThreshold / 4)
			defer t.Stop()
			for {
				select {
				case <-m.ctx.Done():
					return
				case <-t.C:
					last := time.Since(time.Unix(atomic.LoadInt64(&m.LastPing), 0))
					if last > lastPingThreshold {
						logger.LogIf(m.ctx, fmt.Errorf("canceling remote connection %s not seen for %v", m.parent, last))
						m.close()
						return
					}
				}
			}
		}()
	}
	return &m
}

// checkSeq will check if sequence number is correct and increment it by 1.
func (m *muxServer) checkSeq(seq uint32) (ok bool) {
	if seq != m.RecvSeq {
		if debugPrint {
			fmt.Printf("expected sequence %d, got %d\n", m.RecvSeq, seq)
		}
		m.disconnect(fmt.Sprintf("receive sequence number mismatch. want %d, got %d", m.RecvSeq, seq))
		return false
	}
	m.RecvSeq++
	return true
}

func (m *muxServer) message(msg message) {
	if debugPrint {
		fmt.Printf("muxServer: received message %d, length %d\n", msg.Seq, len(msg.Payload))
	}
	m.recvMu.Lock()
	defer m.recvMu.Unlock()
	if cap(m.inbound) == 0 {
		m.disconnect("did not expect inbound message")
		return
	}
	if !m.checkSeq(msg.Seq) {
		return
	}
	// Note, on EOF no value can be sent.
	if msg.Flags&FlagEOF != 0 {
		if len(msg.Payload) > 0 {
			logger.LogIf(m.ctx, fmt.Errorf("muxServer: EOF message with payload"))
		}
		close(m.inbound)
		m.inbound = nil
		return
	}

	select {
	case <-m.ctx.Done():
	case m.inbound <- msg.Payload:
		if debugPrint {
			fmt.Printf("muxServer: Sent seq %d to handler\n", msg.Seq)
		}
	default:
		m.disconnect("handler blocked")
	}
}

func (m *muxServer) unblockSend(seq uint32) {
	if !m.checkSeq(seq) {
		return
	}
	m.recvMu.Lock()
	defer m.recvMu.Unlock()
	if m.outBlock == nil {
		// Closed
		return
	}
	select {
	case m.outBlock <- struct{}{}:
	default:
		logger.LogIf(m.ctx, errors.New("output unblocked overflow"))
	}
}

func (m *muxServer) ping(seq uint32) pongMsg {
	if !m.checkSeq(seq) {
		msg := fmt.Sprintf("receive sequence number mismatch. want %d, got %d", m.RecvSeq, seq)
		return pongMsg{Err: &msg}
	}
	select {
	case <-m.ctx.Done():
		err := context.Cause(m.ctx).Error()
		return pongMsg{Err: &err}
	default:
		atomic.StoreInt64(&m.LastPing, time.Now().Unix())
		return pongMsg{}
	}
}

func (m *muxServer) disconnect(msg string) {
	if debugPrint {
		fmt.Println("Mux", m.ID, "disconnecting. Reason:", msg)
	}
	if msg != "" {
		m.send(message{Op: OpMuxServerMsg, MuxID: m.ID, Flags: FlagPayloadIsErr | FlagEOF, Payload: []byte(msg)})
	} else {
		m.send(message{Op: OpDisconnectClientMux, MuxID: m.ID})
	}
	m.parent.deleteMux(true, m.ID)
}

func (m *muxServer) send(msg message) {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	msg.MuxID = m.ID
	msg.Seq = m.SendSeq
	m.SendSeq++
	if debugPrint {
		fmt.Printf("Mux %d, Sending %+v\n", m.ID, msg)
	}
	logger.LogIf(m.ctx, m.parent.queueMsg(msg, nil))
}

func (m *muxServer) close() {
	m.cancel()
	m.recvMu.Lock()
	defer m.recvMu.Unlock()
	if m.inbound != nil {
		close(m.inbound)
		m.inbound = nil
	}
	if m.outBlock != nil {
		close(m.outBlock)
		m.outBlock = nil
	}
}

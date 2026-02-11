package bfttcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/usernamenenad/bft-quic/core"
)

// Codec serializes and deserializes messages for transport over the wire.
type Codec interface {
	Marshal(msg core.Message) ([]byte, error)
	Unmarshal(data []byte) (core.Message, error)
}

// peerConn wraps a connection with a mutex for thread-safe writing.
type peerConn struct {
	conn net.Conn
	mu   sync.Mutex
}

// TCPTransport implements core.Transport using TCP connections in a full-mesh topology.
// Each node listens for incoming connections and maintains outgoing connections to all peers.
// Messages are framed with a 4-byte big-endian length prefix followed by codec-encoded payload.
type TCPTransport struct {
	nodeId core.NodeId
	addr   string
	codec  Codec

	listener net.Listener

	peers    map[core.NodeId]string
	outPeers map[core.NodeId]*peerConn
	outMu    sync.RWMutex

	inConns []net.Conn
	inMu    sync.Mutex

	msgCh   chan core.Message
	readyCh chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	logger *slog.Logger
}

// NewTCPTransport creates a new TCP transport and starts listening on the given address.
// Use ":0" to let the OS assign a random port, then call Addr() to discover it.
// Call Connect() to establish outgoing connections to peers.
func NewTCPTransport(
	nodeId core.NodeId,
	listenAddr string,
	codec Codec,
	logger *slog.Logger,
) (*TCPTransport, error) {
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
	}

	t := &TCPTransport{
		nodeId:   nodeId,
		addr:     listener.Addr().String(),
		codec:    codec,
		listener: listener,
		outPeers: make(map[core.NodeId]*peerConn),
		msgCh:    make(chan core.Message, 256),
		readyCh:  make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
	}

	t.wg.Add(1)
	go t.acceptLoop()

	t.logger.Info("TCP transport listening", "nodeId", nodeId, "addr", t.addr)

	return t, nil
}

// Addr returns the actual listen address (useful when listening on ":0").
func (t *TCPTransport) Addr() string {
	return t.addr
}

// Connect starts establishing outgoing TCP connections to all peers.
// WaitForReady() blocks until all connections are established.
func (t *TCPTransport) Connect(peers map[core.NodeId]string) {
	t.peers = peers
	if len(peers) == 0 {
		close(t.readyCh)
		return
	}
	t.wg.Add(1)
	go t.connectToPeers()
}

func (t *TCPTransport) acceptLoop() {
	defer t.wg.Done()

	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.ctx.Done():
				return
			default:
				t.logger.Error("accept error", "error", err)
				continue
			}
		}

		t.inMu.Lock()
		t.inConns = append(t.inConns, conn)
		t.inMu.Unlock()

		t.wg.Add(1)
		go t.handleIncoming(conn)
	}
}

func (t *TCPTransport) handleIncoming(conn net.Conn) {
	defer t.wg.Done()
	defer conn.Close()

	for {
		msg, err := t.readMessage(conn)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return
			}
			select {
			case <-t.ctx.Done():
				return
			default:
				t.logger.Error("read error", "error", err)
				return
			}
		}

		select {
		case t.msgCh <- msg:
		case <-t.ctx.Done():
			return
		}
	}
}

func (t *TCPTransport) connectToPeers() {
	defer t.wg.Done()

	var connectWg sync.WaitGroup
	for peerId, peerAddr := range t.peers {
		connectWg.Add(1)
		go func(id core.NodeId, addr string) {
			defer connectWg.Done()
			t.connectWithRetry(id, addr)
		}(peerId, peerAddr)
	}

	connectWg.Wait()
	close(t.readyCh)
	t.logger.Info("all peers connected", "nodeId", t.nodeId)
}

func (t *TCPTransport) connectWithRetry(peerId core.NodeId, peerAddr string) {
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		conn, err := net.DialTimeout("tcp", peerAddr, time.Second)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		t.outMu.Lock()
		t.outPeers[peerId] = &peerConn{conn: conn}
		t.outMu.Unlock()

		t.logger.Debug("connected to peer", "nodeId", t.nodeId, "peer", peerId)
		return
	}
}

// writeMessage writes a length-prefixed, codec-encoded message to a peer connection.
func (t *TCPTransport) writeMessage(pc *peerConn, msg core.Message) error {
	data, err := t.codec.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	pc.mu.Lock()
	defer pc.mu.Unlock()

	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := pc.conn.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("write length: %w", err)
	}
	if _, err := pc.conn.Write(data); err != nil {
		return fmt.Errorf("write data: %w", err)
	}

	return nil
}

// readMessage reads a length-prefixed, codec-encoded message from a connection.
func (t *TCPTransport) readMessage(conn net.Conn) (core.Message, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(lenBuf[:])
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}

	return t.codec.Unmarshal(data)
}

// Broadcast sends a message to all connected peers and delivers a copy to self.
func (t *TCPTransport) Broadcast(ctx context.Context, msg core.Message) error {
	// Wait for peer connections to be established
	select {
	case <-t.readyCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	t.outMu.RLock()
	peers := make([]*peerConn, 0, len(t.outPeers))
	peerIds := make([]core.NodeId, 0, len(t.outPeers))
	for id, pc := range t.outPeers {
		peers = append(peers, pc)
		peerIds = append(peerIds, id)
	}
	t.outMu.RUnlock()

	var errs []error
	for i, pc := range peers {
		if err := t.writeMessage(pc, msg); err != nil {
			errs = append(errs, fmt.Errorf("send to %s: %w", peerIds[i], err))
		}
	}

	// Deliver to self
	select {
	case t.msgCh <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}

	if len(errs) > 0 {
		return fmt.Errorf("broadcast errors: %v", errs)
	}

	return nil
}

// Send sends a message to a specific peer.
func (t *TCPTransport) Send(ctx context.Context, nodeId core.NodeId, msg core.Message) error {
	select {
	case <-t.readyCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	t.outMu.RLock()
	pc, ok := t.outPeers[nodeId]
	t.outMu.RUnlock()

	if !ok {
		return fmt.Errorf("no connection to %s", nodeId)
	}

	return t.writeMessage(pc, msg)
}

// Subscribe returns the channel that delivers incoming messages.
func (t *TCPTransport) Subscribe() <-chan core.Message {
	return t.msgCh
}

// WaitForReady blocks until all outgoing peer connections are established.
func (t *TCPTransport) WaitForReady() {
	<-t.readyCh
}

// Close shuts down the transport, closing all connections and the listener.
func (t *TCPTransport) Close() error {
	t.cancel()

	if t.listener != nil {
		t.listener.Close()
	}

	t.inMu.Lock()
	for _, conn := range t.inConns {
		conn.Close()
	}
	t.inMu.Unlock()

	t.outMu.Lock()
	for _, pc := range t.outPeers {
		pc.conn.Close()
	}
	t.outMu.Unlock()

	t.wg.Wait()
	return nil
}
package bftquic

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/usernamenenad/bft-quic/core"
)

// Stream type identifiers for the multi-stream architecture.
// Separating control and data planes at the QUIC stream level
// mitigates Head-of-Line blocking: packet loss on one stream
// does not stall reads on another stream.
const (
	StreamTypeControl byte = 0x00 // Consensus: PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE
	StreamTypeData    byte = 0x01 // Bulk: state sync, block proposals, tx gossip
)

const heartbeatMagic byte = 0xBF

// Codec serializes and deserializes messages for transport over the wire.
type Codec interface {
	Marshal(msg core.Message) ([]byte, error)
	Unmarshal(data []byte) (core.Message, error)
}

// MessageClassifier determines which QUIC stream a message should use.
type MessageClassifier interface {
	StreamType(msg core.Message) byte
}

// DefaultClassifier routes all messages to the control plane stream.
type DefaultClassifier struct{}

func (c *DefaultClassifier) StreamType(msg core.Message) byte {
	return StreamTypeControl
}

// HeartbeatMessage represents a liveness probe received via QUIC datagrams.
type HeartbeatMessage struct {
	From      core.NodeId
	Timestamp time.Time
}

// peerStreams holds the QUIC connection and dedicated streams to a single peer.
type peerStreams struct {
	conn    *quic.Conn
	control *quic.Stream
	data    *quic.Stream
	ctrlMu  sync.Mutex
	dataMu  sync.Mutex
}

// QUICTransport implements core.Transport using QUIC with multi-stream
// and datagram support for Head-of-Line blocking mitigation.
//
// Architecture:
//   - Control Plane (stream per peer): consensus-critical messages
//   - Data Plane (stream per peer): bulk data transfers
//   - Heartbeat Plane (QUIC datagrams, RFC 9221): unreliable liveness probes
type QUICTransport struct {
	nodeId     core.NodeId
	codec      Codec
	classifier MessageClassifier

	quicTr   *quic.Transport
	udpConn  *net.UDPConn
	listener *quic.Listener

	peers    map[core.NodeId]string
	outPeers map[core.NodeId]*peerStreams
	outMu    sync.RWMutex

	inConns []*quic.Conn
	inMu    sync.Mutex

	msgCh   chan core.Message
	readyCh chan struct{}

	heartbeatCh   chan HeartbeatMessage
	lastHeartbeat map[core.NodeId]time.Time
	heartbeatMu   sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	logger *slog.Logger
}

// NewQUICTransport creates a QUIC transport and starts listening for connections.
// Use ":0" for listenAddr to let the OS assign a random port.
// If classifier is nil, DefaultClassifier (all control plane) is used.
func NewQUICTransport(
	nodeId core.NodeId,
	listenAddr string,
	codec Codec,
	classifier MessageClassifier,
	logger *slog.Logger,
) (*QUICTransport, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if classifier == nil {
		classifier = &DefaultClassifier{}
	}

	cert, err := GenerateSelfSignedCert()
	if err != nil {
		return nil, fmt.Errorf("generate TLS cert: %w", err)
	}

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"bft-quic"},
	}

	udpAddr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve udp addr: %w", err)
	}

	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("listen udp: %w", err)
	}

	qtr := &quic.Transport{Conn: udpConn}

	quicConf := &quic.Config{
		EnableDatagrams:    true,
		MaxIncomingStreams: 10,
		MaxIdleTimeout:     30 * time.Second,
		KeepAlivePeriod:    10 * time.Second,
	}

	listener, err := qtr.Listen(serverTLS, quicConf)
	if err != nil {
		udpConn.Close()
		return nil, fmt.Errorf("quic listen: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	t := &QUICTransport{
		nodeId:        nodeId,
		codec:         codec,
		classifier:    classifier,
		quicTr:        qtr,
		udpConn:       udpConn,
		listener:      listener,
		outPeers:      make(map[core.NodeId]*peerStreams),
		msgCh:         make(chan core.Message, 256),
		readyCh:       make(chan struct{}),
		heartbeatCh:   make(chan HeartbeatMessage, 64),
		lastHeartbeat: make(map[core.NodeId]time.Time),
		ctx:           ctx,
		cancel:        cancel,
		logger:        logger,
	}

	t.wg.Add(1)
	go t.acceptLoop()

	t.logger.Info("QUIC transport listening", "nodeId", nodeId, "addr", udpConn.LocalAddr().String())

	return t, nil
}

// Addr returns the local UDP address the transport is listening on.
func (t *QUICTransport) Addr() string {
	return t.udpConn.LocalAddr().String()
}

// Connect starts establishing outgoing QUIC connections to all peers.
// Each connection opens two bidirectional streams: control and data.
func (t *QUICTransport) Connect(peers map[core.NodeId]string) {
	t.peers = peers
	if len(peers) == 0 {
		close(t.readyCh)
		return
	}
	t.wg.Add(1)
	go t.connectToPeers()
}

func (t *QUICTransport) connectToPeers() {
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
	t.logger.Info("all QUIC peers connected", "nodeId", t.nodeId)
}

func (t *QUICTransport) connectWithRetry(peerId core.NodeId, peerAddr string) {
	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"bft-quic"},
	}

	quicConf := &quic.Config{
		EnableDatagrams:    true,
		MaxIncomingStreams: 10,
	}

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		udpAddr, err := net.ResolveUDPAddr("udp", peerAddr)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		conn, err := t.quicTr.Dial(t.ctx, udpAddr, clientTLS, quicConf)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// Open control plane stream
		controlStream, err := conn.OpenStreamSync(t.ctx)
		if err != nil {
			conn.CloseWithError(0, "failed to open control stream")
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if _, err := controlStream.Write([]byte{StreamTypeControl}); err != nil {
			conn.CloseWithError(0, "failed to write control header")
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// Open data plane stream
		dataStream, err := conn.OpenStreamSync(t.ctx)
		if err != nil {
			conn.CloseWithError(0, "failed to open data stream")
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if _, err := dataStream.Write([]byte{StreamTypeData}); err != nil {
			conn.CloseWithError(0, "failed to write data header")
			time.Sleep(50 * time.Millisecond)
			continue
		}

		t.outMu.Lock()
		t.outPeers[peerId] = &peerStreams{
			conn:    conn,
			control: controlStream,
			data:    dataStream,
		}
		t.outMu.Unlock()

		t.logger.Debug("connected to QUIC peer", "nodeId", t.nodeId, "peer", peerId)
		return
	}
}

func (t *QUICTransport) acceptLoop() {
	defer t.wg.Done()

	for {
		conn, err := t.listener.Accept(t.ctx)
		if err != nil {
			select {
			case <-t.ctx.Done():
				return
			default:
				t.logger.Error("QUIC accept error", "error", err)
				return
			}
		}

		t.inMu.Lock()
		t.inConns = append(t.inConns, conn)
		t.inMu.Unlock()

		t.wg.Add(1)
		go t.handleConnection(conn)
	}
}

func (t *QUICTransport) handleConnection(conn *quic.Conn) {
	defer t.wg.Done()

	// Receive heartbeat datagrams from this connection
	t.wg.Add(1)
	go t.handleDatagrams(conn)

	// Accept and handle streams
	for {
		stream, err := conn.AcceptStream(t.ctx)
		if err != nil {
			return
		}

		t.wg.Add(1)
		go t.handleStream(stream)
	}
}

func (t *QUICTransport) handleStream(stream *quic.Stream) {
	defer t.wg.Done()
	defer stream.Close()

	// First byte identifies the stream type (control or data)
	var typeBuf [1]byte
	if _, err := io.ReadFull(stream, typeBuf[:]); err != nil {
		t.logger.Error("read stream type", "error", err)
		return
	}

	streamType := typeBuf[0]
	t.logger.Debug("accepted QUIC stream", "type", streamType)

	for {
		msg, err := readMessage(stream, t.codec)
		if err != nil {
			if err == io.EOF {
				return
			}
			select {
			case <-t.ctx.Done():
				return
			default:
				// Suppress expected errors during transport shutdown
				if isClosingError(err) {
					return
				}
				t.logger.Error("read message error", "error", err, "streamType", streamType)
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

func (t *QUICTransport) handleDatagrams(conn *quic.Conn) {
	defer t.wg.Done()

	ds := conn.ConnectionState().SupportsDatagrams
	if !ds.Remote || !ds.Local {
		return
	}

	for {
		data, err := conn.ReceiveDatagram(t.ctx)
		if err != nil {
			return
		}

		hb, err := decodeHeartbeat(data)
		if err != nil {
			continue
		}

		t.heartbeatMu.Lock()
		t.lastHeartbeat[hb.From] = hb.Timestamp
		t.heartbeatMu.Unlock()

		select {
		case t.heartbeatCh <- hb:
		default: // drop if channel full
		}
	}
}

// Broadcast sends msg to all connected peers and delivers a copy to self.
// The message is routed to the appropriate stream based on the MessageClassifier.
func (t *QUICTransport) Broadcast(ctx context.Context, msg core.Message) error {
	select {
	case <-t.readyCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	streamType := t.classifier.StreamType(msg)

	t.outMu.RLock()
	peers := make([]*peerStreams, 0, len(t.outPeers))
	peerIds := make([]core.NodeId, 0, len(t.outPeers))
	for id, ps := range t.outPeers {
		peers = append(peers, ps)
		peerIds = append(peerIds, id)
	}
	t.outMu.RUnlock()

	var errs []error
	for i, ps := range peers {
		if err := t.writeToStream(ps, streamType, msg); err != nil {
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

// Send sends msg to a specific peer on the appropriate stream.
func (t *QUICTransport) Send(ctx context.Context, nodeId core.NodeId, msg core.Message) error {
	select {
	case <-t.readyCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	t.outMu.RLock()
	ps, ok := t.outPeers[nodeId]
	t.outMu.RUnlock()

	if !ok {
		return fmt.Errorf("no QUIC connection to %s", nodeId)
	}

	return t.writeToStream(ps, t.classifier.StreamType(msg), msg)
}

// Subscribe returns the channel delivering incoming messages from all streams.
func (t *QUICTransport) Subscribe() <-chan core.Message {
	return t.msgCh
}

// WaitForReady blocks until all outgoing peer connections and streams are established.
func (t *QUICTransport) WaitForReady() {
	<-t.readyCh
}

// StartHeartbeat begins sending periodic heartbeat datagrams to all peers.
func (t *QUICTransport) StartHeartbeat(interval time.Duration) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-t.ctx.Done():
				return
			case <-ticker.C:
				t.sendHeartbeat()
			}
		}
	}()
}

// HeartbeatCh returns the channel that delivers heartbeat messages from peers.
func (t *QUICTransport) HeartbeatCh() <-chan HeartbeatMessage {
	return t.heartbeatCh
}

// IsAlive checks if a peer has sent a heartbeat within the given timeout.
func (t *QUICTransport) IsAlive(nodeId core.NodeId, timeout time.Duration) bool {
	t.heartbeatMu.RLock()
	last, ok := t.lastHeartbeat[nodeId]
	t.heartbeatMu.RUnlock()

	if !ok {
		return false
	}
	return time.Since(last) < timeout
}

// Close shuts down the QUIC transport, closing all connections and the listener.
func (t *QUICTransport) Close() error {
	t.cancel()

	if t.listener != nil {
		t.listener.Close()
	}

	t.inMu.Lock()
	for _, conn := range t.inConns {
		conn.CloseWithError(0, "transport closing")
	}
	t.inMu.Unlock()

	t.outMu.Lock()
	for _, ps := range t.outPeers {
		ps.conn.CloseWithError(0, "transport closing")
	}
	t.outMu.Unlock()

	t.wg.Wait()

	if t.quicTr != nil {
		return t.quicTr.Close()
	}
	return nil
}

func (t *QUICTransport) writeToStream(ps *peerStreams, streamType byte, msg core.Message) error {
	var stream *quic.Stream
	var mu *sync.Mutex

	switch streamType {
	case StreamTypeData:
		stream = ps.data
		mu = &ps.dataMu
	default:
		stream = ps.control
		mu = &ps.ctrlMu
	}

	mu.Lock()
	defer mu.Unlock()

	return writeMessage(stream, t.codec, msg)
}

func (t *QUICTransport) sendHeartbeat() {
	t.outMu.RLock()
	defer t.outMu.RUnlock()

	data := encodeHeartbeat(t.nodeId)
	for _, ps := range t.outPeers {
		ds := ps.conn.ConnectionState().SupportsDatagrams
		if ds.Remote && ds.Local {
			_ = ps.conn.SendDatagram(data)
		}
	}
}

// writeMessage writes a length-prefixed, codec-encoded message.
func writeMessage(w io.Writer, codec Codec, msg core.Message) error {
	data, err := codec.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("write length: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write data: %w", err)
	}
	return nil
}

// readMessage reads a length-prefixed, codec-encoded message.
func readMessage(r io.Reader, codec Codec) (core.Message, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(lenBuf[:])
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	return codec.Unmarshal(data)
}

// Heartbeat datagram format: magic(1) + nodeIdLen(1) + nodeId(N) + timestamp(8)
func encodeHeartbeat(nodeId core.NodeId) []byte {
	id := []byte(nodeId)
	buf := make([]byte, 1+1+len(id)+8)
	buf[0] = heartbeatMagic
	buf[1] = byte(len(id))
	copy(buf[2:2+len(id)], id)
	binary.BigEndian.PutUint64(buf[2+len(id):], uint64(time.Now().UnixNano()))
	return buf
}

func decodeHeartbeat(data []byte) (HeartbeatMessage, error) {
	if len(data) < 3 || data[0] != heartbeatMagic {
		return HeartbeatMessage{}, fmt.Errorf("invalid heartbeat")
	}
	idLen := int(data[1])
	if len(data) < 2+idLen+8 {
		return HeartbeatMessage{}, fmt.Errorf("heartbeat too short")
	}
	nodeId := core.NodeId(data[2 : 2+idLen])
	ts := int64(binary.BigEndian.Uint64(data[2+idLen:]))
	return HeartbeatMessage{
		From:      nodeId,
		Timestamp: time.Unix(0, ts),
	}, nil
}

func isClosingError(err error) bool {
	var appErr *quic.ApplicationError
	return errors.As(err, &appErr)
}

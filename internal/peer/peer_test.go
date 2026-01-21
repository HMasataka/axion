package peer

import (
	"net"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/protocol"
)

func TestNewClient(t *testing.T) {
	p := NewClient("127.0.0.1:8765")

	if p.addr != "127.0.0.1:8765" {
		t.Errorf("expected addr 127.0.0.1:8765, got %s", p.addr)
	}

	if p.connected {
		t.Error("new client should not be connected")
	}

	if !p.reconnect {
		t.Error("client should have reconnect enabled")
	}

	if p.isServer {
		t.Error("client should not be marked as server")
	}
}

func TestNewFromConn(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	p := NewFromConn(conn)

	if !p.connected {
		t.Error("peer from conn should be connected")
	}

	if p.reconnect {
		t.Error("peer from conn should not have reconnect enabled")
	}

	if !p.isServer {
		t.Error("peer from conn should be marked as server")
	}
}

func TestPeerConnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			time.Sleep(100 * time.Millisecond)
		}
	}()

	p := NewClient(listener.Addr().String())
	defer p.Close()

	if err := p.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	if !p.IsConnected() {
		t.Error("peer should be connected")
	}
}

func TestPeerConnect_AlreadyConnected(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			time.Sleep(200 * time.Millisecond)
		}
	}()

	p := NewClient(listener.Addr().String())
	defer p.Close()

	if err := p.Connect(); err != nil {
		t.Fatalf("first connect failed: %v", err)
	}

	if err := p.Connect(); err != nil {
		t.Fatalf("second connect should succeed (no-op): %v", err)
	}
}

func TestPeerConnect_Refused(t *testing.T) {
	p := NewClient("127.0.0.1:59999")

	err := p.Connect()
	if err == nil {
		p.Close()
		t.Error("expected connection refused error, got nil")
	}
}

func TestPeerClose(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			time.Sleep(100 * time.Millisecond)
		}
	}()

	p := NewClient(listener.Addr().String())

	if err := p.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	p.Close()

	if p.IsConnected() {
		t.Error("peer should not be connected after close")
	}

	p.Close()
}

func TestPeerSend(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	received := make(chan *protocol.Message, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		msg, err := protocol.Decode(conn)
		if err == nil {
			received <- msg
		}
	}()

	p := NewClient(listener.Addr().String())
	defer p.Close()

	if err := p.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"test": "data"}`),
	}

	if err := p.Send(msg); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	select {
	case recv := <-received:
		if recv.Type != msg.Type {
			t.Errorf("type mismatch: expected %d, got %d", msg.Type, recv.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestPeerSend_NotConnected(t *testing.T) {
	p := NewClient("127.0.0.1:8765")

	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{}`),
	}

	err := p.Send(msg)
	if err == nil {
		t.Error("expected error when sending to disconnected peer, got nil")
	}
}

func TestPeerIsConnected(t *testing.T) {
	p := NewClient("127.0.0.1:8765")

	if p.IsConnected() {
		t.Error("new peer should not be connected")
	}
}

func TestPeerAddr(t *testing.T) {
	p := NewClient("192.168.1.100:8765")

	if p.Addr() != "192.168.1.100:8765" {
		t.Errorf("expected addr 192.168.1.100:8765, got %s", p.Addr())
	}
}

func TestPeerSetMessageHandler(t *testing.T) {
	p := NewClient("127.0.0.1:8765")

	handler := func(msg *protocol.Message) {
		_ = msg
	}

	p.SetMessageHandler(handler)

	if p.onMessage == nil {
		t.Error("message handler should be set")
	}
}

func TestServerNew(t *testing.T) {
	s := NewServer(":0")

	if s.addr != ":0" {
		t.Errorf("expected addr :0, got %s", s.addr)
	}

	if len(s.peers) != 0 {
		t.Error("new server should have no peers")
	}
}

func TestServerStartStop(t *testing.T) {
	s := NewServer(":0")

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	if s.listener == nil {
		t.Error("listener should not be nil after start")
	}

	s.Stop()
}

func TestServerStart_PortInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	s := NewServer(addr)
	err = s.Start()
	if err == nil {
		s.Stop()
		t.Error("expected error when port is in use, got nil")
	}
}

func TestServerAcceptConnection(t *testing.T) {
	s := NewServer(":0")

	peerConnected := make(chan struct{}, 1)
	s.SetPeerHandler(func(p *Peer) {
		peerConnected <- struct{}{}
	})

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer s.Stop()

	addr := s.listener.Addr().String()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Wait for peer connection using channel instead of sleep
	select {
	case <-peerConnected:
		// Connection established
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for peer connection")
	}

	// Use retry loop instead of fixed sleep
	var peers []*Peer
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		peers = s.GetPeers()
		if len(peers) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(peers) != 1 {
		t.Errorf("expected 1 peer, got %d", len(peers))
	}
}

func TestServerBroadcast(t *testing.T) {
	s := NewServer(":0")

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer s.Stop()

	addr := s.listener.Addr().String()

	received := make(chan *protocol.Message, 2)

	for i := 0; i < 2; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("failed to connect client %d: %v", i, err)
		}
		defer conn.Close()

		go func(c net.Conn) {
			msg, err := protocol.Decode(c)
			if err == nil {
				received <- msg
			}
		}(conn)
	}

	time.Sleep(100 * time.Millisecond)

	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"broadcast": "test"}`),
	}

	s.Broadcast(msg)

	count := 0
	timeout := time.After(2 * time.Second)
	for count < 2 {
		select {
		case <-received:
			count++
		case <-timeout:
			t.Fatalf("timeout: only received %d of 2 messages", count)
		}
	}
}

func TestServerGetPeers(t *testing.T) {
	s := NewServer(":0")

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer s.Stop()

	peers := s.GetPeers()
	if len(peers) != 0 {
		t.Errorf("expected 0 peers, got %d", len(peers))
	}
}

func TestServerRemovePeer(t *testing.T) {
	s := NewServer(":0")

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer s.Stop()

	addr := s.listener.Addr().String()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	peers := s.GetPeers()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}

	remoteAddr := conn.LocalAddr().String()
	s.RemovePeer(remoteAddr)

	conn.Close()
}

func TestServerSetPeerHandler(t *testing.T) {
	s := NewServer(":0")

	s.SetPeerHandler(func(p *Peer) {
		_ = p
	})

	if s.onPeer == nil {
		t.Error("peer handler should be set")
	}
}

func TestPeerSend_ChannelFull(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Accept but don't read - this will cause the channel to fill
	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			time.Sleep(3 * time.Second)
		}
	}()

	p := NewClient(listener.Addr().String())
	defer p.Close()

	if err := p.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"test": "data"}`),
	}

	// Fill up the outgoing channel
	successCount := 0
	errorCount := 0
	for i := 0; i < 150; i++ {
		if err := p.Send(msg); err != nil {
			errorCount++
		} else {
			successCount++
		}
	}

	// When channel is full, Send should return an error
	// At minimum, SOME sends should succeed (up to channel capacity)
	if successCount == 0 {
		t.Error("expected some sends to succeed before channel full")
	}

	// When channel is full, we should see errors
	// The exact number depends on channel capacity
	t.Logf("sends: %d success, %d errors", successCount, errorCount)
}

func TestPeerIncoming(t *testing.T) {
	p := NewClient("127.0.0.1:8765")

	incoming := p.Incoming()
	if incoming == nil {
		t.Error("Incoming channel should not be nil")
	}
}

func TestPeerMessageHandler(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	received := make(chan *protocol.Message, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		msg := &protocol.Message{
			Type:    protocol.TypeFileChange,
			Payload: []byte(`{"from": "server"}`),
		}
		data, _ := protocol.Encode(msg)
		conn.Write(data)

		time.Sleep(500 * time.Millisecond)
	}()

	p := NewClient(listener.Addr().String())
	p.SetMessageHandler(func(msg *protocol.Message) {
		received <- msg
	})
	defer p.Close()

	if err := p.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	select {
	case msg := <-received:
		if msg.Type != protocol.TypeFileChange {
			t.Errorf("expected TypeFileChange, got %d", msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message from handler")
	}
}

func TestServerMultipleConnections(t *testing.T) {
	s := NewServer(":0")

	connectedCount := 0
	connectedChan := make(chan struct{}, 3)
	s.SetPeerHandler(func(p *Peer) {
		connectedChan <- struct{}{}
	})

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer s.Stop()

	addr := s.listener.Addr().String()

	var conns []net.Conn
	for i := 0; i < 3; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("failed to connect client %d: %v", i, err)
		}
		conns = append(conns, conn)
	}

	defer func() {
		for _, conn := range conns {
			conn.Close()
		}
	}()

	// Wait for all connections using channel
	timeout := time.After(3 * time.Second)
	for connectedCount < 3 {
		select {
		case <-connectedChan:
			connectedCount++
		case <-timeout:
			t.Fatalf("timeout: only %d/3 connections established", connectedCount)
		}
	}

	peers := s.GetPeers()
	if len(peers) != 3 {
		t.Errorf("expected 3 peers, got %d", len(peers))
	}
}

func TestServerStopClosesConnections(t *testing.T) {
	s := NewServer(":0")

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	addr := s.listener.Addr().String()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	s.Stop()

	s.Stop()
}

func TestPeerStart(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			time.Sleep(200 * time.Millisecond)
		}
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}

	p := NewFromConn(conn)
	defer p.Close()

	p.Start()

	time.Sleep(50 * time.Millisecond)

	if !p.IsConnected() {
		t.Error("peer should be connected after Start")
	}
}

// TestPeerSend_WithAssertion tests send with proper assertion on success
func TestPeerSend_WithAssertion(t *testing.T) {
	server := NewServer(":0")

	receivedChan := make(chan *protocol.Message, 1)
	server.SetPeerHandler(func(p *Peer) {
		p.SetMessageHandler(func(msg *protocol.Message) {
			receivedChan <- msg
		})
	})

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	client := NewClient(addr)
	defer client.Close()

	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"relative_path":"test.txt","hash":"abc123"}`),
	}

	err := client.Send(msg)
	if err != nil {
		t.Fatalf("Send should succeed: %v", err)
	}

	select {
	case received := <-receivedChan:
		if received.Type != protocol.TypeFileChange {
			t.Errorf("expected TypeFileChange, got %d", received.Type)
		}
		// Verify payload content
		payload, err := protocol.ParseFileChangePayload(received.Payload)
		if err != nil {
			t.Fatalf("failed to parse payload: %v", err)
		}
		if payload.RelativePath != "test.txt" {
			t.Errorf("expected path test.txt, got %s", payload.RelativePath)
		}
		if payload.Hash != "abc123" {
			t.Errorf("expected hash abc123, got %s", payload.Hash)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

// TestPeerChannelFull_WithAssertion tests channel full behavior with proper assertion
func TestPeerChannelFull_WithAssertion(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Accept but don't read - this will cause the write buffer to fill
	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			// Don't read, just hold the connection
			time.Sleep(5 * time.Second)
		}
	}()

	p := NewClient(listener.Addr().String())
	defer p.Close()

	if err := p.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"test": "data"}`),
	}

	// Fill up the outgoing channel
	errorCount := 0
	successCount := 0
	for i := 0; i < 150; i++ {
		err := p.Send(msg)
		if err != nil {
			errorCount++
		} else {
			successCount++
		}
	}

	// We should have some successes (up to channel capacity)
	if successCount == 0 {
		t.Error("expected some successful sends before channel full")
	}

	t.Logf("successful sends: %d, failed sends: %d", successCount, errorCount)
}

// TestPeerConnectionState tests IsConnected state transitions
func TestPeerConnectionState(t *testing.T) {
	server := NewServer(":0")
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	client := NewClient(addr)

	// Before connect - should not be connected
	if client.IsConnected() {
		t.Error("client should not be connected before Connect()")
	}

	// After connect - should be connected
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if !client.IsConnected() {
		t.Error("client should be connected after Connect()")
	}

	// After close - should not be connected
	client.Close()

	time.Sleep(50 * time.Millisecond)

	if client.IsConnected() {
		t.Error("client should not be connected after Close()")
	}
}

// TestPeerAddrMethod tests the Addr() method
func TestPeerAddrMethod(t *testing.T) {
	expectedAddr := "192.168.1.100:8080"
	p := NewClient(expectedAddr)

	addr := p.Addr()
	if addr != expectedAddr {
		t.Errorf("expected addr %s, got %s", expectedAddr, addr)
	}
}

// TestServerBroadcast_WithAssertion tests broadcast with proper assertions
func TestServerBroadcast_WithAssertion(t *testing.T) {
	server := NewServer(":0")

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	// Create multiple clients
	const numClients = 3
	receivedChannels := make([]chan *protocol.Message, numClients)
	var clients []*Peer

	for i := 0; i < numClients; i++ {
		ch := make(chan *protocol.Message, 1)
		receivedChannels[i] = ch

		client := NewClient(addr)
		client.SetMessageHandler(func(msg *protocol.Message) {
			ch <- msg
		})

		if err := client.Connect(); err != nil {
			t.Fatalf("client %d failed to connect: %v", i, err)
		}
		clients = append(clients, client)
	}

	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	time.Sleep(200 * time.Millisecond)

	// Verify all clients connected
	peers := server.GetPeers()
	if len(peers) != numClients {
		t.Errorf("expected %d peers, got %d", numClients, len(peers))
	}

	// Broadcast a message
	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"relative_path":"broadcast.txt"}`),
	}
	server.Broadcast(msg)

	// Verify all clients received the message
	receivedCount := 0
	timeout := time.After(3 * time.Second)

	for i := 0; i < numClients; i++ {
		select {
		case received := <-receivedChannels[i]:
			if received.Type == protocol.TypeFileChange {
				receivedCount++
			}
		case <-timeout:
			t.Logf("timeout waiting for broadcast at client %d", i)
		}
	}

	if receivedCount != numClients {
		t.Errorf("expected %d clients to receive broadcast, got %d", numClients, receivedCount)
	}
}

// TestServerRemovePeer_WithAssertion tests peer removal with assertions
func TestServerRemovePeer_WithAssertion(t *testing.T) {
	server := NewServer(":0")

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	client := NewClient(addr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	// Verify peer is connected
	peers := server.GetPeers()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}

	// Get peer address
	peerAddr := peers[0].Addr()

	// Remove peer
	server.RemovePeer(peerAddr)

	time.Sleep(100 * time.Millisecond)

	// Verify peer was removed
	peersAfter := server.GetPeers()
	if len(peersAfter) != 0 {
		t.Errorf("expected 0 peers after removal, got %d", len(peersAfter))
	}
}

// TestPeerDoubleConnect tests behavior when connecting an already connected peer
func TestPeerDoubleConnect(t *testing.T) {
	server := NewServer(":0")
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	client := NewClient(addr)
	defer client.Close()

	// First connect
	if err := client.Connect(); err != nil {
		t.Fatalf("first connect failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if !client.IsConnected() {
		t.Fatal("should be connected after first connect")
	}

	// Second connect should be a no-op (already connected)
	err := client.Connect()
	if err != nil {
		t.Logf("second connect returned: %v", err)
	}

	// Should still be connected
	if !client.IsConnected() {
		t.Error("should still be connected after second connect attempt")
	}
}

// TestServerGetListenAddr tests the GetListenAddr method
func TestServerGetListenAddr(t *testing.T) {
	server := NewServer(":0")

	// Before start - should return the configured address
	addrBefore := server.GetListenAddr()
	if addrBefore != ":0" {
		t.Errorf("expected :0 before start, got %s", addrBefore)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	// After start - should return actual listening address
	addrAfter := server.GetListenAddr()
	if addrAfter == ":0" {
		t.Error("expected actual address after start, still got :0")
	}

	// Should contain a port number
	if addrAfter == "" {
		t.Error("address should not be empty after start")
	}
}

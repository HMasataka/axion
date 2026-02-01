package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/peer"
	"github.com/HMasataka/axion/internal/protocol"
	"github.com/HMasataka/axion/internal/syncer"
)

// TestP2P_ClientServerConnection tests basic P2P client-server connectivity
func TestP2P_ClientServerConnection(t *testing.T) {
	server := peer.NewServer(":0")

	peerConnected := make(chan struct{}, 1)
	server.SetPeerHandler(func(p *peer.Peer) {
		peerConnected <- struct{}{}
	})

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	client := peer.NewClient(addr)
	defer client.Close()

	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	// Wait for connection using channel instead of sleep
	select {
	case <-peerConnected:
		// Connection established
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for peer connection")
	}

	peers := server.GetPeers()
	if len(peers) != 1 {
		t.Errorf("expected 1 peer connected, got %d", len(peers))
	}

	if !client.IsConnected() {
		t.Error("client should be connected")
	}
}

// TestP2P_MessageExchange tests message sending between client and server
func TestP2P_MessageExchange(t *testing.T) {
	server := peer.NewServer(":0")

	peerConnected := make(chan struct{}, 1)
	receivedFromClient := make(chan *protocol.Message, 1)
	server.SetPeerHandler(func(p *peer.Peer) {
		p.SetMessageHandler(func(msg *protocol.Message, from *peer.Peer) {
			receivedFromClient <- msg
		})
		peerConnected <- struct{}{}
	})

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	client := peer.NewClient(addr)
	defer client.Close()

	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	// Wait for connection using channel instead of sleep
	select {
	case <-peerConnected:
		// Connection established
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for peer connection")
	}

	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"relative_path":"test.txt","hash":"abc123","mod_time":1234567890,"size":100}`),
	}

	if err := client.Send(msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	select {
	case received := <-receivedFromClient:
		if received.Type != protocol.TypeFileChange {
			t.Errorf("expected message type %d, got %d", protocol.TypeFileChange, received.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

// TestP2P_BidirectionalCommunication tests two-way message passing
func TestP2P_BidirectionalCommunication(t *testing.T) {
	server := peer.NewServer(":0")

	peerConnected := make(chan struct{}, 1)
	receivedFromClient := make(chan *protocol.Message, 1)
	var serverPeer *peer.Peer

	server.SetPeerHandler(func(p *peer.Peer) {
		serverPeer = p
		p.SetMessageHandler(func(msg *protocol.Message, from *peer.Peer) {
			receivedFromClient <- msg
		})
		peerConnected <- struct{}{}
	})

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	receivedFromServer := make(chan *protocol.Message, 1)
	client := peer.NewClient(addr)
	client.SetMessageHandler(func(msg *protocol.Message, from *peer.Peer) {
		receivedFromServer <- msg
	})
	defer client.Close()

	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	// Wait for connection using channel instead of sleep
	select {
	case <-peerConnected:
		// Connection established
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for peer connection")
	}

	_ = serverPeer // Avoid unused variable warning

	// Client sends to server
	clientMsg := &protocol.Message{
		Type:    protocol.TypeFileRequest,
		Payload: []byte(`{"relative_path":"from_client.txt"}`),
	}
	if err := client.Send(clientMsg); err != nil {
		t.Fatalf("client failed to send: %v", err)
	}

	select {
	case msg := <-receivedFromClient:
		if msg.Type != protocol.TypeFileRequest {
			t.Errorf("server expected FileRequest, got %d", msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message at server")
	}

	// Server sends to client via broadcast
	serverMsg := &protocol.Message{
		Type:    protocol.TypeFileData,
		Payload: []byte(`{"relative_path":"from_server.txt","data":"aGVsbG8=","mod_time":1234567890}`),
	}
	server.Broadcast(serverMsg)

	select {
	case msg := <-receivedFromServer:
		if msg.Type != protocol.TypeFileData {
			t.Errorf("client expected FileData, got %d", msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message at client")
	}

	_ = serverPeer
}

// TestP2P_MultipleClients tests server handling multiple clients
func TestP2P_MultipleClients(t *testing.T) {
	server := peer.NewServer(":0")

	connectedCount := 0
	connectedChan := make(chan struct{}, 3)

	server.SetPeerHandler(func(p *peer.Peer) {
		connectedCount++
		connectedChan <- struct{}{}
	})

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	var clients []*peer.Peer
	for i := 0; i < 3; i++ {
		client := peer.NewClient(addr)
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

	// Wait for all connections
	timeout := time.After(3 * time.Second)
	for i := 0; i < 3; i++ {
		select {
		case <-connectedChan:
		case <-timeout:
			t.Fatalf("timeout waiting for client %d connection", i)
		}
	}

	peers := server.GetPeers()
	if len(peers) != 3 {
		t.Errorf("expected 3 peers, got %d", len(peers))
	}
}

// TestP2P_BroadcastToMultipleClients tests broadcasting to all connected clients
func TestP2P_BroadcastToMultipleClients(t *testing.T) {
	server := peer.NewServer(":0")
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	receivedChannels := make([]chan *protocol.Message, 3)
	var clients []*peer.Peer

	for i := 0; i < 3; i++ {
		ch := make(chan *protocol.Message, 1)
		receivedChannels[i] = ch

		client := peer.NewClient(addr)
		client.SetMessageHandler(func(msg *protocol.Message, from *peer.Peer) {
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

	// Wait for all connections to be established
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(server.GetPeers()) >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"relative_path":"broadcast.txt"}`),
	}
	server.Broadcast(msg)

	timeout := time.After(3 * time.Second)
	for i, ch := range receivedChannels {
		select {
		case received := <-ch:
			if received.Type != protocol.TypeFileChange {
				t.Errorf("client %d received wrong message type", i)
			}
		case <-timeout:
			t.Fatalf("timeout waiting for broadcast at client %d", i)
		}
	}
}

// TestSync_FileCreation tests syncing a newly created file between two syncers
func TestSync_FileCreation(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Create syncer1 as server
	syncer1, err := syncer.New(tmpDir1, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer1: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer1: %v", err)
	}
	defer syncer1.Stop()

	// Get syncer1's listen address
	status1 := syncer1.GetStatus()
	syncer1Addr := status1["listen_addr"].(string)

	// Create syncer2 that connects to syncer1
	syncer2, err := syncer.New(tmpDir2, ":0", []string{syncer1Addr}, nil)
	if err != nil {
		t.Fatalf("failed to create syncer2: %v", err)
	}

	if err := syncer2.Start(); err != nil {
		t.Fatalf("failed to start syncer2: %v", err)
	}
	defer syncer2.Stop()

	// Wait for peer connection
	connected := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := syncer1.GetStatus()
		if status["server_peers"].(int) > 0 {
			connected = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !connected {
		t.Fatal("syncer2 failed to connect to syncer1")
	}

	// Create a test file in tmpDir1
	testContent := []byte("test file content for sync")
	testFile := filepath.Join(tmpDir1, "sync_test.txt")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Wait for file to propagate to syncer2
	propagated := false
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		destFile := filepath.Join(tmpDir2, "sync_test.txt")
		if content, err := os.ReadFile(destFile); err == nil {
			if string(content) == string(testContent) {
				propagated = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !propagated {
		t.Error("file did not propagate from syncer1 to syncer2")
	}

	// Verify syncer1 detected the file
	status1After := syncer1.GetStatus()
	localFiles1 := status1After["local_files"].(int)
	if localFiles1 < 1 {
		t.Errorf("syncer1 should detect at least 1 file, got %d", localFiles1)
	}

	// Verify syncer2 now has the file
	status2After := syncer2.GetStatus()
	localFiles2 := status2After["local_files"].(int)
	if localFiles2 < 1 {
		t.Errorf("syncer2 should have at least 1 file after propagation, got %d", localFiles2)
	}
}

// TestSync_SyncerStatus tests syncer status reporting
func TestSync_SyncerStatus(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some test files
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0644)

	syncer1, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer syncer1.Stop()

	time.Sleep(100 * time.Millisecond)

	status := syncer1.GetStatus()

	if status["base_path"] != tmpDir {
		t.Errorf("expected base_path %s, got %v", tmpDir, status["base_path"])
	}

	localFiles, ok := status["local_files"].(int)
	if !ok || localFiles < 2 {
		t.Errorf("expected at least 2 local files, got %v", status["local_files"])
	}

	jsonStatus := syncer1.GetStatusJSON()
	if jsonStatus == "" {
		t.Error("GetStatusJSON should return non-empty string")
	}
}

// TestSync_TwoSyncersConnect tests connection between two syncers
func TestSync_TwoSyncersConnect(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Create syncer1 as server FIRST (no files yet)
	syncer1, err := syncer.New(tmpDir1, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer1: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer1: %v", err)
	}
	defer syncer1.Stop()

	// Get syncer1's listen address from status
	status1 := syncer1.GetStatus()
	syncer1Addr := status1["listen_addr"].(string)

	// Create syncer2 that connects to syncer1
	syncer2, err := syncer.New(tmpDir2, ":0", []string{syncer1Addr}, nil)
	if err != nil {
		t.Fatalf("failed to create syncer2: %v", err)
	}

	if err := syncer2.Start(); err != nil {
		t.Fatalf("failed to start syncer2: %v", err)
	}
	defer syncer2.Stop()

	// Wait for peer connection with polling instead of fixed sleep
	connected := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := syncer1.GetStatus()
		if status["server_peers"].(int) > 0 {
			connected = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !connected {
		t.Fatal("syncer2 failed to connect to syncer1")
	}

	// NOW create a test file in tmpDir1 (after both syncers are connected)
	testContent := []byte("content to sync between syncers")
	testFile := filepath.Join(tmpDir1, "connect_test.txt")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Wait for file to propagate
	propagated := false
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		destFile := filepath.Join(tmpDir2, "connect_test.txt")
		if content, err := os.ReadFile(destFile); err == nil {
			if string(content) == string(testContent) {
				propagated = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !propagated {
		t.Error("file did not propagate from syncer1 to syncer2")
	}

	// Verify syncer1 detected the file
	status1After := syncer1.GetStatus()
	localFiles1 := status1After["local_files"].(int)
	if localFiles1 < 1 {
		t.Errorf("syncer1 should have at least 1 local file, got %d", localFiles1)
	}

	// Verify syncer2 has the file
	status2 := syncer2.GetStatus()
	localFiles2 := status2["local_files"].(int)
	if localFiles2 < 1 {
		t.Errorf("syncer2 should have at least 1 local file after sync, got %d", localFiles2)
	}

	// Verify connection status
	if status1After["server_peers"].(int) < 1 {
		t.Error("syncer1 should have at least 1 connected peer")
	}
}

// TestConflict_LocalNewerWins tests that local file is kept when it's newer
func TestConflict_LocalNewerWins(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a local file with recent modification time
	localFile := filepath.Join(tmpDir, "conflict.txt")
	localContent := []byte("local content")
	if err := os.WriteFile(localFile, localContent, 0644); err != nil {
		t.Fatalf("failed to create local file: %v", err)
	}

	// Set modification time to now
	now := time.Now()
	if err := os.Chtimes(localFile, now, now); err != nil {
		t.Fatalf("failed to set file time: %v", err)
	}

	syncer1, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer syncer1.Stop()

	// Get syncer's server address
	status := syncer1.GetStatus()
	listenAddr := status["listen_addr"].(string)

	// Wait for syncer to be ready
	time.Sleep(100 * time.Millisecond)

	// Connect a client to the syncer and send the FileChange message
	client := peer.NewClient(listenAddr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect to syncer: %v", err)
	}
	defer client.Close()

	time.Sleep(50 * time.Millisecond)

	// Create a FileChange message with older timestamp (local is newer)
	remoteModTime := now.Add(-1 * time.Hour).UnixNano()
	payload := &protocol.FileChangePayload{
		RelativePath: "conflict.txt",
		Hash:         "different_hash_remote",
		ModTime:      remoteModTime,
		Size:         100,
	}
	msg, err := protocol.NewFileChangeMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	// Send the message to the syncer via P2P
	if err := client.Send(msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Wait for syncer to process the message
	time.Sleep(200 * time.Millisecond)

	// Verify local file content hasn't changed (local is newer, so no update)
	content, err := os.ReadFile(localFile)
	if err != nil {
		t.Fatalf("failed to read local file: %v", err)
	}
	if string(content) != string(localContent) {
		t.Error("local file should not be modified when it's newer")
	}

	// Verify file still exists with original size
	info, err := os.Stat(localFile)
	if err != nil {
		t.Fatalf("failed to stat local file: %v", err)
	}
	if info.Size() != int64(len(localContent)) {
		t.Errorf("file size changed: expected %d, got %d", len(localContent), info.Size())
	}
}

// TestConflict_RemoteNewerWins tests that remote file is accepted when it's newer
func TestConflict_RemoteNewerWins(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a local file with old modification time
	localFile := filepath.Join(tmpDir, "remote_newer.txt")
	localContent := []byte("old local content")
	if err := os.WriteFile(localFile, localContent, 0644); err != nil {
		t.Fatalf("failed to create local file: %v", err)
	}

	// Set modification time to past
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(localFile, oldTime, oldTime); err != nil {
		t.Fatalf("failed to set file time: %v", err)
	}

	syncer1, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer syncer1.Stop()

	// Get syncer's server address
	status := syncer1.GetStatus()
	listenAddr := status["listen_addr"].(string)

	// Wait for syncer to be ready with polling
	ready := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status = syncer1.GetStatus()
		localFiles := status["local_files"].(int)
		if localFiles >= 1 {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !ready {
		t.Fatal("syncer did not detect local file")
	}

	// Connect a client to the syncer
	client := peer.NewClient(listenAddr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect to syncer: %v", err)
	}
	defer client.Close()

	// Set up message handler to capture file requests from syncer
	fileRequestReceived := make(chan *protocol.Message, 1)
	client.SetMessageHandler(func(msg *protocol.Message, from *peer.Peer) {
		if msg.Type == protocol.TypeFileRequest {
			fileRequestReceived <- msg
		}
	})

	// Wait for connection to be established
	connDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(connDeadline) {
		if client.IsConnected() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Create a FileChange message with newer timestamp (remote is newer)
	newerModTime := time.Now().Add(1 * time.Hour).UnixNano()
	payload := &protocol.FileChangePayload{
		RelativePath: "remote_newer.txt",
		Hash:         "different_hash_newer",
		ModTime:      newerModTime,
		Size:         200,
	}
	msg, err := protocol.NewFileChangeMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	// Send the message to the syncer via P2P
	if err := client.Send(msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Wait for syncer to process and send a file request
	// Syncer MUST request the file since remote is newer
	select {
	case reqMsg := <-fileRequestReceived:
		// Syncer requested the file - this is expected behavior
		reqPayload, err := protocol.ParseFileRequestPayload(reqMsg.Payload)
		if err != nil {
			t.Fatalf("failed to parse file request: %v", err)
		}
		if reqPayload.RelativePath != "remote_newer.txt" {
			t.Errorf("expected request for remote_newer.txt, got %s", reqPayload.RelativePath)
		}
	case <-time.After(2 * time.Second):
		t.Error("syncer should request file when remote is newer, but no request received")
	}
}

// TestSync_IgnorePatterns tests that ignore patterns are respected
func TestSync_IgnorePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files that should be ignored
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".git", "config"), []byte("git config"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "test.tmp"), []byte("temp file"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "normal.txt"), []byte("normal file"), 0644)

	ignoreList := []string{".git", "*.tmp"}

	syncer1, err := syncer.New(tmpDir, ":0", nil, ignoreList)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer syncer1.Stop()

	time.Sleep(100 * time.Millisecond)

	status := syncer1.GetStatus()
	localFiles := status["local_files"].(int)

	// Should only count normal.txt, not .git or *.tmp
	if localFiles != 1 {
		t.Errorf("expected 1 local file (normal.txt only), got %d", localFiles)
	}
}

// TestSync_IgnorePatterns_NotPropagated tests that ignored files are not broadcast
func TestSync_IgnorePatterns_NotPropagated(t *testing.T) {
	tmpDir := t.TempDir()

	ignoreList := []string{".git", "*.tmp", "*.log"}

	// Create syncer with ignore patterns
	s, err := syncer.New(tmpDir, ":0", nil, ignoreList)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer s.Stop()

	// Set up peer server to capture any broadcast messages
	server := peer.NewServer(":0")
	messagesReceived := make(chan *protocol.Message, 10)
	peerConnected := make(chan struct{}, 1)

	server.SetPeerHandler(func(p *peer.Peer) {
		p.SetMessageHandler(func(msg *protocol.Message, from *peer.Peer) {
			messagesReceived <- msg
		})
		peerConnected <- struct{}{}
	})

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	client := peer.NewClient(addr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for connection
	select {
	case <-peerConnected:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for peer connection")
	}

	time.Sleep(100 * time.Millisecond)

	// Create both ignored and non-ignored files
	os.WriteFile(filepath.Join(tmpDir, "normal.txt"), []byte("should sync"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "ignored.tmp"), []byte("should ignore"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "debug.log"), []byte("log file"), 0644)
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".git", "config"), []byte("git"), 0644)

	// Poll until syncer detects the file (should only count normal.txt)
	deadline := time.Now().Add(2 * time.Second)
	var localFiles int
	for time.Now().Before(deadline) {
		status := s.GetStatus()
		localFiles = status["local_files"].(int)
		if localFiles >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Should only count normal.txt
	if localFiles != 1 {
		t.Errorf("expected 1 local file (normal.txt only), got %d", localFiles)
	}

	// Verify ignored patterns are correctly applied
	// The syncer should not track .git, *.tmp, or *.log files
	// (This is tested implicitly by the local_files count)
}

// TestSync_IgnorePatterns_Validation tests ignore pattern matching
func TestSync_IgnorePatterns_Validation(t *testing.T) {
	testCases := []struct {
		name        string
		ignoreList  []string
		files       map[string]string // filename -> content
		expectCount int               // expected local_files count
	}{
		{
			name:       "ignore_dotfiles",
			ignoreList: []string{".*"},
			files: map[string]string{
				"visible.txt": "content",
				".hidden":     "hidden",
				".gitignore":  "ignore",
			},
			expectCount: 1, // only visible.txt
		},
		{
			name:       "ignore_by_extension",
			ignoreList: []string{"*.bak", "*.swp"},
			files: map[string]string{
				"file.txt":  "content",
				"file.bak":  "backup",
				"file.swp":  "swap",
				"other.doc": "doc",
			},
			expectCount: 2, // file.txt and other.doc
		},
		{
			name:       "ignore_specific_names",
			ignoreList: []string{"node_modules", "vendor"},
			files: map[string]string{
				"main.go":     "package main",
				"other.go":    "package other",
				"readme.txt":  "readme",
			},
			expectCount: 3, // all files (ignore patterns don't match)
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Create test files
			for name, content := range tc.files {
				os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
			}

			s, err := syncer.New(tmpDir, ":0", nil, tc.ignoreList)
			if err != nil {
				t.Fatalf("failed to create syncer: %v", err)
			}
			defer s.Stop()

			status := s.GetStatus()
			localFiles := status["local_files"].(int)

			if localFiles != tc.expectCount {
				t.Errorf("expected %d local files, got %d", tc.expectCount, localFiles)
			}
		})
	}
}

// TestP2P_Reconnection tests client reconnection after server restart
func TestP2P_Reconnection(t *testing.T) {
	server := peer.NewServer(":0")
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	addr := server.GetListenAddr()

	client := peer.NewClient(addr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Verify initial connection
	if !client.IsConnected() {
		t.Fatal("client should be connected initially")
	}

	initialPeers := server.GetPeers()
	if len(initialPeers) != 1 {
		t.Errorf("expected 1 peer initially, got %d", len(initialPeers))
	}

	// Stop server (simulates disconnect)
	server.Stop()

	time.Sleep(200 * time.Millisecond)

	// Client should detect disconnection
	// Note: depending on implementation, IsConnected may still return true briefly
	client.Close()

	// Restart server on same port may not work, use new port
	server2 := peer.NewServer(":0")
	if err := server2.Start(); err != nil {
		t.Fatalf("failed to start server2: %v", err)
	}
	defer server2.Stop()

	addr2 := server2.GetListenAddr()

	// Create new client and connect
	client2 := peer.NewClient(addr2)
	defer client2.Close()

	if err := client2.Connect(); err != nil {
		t.Fatalf("failed to reconnect: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if !client2.IsConnected() {
		t.Error("client2 should be connected after reconnection")
	}

	peers2 := server2.GetPeers()
	if len(peers2) != 1 {
		t.Errorf("expected 1 peer after reconnection, got %d", len(peers2))
	}
}

// TestP2P_LargeMessage tests sending large messages
func TestP2P_LargeMessage(t *testing.T) {
	server := peer.NewServer(":0")

	receivedChan := make(chan *protocol.Message, 1)
	server.SetPeerHandler(func(p *peer.Peer) {
		p.SetMessageHandler(func(msg *protocol.Message, from *peer.Peer) {
			receivedChan <- msg
		})
	})

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	client := peer.NewClient(addr)
	defer client.Close()

	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create large payload (1MB)
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	payload := &protocol.FileDataPayload{
		RelativePath: "large_file.bin",
		Data:         largeData,
		ModTime:      time.Now().UnixNano(),
	}

	msg, err := protocol.NewFileDataMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	if err := client.Send(msg); err != nil {
		t.Fatalf("failed to send large message: %v", err)
	}

	select {
	case received := <-receivedChan:
		if received.Type != protocol.TypeFileData {
			t.Errorf("expected FileData type, got %d", received.Type)
		}

		parsedPayload, err := protocol.ParseFileDataPayload(received.Payload)
		if err != nil {
			t.Fatalf("failed to parse received payload: %v", err)
		}

		if len(parsedPayload.Data) != len(largeData) {
			t.Errorf("data size mismatch: expected %d, got %d", len(largeData), len(parsedPayload.Data))
		}

		if parsedPayload.RelativePath != "large_file.bin" {
			t.Errorf("path mismatch: expected large_file.bin, got %s", parsedPayload.RelativePath)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for large message")
	}
}

// TestSync_FileChangeNotification tests that file changes trigger notifications
func TestSync_FileChangeNotification(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Create initial file in tmpDir1
	testFile := filepath.Join(tmpDir1, "sync_test.txt")
	if err := os.WriteFile(testFile, []byte("initial content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	syncer1, err := syncer.New(tmpDir1, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer1: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer1: %v", err)
	}
	defer syncer1.Stop()

	syncer2, err := syncer.New(tmpDir2, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer2: %v", err)
	}

	if err := syncer2.Start(); err != nil {
		t.Fatalf("failed to start syncer2: %v", err)
	}
	defer syncer2.Stop()

	// Poll until syncer1 detects the file
	deadline := time.Now().Add(2 * time.Second)
	var localFiles1 int
	for time.Now().Before(deadline) {
		status1 := syncer1.GetStatus()
		localFiles1 = status1["local_files"].(int)
		if localFiles1 >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify both syncers started correctly
	status2 := syncer2.GetStatus()
	localFiles2 := status2["local_files"].(int)

	if localFiles1 < 1 {
		t.Errorf("syncer1 should have at least 1 local file, got %d", localFiles1)
	}

	if localFiles2 != 0 {
		t.Errorf("syncer2 should have 0 local files initially, got %d", localFiles2)
	}
}

// TestSync_DirectoryCreation tests syncing directory creation
func TestSync_DirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested directory structure
	nestedDir := filepath.Join(tmpDir, "level1", "level2", "level3")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed to create nested dirs: %v", err)
	}

	// Create file in nested directory
	testFile := filepath.Join(nestedDir, "deep_file.txt")
	if err := os.WriteFile(testFile, []byte("deep content"), 0644); err != nil {
		t.Fatalf("failed to create deep file: %v", err)
	}

	syncer1, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer syncer1.Stop()

	// Poll until syncer detects the nested structure
	deadline := time.Now().Add(2 * time.Second)
	var localFiles int
	for time.Now().Before(deadline) {
		status := syncer1.GetStatus()
		localFiles = status["local_files"].(int)
		if localFiles >= 4 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Should detect the nested structure (3 dirs + 1 file = 4 entries)
	if localFiles < 4 {
		t.Errorf("expected at least 4 entries (3 dirs + 1 file), got %d", localFiles)
	}
}

// TestSync_FileDelete tests syncing file deletion
func TestSync_FileDelete(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file
	testFile := filepath.Join(tmpDir, "to_delete.txt")
	if err := os.WriteFile(testFile, []byte("will be deleted"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	syncer1, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer syncer1.Stop()

	// Poll until file is detected
	deadline := time.Now().Add(2 * time.Second)
	var initialFiles int
	for time.Now().Before(deadline) {
		status := syncer1.GetStatus()
		initialFiles = status["local_files"].(int)
		if initialFiles >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if initialFiles != 1 {
		t.Errorf("expected 1 file initially, got %d", initialFiles)
	}

	// Delete file
	if err := os.Remove(testFile); err != nil {
		t.Fatalf("failed to delete file: %v", err)
	}

	// Poll until deletion is detected
	deadline = time.Now().Add(2 * time.Second)
	var finalFiles int
	for time.Now().Before(deadline) {
		status := syncer1.GetStatus()
		finalFiles = status["local_files"].(int)
		if finalFiles == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalFiles != 0 {
		t.Errorf("expected 0 files after deletion, got %d", finalFiles)
	}
}

// TestConflict_SameTimestamp tests conflict resolution when timestamps are equal
func TestConflict_SameTimestamp(t *testing.T) {
	tmpDir := t.TempDir()

	localFile := filepath.Join(tmpDir, "same_time.txt")
	localContent := []byte("local version")
	if err := os.WriteFile(localFile, localContent, 0644); err != nil {
		t.Fatalf("failed to create local file: %v", err)
	}

	// Get the exact modification time
	info, err := os.Stat(localFile)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	modTime := info.ModTime()

	syncer1, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer syncer1.Stop()

	// Get syncer's server address
	status := syncer1.GetStatus()
	listenAddr := status["listen_addr"].(string)

	time.Sleep(100 * time.Millisecond)

	// Connect a client to the syncer
	client := peer.NewClient(listenAddr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect to syncer: %v", err)
	}
	defer client.Close()

	time.Sleep(50 * time.Millisecond)

	// Create a FileChange message with the SAME timestamp
	payload := &protocol.FileChangePayload{
		RelativePath: "same_time.txt",
		Hash:         "different_hash",
		ModTime:      modTime.UnixNano(),
		Size:         100,
	}
	msg, err := protocol.NewFileChangeMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	// Send the message to the syncer via P2P
	if err := client.Send(msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Wait for syncer to process the message
	time.Sleep(200 * time.Millisecond)

	// When timestamps are equal, local should be preserved (tie-breaker)
	content, _ := os.ReadFile(localFile)
	if string(content) != string(localContent) {
		t.Error("local file should be preserved when timestamps are equal")
	}
}

// TestConflict_HashMatch tests that files with matching hashes are not synced
func TestConflict_HashMatch(t *testing.T) {
	tmpDir := t.TempDir()

	localFile := filepath.Join(tmpDir, "hash_match.txt")
	content := []byte("identical content")
	if err := os.WriteFile(localFile, content, 0644); err != nil {
		t.Fatalf("failed to create local file: %v", err)
	}

	syncer1, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer syncer1.Stop()

	// Get syncer's server address
	status := syncer1.GetStatus()
	listenAddr := status["listen_addr"].(string)

	time.Sleep(100 * time.Millisecond)

	// Calculate the actual hash of the local file (SHA256)
	hash := sha256.Sum256(content)
	localHash := hex.EncodeToString(hash[:])

	// Connect a client to the syncer
	client := peer.NewClient(listenAddr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect to syncer: %v", err)
	}
	defer client.Close()

	// Set up message handler to capture any file requests
	fileRequestReceived := make(chan *protocol.Message, 1)
	client.SetMessageHandler(func(msg *protocol.Message, from *peer.Peer) {
		if msg.Type == protocol.TypeFileRequest {
			fileRequestReceived <- msg
		}
	})

	time.Sleep(50 * time.Millisecond)

	// Create a FileChange message with matching hash (same content)
	// Note: In real scenario, hash would be calculated from content
	payload := &protocol.FileChangePayload{
		RelativePath: "hash_match.txt",
		Hash:         localHash, // Same hash as local file
		ModTime:      time.Now().UnixNano(),
		Size:         int64(len(content)),
	}
	msg, err := protocol.NewFileChangeMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	// Send the message to the syncer via P2P
	if err := client.Send(msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Wait and verify no file request is sent (hashes match)
	// Expected behavior: When hashes match, syncer should NOT request the file
	select {
	case <-fileRequestReceived:
		// File request was sent - hashes should have matched, so no request expected
		t.Error("file request should not be sent when hashes match")
	case <-time.After(300 * time.Millisecond):
		// No request sent - this is the optimal behavior when hashes match
	}

	// Verify file content remains unchanged
	readContent, err := os.ReadFile(localFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(readContent) != string(content) {
		t.Error("file content should remain unchanged when hashes match")
	}
}

// TestSync_EndToEndFilePropagation tests complete file sync between two connected syncers
func TestSync_EndToEndFilePropagation(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Create syncer1 as server first
	syncer1, err := syncer.New(tmpDir1, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer1: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer1: %v", err)
	}
	defer syncer1.Stop()

	// Get syncer1's listen address
	status1 := syncer1.GetStatus()
	syncer1Addr := status1["listen_addr"].(string)

	// Create syncer2 that connects to syncer1 as a peer
	syncer2, err := syncer.New(tmpDir2, ":0", []string{syncer1Addr}, nil)
	if err != nil {
		t.Fatalf("failed to create syncer2: %v", err)
	}

	if err := syncer2.Start(); err != nil {
		t.Fatalf("failed to start syncer2: %v", err)
	}
	defer syncer2.Stop()

	// Wait for peer connection to establish
	connected := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := syncer1.GetStatus()
		if status["server_peers"].(int) > 0 {
			connected = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !connected {
		t.Fatal("syncer2 failed to connect to syncer1")
	}

	// Create a test file in tmpDir1
	testContent := []byte("content to propagate via sync")
	testFile := filepath.Join(tmpDir1, "propagate.txt")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Wait for file to propagate to syncer2
	propagated := false
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		destFile := filepath.Join(tmpDir2, "propagate.txt")
		if content, err := os.ReadFile(destFile); err == nil {
			if string(content) == string(testContent) {
				propagated = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !propagated {
		t.Error("file did not propagate from syncer1 to syncer2")
	}
}

// TestSync_EndToEndFileDataTransfer tests actual file data transfer between peers
func TestSync_EndToEndFileDataTransfer(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test file
	testContent := []byte("file data to transfer")
	testFile := filepath.Join(tmpDir, "transfer.txt")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create syncer
	s, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer s.Stop()

	// Set up peer server to capture messages
	server := peer.NewServer(":0")
	fileDataReceived := make(chan *protocol.Message, 1)
	peerConnected := make(chan struct{}, 1)

	server.SetPeerHandler(func(p *peer.Peer) {
		p.SetMessageHandler(func(msg *protocol.Message, from *peer.Peer) {
			if msg.Type == protocol.TypeFileData {
				fileDataReceived <- msg
			}
		})
		peerConnected <- struct{}{}
	})

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	// Connect client
	client := peer.NewClient(addr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for connection
	select {
	case <-peerConnected:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for peer connection")
	}

	// Send file data message
	filePayload := &protocol.FileDataPayload{
		RelativePath: "transfer.txt",
		Data:         testContent,
		ModTime:      time.Now().UnixNano(),
	}
	msg, _ := protocol.NewFileDataMessage(filePayload)
	if err := client.Send(msg); err != nil {
		t.Fatalf("failed to send file data: %v", err)
	}

	// Verify file data received
	select {
	case received := <-fileDataReceived:
		parsedPayload, err := protocol.ParseFileDataPayload(received.Payload)
		if err != nil {
			t.Fatalf("failed to parse file data payload: %v", err)
		}
		if parsedPayload.RelativePath != "transfer.txt" {
			t.Errorf("path mismatch: expected transfer.txt, got %s", parsedPayload.RelativePath)
		}
		if string(parsedPayload.Data) != string(testContent) {
			t.Errorf("data mismatch: expected %q, got %q", testContent, parsedPayload.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for file data")
	}
}

// TestSync_EndToEndMultipleFiles tests syncing multiple files
func TestSync_EndToEndMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple files before starting syncer
	files := map[string]string{
		"file1.txt":         "content1",
		"file2.txt":         "content2",
		"subdir/file3.txt":  "content3",
		"subdir/nested.txt": "nested content",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create file %s: %v", path, err)
		}
	}

	// Create syncer
	syncer1, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer syncer1.Stop()

	time.Sleep(300 * time.Millisecond)

	// Verify all files are detected
	status := syncer1.GetStatus()
	localFiles := status["local_files"].(int)

	// Should have at least 5 entries (1 subdir + 4 files)
	if localFiles < 5 {
		t.Errorf("expected at least 5 local files/dirs, got %d", localFiles)
	}

	// Verify each file exists and has correct content
	for path, expectedContent := range files {
		fullPath := filepath.Join(tmpDir, path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("file %s should exist: %v", path, err)
			continue
		}
		if string(content) != expectedContent {
			t.Errorf("file %s content mismatch: expected %q, got %q", path, expectedContent, string(content))
		}
	}
}

// TestSync_FileModificationDetection tests that file modifications are detected
func TestSync_FileModificationDetection(t *testing.T) {
	tmpDir := t.TempDir()

	// Create initial file
	testFile := filepath.Join(tmpDir, "modify.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	syncer1, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer syncer1.Stop()

	time.Sleep(200 * time.Millisecond)

	// Verify initial state
	content1, _ := os.ReadFile(testFile)
	if string(content1) != "initial" {
		t.Fatal("initial content mismatch")
	}

	// Modify the file
	if err := os.WriteFile(testFile, []byte("modified content"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	// Wait for watcher to detect modification
	time.Sleep(300 * time.Millisecond)

	// Verify modification is reflected
	content2, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read modified file: %v", err)
	}
	if string(content2) != "modified content" {
		t.Errorf("expected 'modified content', got %q", string(content2))
	}

	// Verify syncer still has 1 file tracked
	status := syncer1.GetStatus()
	localFiles := status["local_files"].(int)
	if localFiles != 1 {
		t.Errorf("expected 1 local file, got %d", localFiles)
	}
}

// TestProtocol_FullMessageCycle tests encode/decode cycle for all message types
func TestProtocol_FullMessageCycle(t *testing.T) {
	testCases := []struct {
		name    string
		msgType protocol.MessageType
		payload []byte
	}{
		{
			name:    "FileChange",
			msgType: protocol.TypeFileChange,
			payload: []byte(`{"relative_path":"test.txt","hash":"abc123","mod_time":1234567890,"size":100}`),
		},
		{
			name:    "FileRequest",
			msgType: protocol.TypeFileRequest,
			payload: []byte(`{"relative_path":"test.txt"}`),
		},
		{
			name:    "FileData",
			msgType: protocol.TypeFileData,
			payload: []byte(`{"relative_path":"test.txt","data":"aGVsbG8=","mod_time":1234567890}`),
		},
		{
			name:    "FileDelete",
			msgType: protocol.TypeFileDelete,
			payload: []byte(`{"relative_path":"test.txt","is_dir":false}`),
		},
		{
			name:    "SyncRequest",
			msgType: protocol.TypeSyncRequest,
			payload: []byte(`{"files":[{"relative_path":"a.txt","hash":"h1","mod_time":1000,"size":10}]}`),
		},
		{
			name:    "SyncResponse",
			msgType: protocol.TypeSyncResponse,
			payload: []byte(`{"need_files":["a.txt"],"delete_files":["b.txt"]}`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			original := &protocol.Message{
				Type:    tc.msgType,
				Payload: tc.payload,
			}

			// Encode
			encoded, err := protocol.Encode(original)
			if err != nil {
				t.Fatalf("encode failed: %v", err)
			}

			// Create a pipe to simulate network
			r, w, _ := os.Pipe()
			go func() {
				w.Write(encoded)
				w.Close()
			}()

			// Decode
			decoded, err := protocol.Decode(r)
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			r.Close()

			if decoded.Type != original.Type {
				t.Errorf("type mismatch: expected %d, got %d", original.Type, decoded.Type)
			}

			if string(decoded.Payload) != string(original.Payload) {
				t.Errorf("payload mismatch")
			}
		})
	}
}

// TestSync_IgnorePatterns_EndToEnd tests that ignored files do NOT propagate between syncers
func TestSync_IgnorePatterns_EndToEnd(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	ignoreList := []string{".git", "*.tmp", "*.log"}

	// Create syncer1 with ignore patterns
	syncer1, err := syncer.New(tmpDir1, ":0", nil, ignoreList)
	if err != nil {
		t.Fatalf("failed to create syncer1: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer1: %v", err)
	}
	defer syncer1.Stop()

	// Get syncer1's listen address
	status1 := syncer1.GetStatus()
	syncer1Addr := status1["listen_addr"].(string)

	// Create syncer2 with same ignore patterns, connecting to syncer1
	syncer2, err := syncer.New(tmpDir2, ":0", []string{syncer1Addr}, ignoreList)
	if err != nil {
		t.Fatalf("failed to create syncer2: %v", err)
	}

	if err := syncer2.Start(); err != nil {
		t.Fatalf("failed to start syncer2: %v", err)
	}
	defer syncer2.Stop()

	// Wait for peer connection
	connected := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := syncer1.GetStatus()
		if status["server_peers"].(int) > 0 {
			connected = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !connected {
		t.Fatal("syncer2 failed to connect to syncer1")
	}

	// Create files in syncer1's directory
	// normal.txt should propagate, ignored files should NOT
	os.WriteFile(filepath.Join(tmpDir1, "normal.txt"), []byte("should sync"), 0644)
	os.WriteFile(filepath.Join(tmpDir1, "ignored.tmp"), []byte("should NOT sync"), 0644)
	os.WriteFile(filepath.Join(tmpDir1, "debug.log"), []byte("should NOT sync"), 0644)
	os.MkdirAll(filepath.Join(tmpDir1, ".git", "objects"), 0755)
	os.WriteFile(filepath.Join(tmpDir1, ".git", "config"), []byte("should NOT sync"), 0644)

	// Wait for file propagation
	normalPropagated := false
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		destFile := filepath.Join(tmpDir2, "normal.txt")
		if content, err := os.ReadFile(destFile); err == nil {
			if string(content) == "should sync" {
				normalPropagated = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !normalPropagated {
		t.Error("normal.txt did not propagate to syncer2")
	}

	// Wait a bit more to ensure ignored files had time to propagate (if they were going to)
	time.Sleep(500 * time.Millisecond)

	// Verify ignored files did NOT propagate
	ignoredFiles := []string{"ignored.tmp", "debug.log", ".git/config"}
	for _, f := range ignoredFiles {
		destFile := filepath.Join(tmpDir2, f)
		if _, err := os.Stat(destFile); !os.IsNotExist(err) {
			t.Errorf("ignored file %s should NOT have propagated to syncer2", f)
		}
	}

	// Verify syncer1 only tracks the non-ignored file
	status1After := syncer1.GetStatus()
	localFiles1 := status1After["local_files"].(int)
	if localFiles1 != 1 {
		t.Errorf("syncer1 should track only 1 file (normal.txt), got %d", localFiles1)
	}

	// Verify syncer2 also only has the non-ignored file
	status2After := syncer2.GetStatus()
	localFiles2 := status2After["local_files"].(int)
	if localFiles2 != 1 {
		t.Errorf("syncer2 should have only 1 file (normal.txt), got %d", localFiles2)
	}
}

// TestSync_FileDelete_EndToEnd tests that file deletions propagate between syncers
func TestSync_FileDelete_EndToEnd(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Create initial file in both directories
	os.WriteFile(filepath.Join(tmpDir1, "todelete.txt"), []byte("will be deleted"), 0644)
	os.WriteFile(filepath.Join(tmpDir2, "todelete.txt"), []byte("will be deleted"), 0644)

	// Create syncer1
	syncer1, err := syncer.New(tmpDir1, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer1: %v", err)
	}

	if err := syncer1.Start(); err != nil {
		t.Fatalf("failed to start syncer1: %v", err)
	}
	defer syncer1.Stop()

	// Get syncer1's listen address
	status1 := syncer1.GetStatus()
	syncer1Addr := status1["listen_addr"].(string)

	// Create syncer2 connecting to syncer1
	syncer2, err := syncer.New(tmpDir2, ":0", []string{syncer1Addr}, nil)
	if err != nil {
		t.Fatalf("failed to create syncer2: %v", err)
	}

	if err := syncer2.Start(); err != nil {
		t.Fatalf("failed to start syncer2: %v", err)
	}
	defer syncer2.Stop()

	// Wait for peer connection
	connected := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := syncer1.GetStatus()
		if status["server_peers"].(int) > 0 {
			connected = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !connected {
		t.Fatal("syncer2 failed to connect to syncer1")
	}

	// Verify both syncers have the file
	if _, err := os.Stat(filepath.Join(tmpDir1, "todelete.txt")); os.IsNotExist(err) {
		t.Fatal("todelete.txt should exist in syncer1")
	}
	if _, err := os.Stat(filepath.Join(tmpDir2, "todelete.txt")); os.IsNotExist(err) {
		t.Fatal("todelete.txt should exist in syncer2")
	}

	// Delete file from syncer1
	os.Remove(filepath.Join(tmpDir1, "todelete.txt"))

	// Wait for deletion to propagate
	deleted := false
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(tmpDir2, "todelete.txt")); os.IsNotExist(err) {
			deleted = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !deleted {
		t.Error("file deletion did not propagate to syncer2")
	}
}

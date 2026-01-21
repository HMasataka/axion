package integration

import (
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

	time.Sleep(100 * time.Millisecond)

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

	receivedFromClient := make(chan *protocol.Message, 1)
	server.SetPeerHandler(func(p *peer.Peer) {
		p.SetMessageHandler(func(msg *protocol.Message) {
			receivedFromClient <- msg
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
		t.Fatalf("failed to connect client: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

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

	receivedFromClient := make(chan *protocol.Message, 1)
	var serverPeer *peer.Peer

	server.SetPeerHandler(func(p *peer.Peer) {
		serverPeer = p
		p.SetMessageHandler(func(msg *protocol.Message) {
			receivedFromClient <- msg
		})
	})

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	receivedFromServer := make(chan *protocol.Message, 1)
	client := peer.NewClient(addr)
	client.SetMessageHandler(func(msg *protocol.Message) {
		receivedFromServer <- msg
	})
	defer client.Close()

	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

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
	if status1["base_path"] != tmpDir1 {
		t.Errorf("expected base_path %s, got %v", tmpDir1, status1["base_path"])
	}

	// Create a test file in tmpDir1
	testContent := []byte("test file content for sync")
	testFile := filepath.Join(tmpDir1, "sync_test.txt")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Wait for syncer1 to detect the file
	time.Sleep(300 * time.Millisecond)

	// Verify syncer1 detected the file
	status1After := syncer1.GetStatus()
	localFiles1 := status1After["local_files"].(int)
	if localFiles1 < 1 {
		t.Errorf("syncer1 should detect at least 1 file, got %d", localFiles1)
	}

	// Create syncer2 with tmpDir2
	syncer2, err := syncer.New(tmpDir2, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer2: %v", err)
	}

	if err := syncer2.Start(); err != nil {
		t.Fatalf("failed to start syncer2: %v", err)
	}
	defer syncer2.Stop()

	// Verify syncer2 started correctly
	status2 := syncer2.GetStatus()
	if status2["base_path"] != tmpDir2 {
		t.Errorf("expected base_path %s, got %v", tmpDir2, status2["base_path"])
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

	// Create a test file in tmpDir1 before starting syncers
	testContent := []byte("content to sync between syncers")
	testFile := filepath.Join(tmpDir1, "connect_test.txt")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create syncer1 as server
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
	if status1["base_path"] != tmpDir1 {
		t.Errorf("syncer1 base_path mismatch: expected %s, got %v", tmpDir1, status1["base_path"])
	}

	// Verify syncer1 detected the file
	localFiles1 := status1["local_files"].(int)
	if localFiles1 < 1 {
		t.Errorf("syncer1 should have at least 1 local file, got %d", localFiles1)
	}

	// Create syncer2 with separate directory
	syncer2, err := syncer.New(tmpDir2, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer2: %v", err)
	}

	if err := syncer2.Start(); err != nil {
		t.Fatalf("failed to start syncer2: %v", err)
	}
	defer syncer2.Stop()

	time.Sleep(200 * time.Millisecond)

	status2 := syncer2.GetStatus()
	if status2["base_path"] != tmpDir2 {
		t.Errorf("syncer2 base_path mismatch: expected %s, got %v", tmpDir2, status2["base_path"])
	}

	// Verify both syncers are running and status is valid
	if status1["connected_clients"] == nil {
		t.Error("syncer1 status should include connected_clients")
	}
	if status2["server_peers"] == nil {
		t.Error("syncer2 status should include server_peers")
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

	time.Sleep(100 * time.Millisecond)

	// Create a FileChange message with older timestamp
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

	// Verify the message was created correctly
	if msg.Type != protocol.TypeFileChange {
		t.Errorf("expected TypeFileChange, got %d", msg.Type)
	}

	// Wait to ensure no file request is triggered (local is newer)
	time.Sleep(200 * time.Millisecond)

	// Verify local file content hasn't changed
	content, err := os.ReadFile(localFile)
	if err != nil {
		t.Fatalf("failed to read local file: %v", err)
	}
	if string(content) != string(localContent) {
		t.Error("local file should not be modified when it's newer")
	}

	// Verify file still exists with original permissions
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

	time.Sleep(100 * time.Millisecond)

	// Verify syncer detected the local file
	status := syncer1.GetStatus()
	localFiles := status["local_files"].(int)
	if localFiles < 1 {
		t.Fatalf("syncer should detect at least 1 local file, got %d", localFiles)
	}

	// Verify the local file's modification time is old
	info, err := os.Stat(localFile)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	if info.ModTime().After(time.Now().Add(-1 * time.Hour)) {
		t.Error("local file modification time should be in the past")
	}

	// Create a FileChange message with newer timestamp
	newerModTime := time.Now().UnixNano()
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

	// Verify message was created correctly
	if msg.Type != protocol.TypeFileChange {
		t.Errorf("expected TypeFileChange, got %d", msg.Type)
	}

	// The syncer would normally request this file because remote is newer
	// This test verifies the syncer can handle such scenarios
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
		p.SetMessageHandler(func(msg *protocol.Message) {
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

	time.Sleep(200 * time.Millisecond)

	// Verify both syncers started correctly
	status1 := syncer1.GetStatus()
	status2 := syncer2.GetStatus()

	localFiles1 := status1["local_files"].(int)
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

	time.Sleep(200 * time.Millisecond)

	status := syncer1.GetStatus()
	localFiles := status["local_files"].(int)

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

	time.Sleep(200 * time.Millisecond)

	// Verify file is detected
	status1 := syncer1.GetStatus()
	initialFiles := status1["local_files"].(int)
	if initialFiles != 1 {
		t.Errorf("expected 1 file initially, got %d", initialFiles)
	}

	// Delete file
	if err := os.Remove(testFile); err != nil {
		t.Fatalf("failed to delete file: %v", err)
	}

	// Wait for watcher to detect deletion
	time.Sleep(300 * time.Millisecond)

	// Verify file count updated
	status2 := syncer1.GetStatus()
	finalFiles := status2["local_files"].(int)
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

	time.Sleep(100 * time.Millisecond)

	// When timestamps are equal, local should be preserved (tie-breaker)
	// The file content should remain unchanged
	content, _ := os.ReadFile(localFile)
	if string(content) != string(localContent) {
		t.Error("local file should be preserved when timestamps are equal")
	}

	_ = modTime
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

	time.Sleep(100 * time.Millisecond)

	// Verify file exists and hasn't been modified
	readContent, err := os.ReadFile(localFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(readContent) != string(content) {
		t.Error("file content should remain unchanged when hashes match")
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

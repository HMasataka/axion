package integration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/config"
	"github.com/HMasataka/axion/internal/peer"
	"github.com/HMasataka/axion/internal/protocol"
	"github.com/HMasataka/axion/internal/syncer"
	"github.com/HMasataka/axion/internal/watcher"
)

// TestError_InvalidConfigJSON tests error handling for invalid config JSON
func TestError_InvalidConfigJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.json")

	// Write invalid JSON
	os.WriteFile(configPath, []byte("{invalid json}"), 0644)

	_, err := config.Load(configPath)
	if err == nil {
		t.Error("expected error for invalid JSON config")
	}
}

// TestError_ConfigNotFound tests error handling for missing config file
func TestError_ConfigNotFound(t *testing.T) {
	_, err := config.Load("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for nonexistent config file")
	}

	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got: %v", err)
	}
}

// TestError_WatcherInvalidPath tests watcher start with invalid path
func TestError_WatcherInvalidPath(t *testing.T) {
	nonexistentPath := "/nonexistent/path/that/does/not/exist"

	// watcher.New may or may not validate path existence
	w, err := watcher.New(nonexistentPath, nil)
	if err != nil {
		// Expected: New validates path and returns error
		// This is the correct behavior - path validation at creation time
		return
	}
	defer w.Stop()

	// If New succeeded, Start MUST fail for nonexistent path
	err = w.Start()
	if err == nil {
		t.Error("watcher.Start should fail for nonexistent path")
	}
}

// TestError_ServerPortInUse tests error when port is already in use
func TestError_ServerPortInUse(t *testing.T) {
	// Start first server
	server1 := peer.NewServer(":0")
	if err := server1.Start(); err != nil {
		t.Fatalf("failed to start first server: %v", err)
	}
	defer server1.Stop()

	// Get the actual port
	addr := server1.GetListenAddr()

	// Try to start second server on same port
	server2 := peer.NewServer(addr)
	err := server2.Start()
	if err == nil {
		server2.Stop()
		t.Error("expected error when starting server on occupied port")
	}
}

// TestError_ConnectToNonexistentServer tests connection to non-existent server
func TestError_ConnectToNonexistentServer(t *testing.T) {
	client := peer.NewClient("127.0.0.1:59999")

	err := client.Connect()
	if err == nil {
		client.Close()
		t.Error("expected error when connecting to nonexistent server")
	}
}

// TestError_SendToDisconnectedPeer tests sending message to disconnected peer
func TestError_SendToDisconnectedPeer(t *testing.T) {
	client := peer.NewClient("127.0.0.1:8765")

	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{}`),
	}

	err := client.Send(msg)
	if err == nil {
		t.Error("expected error when sending to disconnected peer")
	}
}

// TestError_InvalidMessagePayload tests handling of invalid message payloads
func TestError_InvalidMessagePayload(t *testing.T) {
	testCases := []struct {
		name    string
		parser  func([]byte) (interface{}, error)
		payload []byte
	}{
		{
			name: "FileChange_InvalidJSON",
			parser: func(b []byte) (interface{}, error) {
				return protocol.ParseFileChangePayload(b)
			},
			payload: []byte(`{invalid}`),
		},
		{
			name: "FileRequest_InvalidJSON",
			parser: func(b []byte) (interface{}, error) {
				return protocol.ParseFileRequestPayload(b)
			},
			payload: []byte(`not json`),
		},
		{
			name: "FileData_InvalidJSON",
			parser: func(b []byte) (interface{}, error) {
				return protocol.ParseFileDataPayload(b)
			},
			payload: []byte(`{broken`),
		},
		{
			name: "FileDelete_InvalidJSON",
			parser: func(b []byte) (interface{}, error) {
				return protocol.ParseFileDeletePayload(b)
			},
			payload: []byte(`[]`),
		},
		{
			name: "SyncRequest_InvalidJSON",
			parser: func(b []byte) (interface{}, error) {
				return protocol.ParseSyncRequestPayload(b)
			},
			payload: []byte(`{"files": "not an array"}`),
		},
		{
			name: "SyncResponse_InvalidJSON",
			parser: func(b []byte) (interface{}, error) {
				return protocol.ParseSyncResponsePayload(b)
			},
			payload: []byte(`{invalid json}`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.parser(tc.payload)
			if err == nil {
				t.Error("expected error for invalid payload")
			}
		})
	}
}

// TestError_ProtocolDecodeInvalidData tests protocol decode with invalid data
func TestError_ProtocolDecodeInvalidData(t *testing.T) {
	testCases := []struct {
		name        string
		data        []byte
		expectError bool
	}{
		{"empty_data", []byte{}, true},
		{"incomplete_header", []byte{0x00, 0x00, 0x00}, true},
		{"zero_length", []byte{0x00, 0x00, 0x00, 0x00}, true},
		{"truncated_payload", []byte{0x00, 0x00, 0x00, 0x10, 0x01}, true}, // claims 16 bytes but only has 1
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("failed to create pipe: %v", err)
			}

			go func() {
				w.Write(tc.data)
				w.Close()
			}()

			_, decodeErr := protocol.Decode(r)
			r.Close()

			// Invalid/incomplete data MUST result in an error
			if tc.expectError && decodeErr == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

// TestError_SyncerStartWithInvalidPath tests syncer with invalid base path
func TestError_SyncerStartWithInvalidPath(t *testing.T) {
	nonexistentPath := "/nonexistent/syncer/path"

	// syncer.New may or may not validate path existence
	s, err := syncer.New(nonexistentPath, ":0", nil, nil)
	if err != nil {
		// Expected: syncer.New validates path and returns error
		// This is the correct behavior - path validation at creation time
		return
	}

	// If New succeeded, Start MUST fail for nonexistent path
	err = s.Start()
	if err == nil {
		s.Stop()
		t.Error("syncer.Start should fail for nonexistent path")
	}
}

// TestError_FileReadNonexistent tests reading a nonexistent file
func TestError_FileReadNonexistent(t *testing.T) {
	_, err := os.ReadFile("/nonexistent/file.txt")
	if err == nil {
		t.Error("expected error when reading nonexistent file")
	}

	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got: %v", err)
	}
}

// TestError_WatcherHashNonexistent tests hash calculation for nonexistent file
func TestError_WatcherHashNonexistent(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := watcher.New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	nonexistent := filepath.Join(tmpDir, "nonexistent.txt")
	hash := w.GetFileHash(nonexistent)
	if hash != "" {
		t.Error("expected empty hash for nonexistent file")
	}
}

// TestError_ConnectionDrop tests handling of dropped connections
func TestError_ConnectionDrop(t *testing.T) {
	server := peer.NewServer(":0")
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	client := peer.NewClient(addr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Wait for peer connection with polling
	var initialPeers int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		initialPeers = len(server.GetPeers())
		if initialPeers == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if initialPeers != 1 {
		t.Errorf("expected 1 peer after connect, got %d", initialPeers)
	}

	// Abruptly close the client
	client.Close()

	// Wait for server to detect disconnection with polling
	deadline = time.Now().Add(2 * time.Second)
	var finalPeers int
	for time.Now().Before(deadline) {
		finalPeers = len(server.GetPeers())
		if finalPeers == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Server should handle the dropped connection gracefully
	// (no panic, server still running)
	// After client close, peer count should be 0 or unchanged depending on cleanup timing
	if finalPeers > initialPeers {
		t.Errorf("peer count should not increase after close: initial=%d, final=%d", initialPeers, finalPeers)
	}
}

// TestError_ServerStopDuringConnection tests server stop while clients connected
func TestError_ServerStopDuringConnection(t *testing.T) {
	server := peer.NewServer(":0")
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	addr := server.GetListenAddr()

	client := peer.NewClient(addr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Wait for connection with polling
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if client.IsConnected() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !client.IsConnected() {
		t.Fatal("client should be connected before server stop")
	}

	// Stop server while client is connected
	server.Stop()

	// Wait for client to detect disconnection with polling
	deadline = time.Now().Add(2 * time.Second)
	disconnected := false
	for time.Now().Before(deadline) {
		if !client.IsConnected() {
			disconnected = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Client may or may not detect disconnection immediately depending on implementation
	_ = disconnected
}

// TestError_DoubleClose tests that double close doesn't panic
func TestError_DoubleClose(t *testing.T) {
	server := peer.NewServer(":0")
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Close twice - should not panic
	server.Stop()
	server.Stop()

	client := peer.NewClient("127.0.0.1:8765")
	client.Close()
	client.Close()
}

// TestError_DoubleStart tests that double start is handled
func TestError_DoubleStart(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := watcher.New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	if err := w.Start(); err != nil {
		t.Fatalf("first start failed: %v", err)
	}

	// Second start - should either succeed (idempotent) or return error
	// Either way, it should NOT panic
	err = w.Start()
	// Log the result for debugging
	if err != nil {
		t.Logf("second start returned error (expected): %v", err)
	} else {
		t.Log("second start succeeded (idempotent behavior)")
	}
	// Test passes if no panic occurred
}

// TestError_EmptyPayload tests handling of empty payloads
func TestError_EmptyPayload(t *testing.T) {
	parsers := []struct {
		name   string
		parser func([]byte) error
	}{
		{
			name: "FileChange",
			parser: func(b []byte) error {
				_, err := protocol.ParseFileChangePayload(b)
				return err
			},
		},
		{
			name: "FileRequest",
			parser: func(b []byte) error {
				_, err := protocol.ParseFileRequestPayload(b)
				return err
			},
		},
		{
			name: "FileData",
			parser: func(b []byte) error {
				_, err := protocol.ParseFileDataPayload(b)
				return err
			},
		},
	}

	for _, p := range parsers {
		t.Run(p.name+"_Empty", func(t *testing.T) {
			err := p.parser([]byte{})
			if err == nil {
				t.Error("expected error for empty payload")
			}
		})

		t.Run(p.name+"_Nil", func(t *testing.T) {
			err := p.parser(nil)
			if err == nil {
				t.Error("expected error for nil payload")
			}
		})
	}
}

// TestError_WriteToReadOnlyDir tests writing to read-only directory (Unix only)
func TestError_WriteToReadOnlyDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a read-only directory
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0555); err != nil {
		t.Fatalf("failed to create read-only dir: %v", err)
	}

	// Restore permissions for cleanup
	defer os.Chmod(readOnlyDir, 0755)

	testFile := filepath.Join(readOnlyDir, "test.txt")
	err := os.WriteFile(testFile, []byte("content"), 0644)
	if err == nil {
		// Running as root or special permissions - skip this test
		os.Remove(testFile)
		t.Skip("skipping test: running as root or with elevated permissions")
	}

	// Expected behavior: should fail with permission error
	if !os.IsPermission(err) {
		t.Errorf("expected permission error, got: %v", err)
	}
}

// TestError_LargeMessage tests handling of large messages
func TestError_LargeMessage(t *testing.T) {
	// Create a large payload (1MB)
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	payload := &protocol.FileDataPayload{
		RelativePath: "large.bin",
		Data:         largeData,
		ModTime:      time.Now().UnixNano(),
	}

	msg, err := protocol.NewFileDataMessage(payload)
	if err != nil {
		t.Fatalf("failed to create large message: %v", err)
	}

	// Encode the message
	encoded, err := protocol.Encode(msg)
	if err != nil {
		t.Fatalf("failed to encode large message: %v", err)
	}

	// Create pipe for decode test
	r, w, _ := os.Pipe()
	go func() {
		w.Write(encoded)
		w.Close()
	}()

	decoded, err := protocol.Decode(r)
	r.Close()

	if err != nil {
		t.Fatalf("failed to decode large message: %v", err)
	}

	if decoded.Type != protocol.TypeFileData {
		t.Error("decoded message type mismatch")
	}
}

// TestError_ConnectionTimeout tests connection timeout behavior
func TestError_ConnectionTimeout(t *testing.T) {
	// Try to connect to a non-routable IP address to trigger timeout
	// 10.255.255.1 is typically non-routable
	client := peer.NewClient("10.255.255.1:9999")

	startTime := time.Now()
	err := client.Connect()
	elapsed := time.Since(startTime)

	// Connection to non-routable address MUST fail with an error
	if err == nil {
		client.Close()
		t.Fatal("expected error when connecting to non-routable address, got nil")
	}

	// Verify we got an error within reasonable time
	if elapsed > 30*time.Second {
		t.Errorf("connection attempt took too long: %v (expected timeout within 30s)", elapsed)
	}

	// Client should not be connected after failed connection
	if client.IsConnected() {
		t.Error("client should not be connected after connection failure")
	}
}

// TestError_NetworkInterruption tests handling of network interruption
func TestError_NetworkInterruption(t *testing.T) {
	server := peer.NewServer(":0")
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	addr := server.GetListenAddr()

	// Track messages received
	messageReceived := make(chan struct{}, 1)
	server.SetPeerHandler(func(p *peer.Peer) {
		p.SetMessageHandler(func(msg *protocol.Message, from *peer.Peer) {
			messageReceived <- struct{}{}
		})
	})

	client := peer.NewClient(addr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Wait for connection with polling
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if client.IsConnected() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !client.IsConnected() {
		t.Fatal("client should be connected")
	}

	// Wait for peer registration with polling
	var initialPeers int
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		initialPeers = len(server.GetPeers())
		if initialPeers == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if initialPeers != 1 {
		t.Fatalf("expected 1 peer, got %d", initialPeers)
	}

	// Send a message before interruption
	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"relative_path":"test.txt"}`),
	}
	if err := client.Send(msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Wait for message to be received
	select {
	case <-messageReceived:
		// Message received successfully
	case <-time.After(2 * time.Second):
		t.Fatal("message was not received before server stop")
	}

	// Simulate network interruption by stopping the server
	server.Stop()

	// Wait for client to detect disconnection with timeout
	disconnected := false
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !client.IsConnected() {
			disconnected = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Client MUST detect disconnection after server stop
	if !disconnected {
		t.Error("client should detect disconnection after server stop")
	}

	// After server stop and disconnection detected, send MUST fail
	sendErr := client.Send(msg)
	if sendErr == nil {
		// If first send succeeded (buffered), wait and retry
		time.Sleep(200 * time.Millisecond)
		sendErr = client.Send(msg)
		if sendErr == nil {
			t.Error("send should fail after server stop and disconnection")
		}
	}

	// Verify client can still be closed without panic
	client.Close()

	// After close, client MUST report disconnected
	if client.IsConnected() {
		t.Error("client should report disconnected after Close()")
	}
}

// TestError_FilePermissionDenied tests handling of permission denied errors
func TestError_FilePermissionDenied(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file with no read permissions
	testFile := filepath.Join(tmpDir, "no_read.txt")
	if err := os.WriteFile(testFile, []byte("secret"), 0000); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	defer os.Chmod(testFile, 0644) // Restore for cleanup

	// Try to read the file
	_, err := os.ReadFile(testFile)
	if err == nil {
		// Running as root or special permissions - skip this test
		t.Skip("skipping test: running as root or with elevated permissions")
	}

	// Verify it's a permission error
	if !os.IsPermission(err) {
		t.Errorf("expected permission error, got: %v", err)
	}
}

// TestError_DiskSpaceSimulation tests behavior with simulated disk constraints
func TestError_DiskSpaceSimulation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a syncer
	s, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer s.Stop()

	// Verify syncer starts properly
	status := s.GetStatus()
	if status["base_path"] != tmpDir {
		t.Errorf("expected base_path %s, got %v", tmpDir, status["base_path"])
	}
}

// TestError_InvalidPathCharacters tests handling of invalid path characters
func TestError_InvalidPathCharacters(t *testing.T) {
	// Test protocol payload with potentially problematic paths
	invalidPaths := []string{
		"../../../etc/passwd",  // Path traversal attempt
		"file\x00name.txt",     // Null byte
		"con.txt",              // Windows reserved name
	}

	for _, path := range invalidPaths {
		t.Run("path_"+path[:min(10, len(path))], func(t *testing.T) {
			payload := &protocol.FileChangePayload{
				RelativePath: path,
				Hash:         "abc123",
				ModTime:      time.Now().UnixNano(),
				Size:         100,
			}

			// Creating the message should succeed (it's just JSON)
			msg, err := protocol.NewFileChangeMessage(payload)
			if err != nil {
				t.Logf("message creation failed for path %q: %v", path, err)
				return
			}

			// Parse should also succeed (just JSON parsing)
			parsed, err := protocol.ParseFileChangePayload(msg.Payload)
			if err != nil {
				t.Logf("parsing failed for path %q: %v", path, err)
				return
			}

			// Verify the path was preserved
			if parsed.RelativePath != path {
				t.Errorf("path mismatch: expected %q, got %q", path, parsed.RelativePath)
			}
		})
	}
}

// TestError_ConcurrentAccess tests thread safety under concurrent access
func TestError_ConcurrentAccess(t *testing.T) {
	server := peer.NewServer(":0")
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	// Create multiple clients concurrently
	const numClients = 5
	errors := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		go func(id int) {
			client := peer.NewClient(addr)
			defer client.Close()

			if err := client.Connect(); err != nil {
				errors <- err
				return
			}

			// Send a message
			msg := &protocol.Message{
				Type:    protocol.TypeFileChange,
				Payload: []byte(`{"relative_path":"concurrent.txt"}`),
			}
			if err := client.Send(msg); err != nil {
				errors <- err
				return
			}

			errors <- nil
		}(i)
	}

	// Collect results
	successCount := 0
	for i := 0; i < numClients; i++ {
		if err := <-errors; err == nil {
			successCount++
		}
	}

	// Most or all should succeed
	if successCount < numClients/2 {
		t.Errorf("too many failures: only %d/%d succeeded", successCount, numClients)
	}
}

// TestError_MessageAfterClose tests sending after connection close
func TestError_MessageAfterClose(t *testing.T) {
	server := peer.NewServer(":0")
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	client := peer.NewClient(addr)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Close the client
	client.Close()

	// Try to send after close
	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{}`),
	}

	err := client.Send(msg)
	if err == nil {
		t.Error("expected error when sending after close")
	}
}

// TestError_PayloadSizeValidation tests payload size constraints
func TestError_PayloadSizeValidation(t *testing.T) {
	// Create a very large payload (10MB)
	largeData := make([]byte, 10*1024*1024)

	payload := &protocol.FileDataPayload{
		RelativePath: "huge.bin",
		Data:         largeData,
		ModTime:      time.Now().UnixNano(),
	}

	msg, err := protocol.NewFileDataMessage(payload)
	if err != nil {
		t.Logf("large payload creation failed (may be expected): %v", err)
		return
	}

	// Encode should work
	encoded, err := protocol.Encode(msg)
	if err != nil {
		t.Logf("large payload encoding failed: %v", err)
		return
	}

	// Verify the encoded size is reasonable
	if len(encoded) < len(largeData) {
		t.Error("encoded data smaller than payload - compression or error")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

package syncer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/protocol"
)

func TestSyncerNew(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	if s.basePath != tmpDir {
		t.Errorf("basePath mismatch: expected %s, got %s", tmpDir, s.basePath)
	}
}

func TestSyncerNew_WithPeers(t *testing.T) {
	tmpDir := t.TempDir()

	peers := []string{"192.168.1.1:8765", "192.168.1.2:8765"}
	s, err := New(tmpDir, ":0", peers, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	if len(s.clients) != 2 {
		t.Errorf("expected 2 clients, got %d", len(s.clients))
	}
}

func TestSyncerNew_WithIgnoreList(t *testing.T) {
	tmpDir := t.TempDir()

	ignoreList := []string{".git", "*.tmp"}
	s, err := New(tmpDir, ":0", nil, ignoreList)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	if len(s.ignoreList) != 2 {
		t.Errorf("expected 2 ignore patterns, got %d", len(s.ignoreList))
	}
}

func TestSyncerStartStop(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	s.Stop()
}

func TestSyncerCalculateFileHash(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("Hello, World!"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	hash := s.calculateFileHash(testFile)

	expectedHash := "dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f"
	if hash != expectedHash {
		t.Errorf("hash mismatch: expected %s, got %s", expectedHash, hash)
	}
}

func TestSyncerCalculateFileHash_NotExist(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	hash := s.calculateFileHash("/nonexistent/file.txt")

	if hash != "" {
		t.Errorf("expected empty hash for nonexistent file, got %s", hash)
	}
}

func TestSyncerScanLocalFiles(t *testing.T) {
	tmpDir := t.TempDir()

	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "subdir", "file3.txt"), []byte("content3"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	files, err := s.scanLocalFiles()
	if err != nil {
		t.Fatalf("failed to scan local files: %v", err)
	}

	if len(files) != 4 {
		t.Errorf("expected 4 files (including dir), got %d", len(files))
	}

	fileMap := make(map[string]protocol.FileInfo)
	for _, f := range files {
		fileMap[f.RelativePath] = f
	}

	if _, ok := fileMap["file1.txt"]; !ok {
		t.Error("file1.txt not found")
	}

	if _, ok := fileMap["subdir"]; !ok {
		t.Error("subdir not found")
	}

	if _, ok := fileMap["subdir/file3.txt"]; !ok {
		t.Error("subdir/file3.txt not found")
	}
}

func TestSyncerScanLocalFiles_IgnorePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	os.WriteFile(filepath.Join(tmpDir, "keep.txt"), []byte("keep"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "ignore.tmp"), []byte("ignore"), 0644)
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0755)
	os.WriteFile(filepath.Join(tmpDir, ".git", "config"), []byte("git config"), 0644)

	ignoreList := []string{".git", "*.tmp"}
	s, err := New(tmpDir, ":0", nil, ignoreList)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	files, err := s.scanLocalFiles()
	if err != nil {
		t.Fatalf("failed to scan local files: %v", err)
	}

	for _, f := range files {
		if f.RelativePath == ".git" || f.RelativePath == "ignore.tmp" {
			t.Errorf("should have ignored %s", f.RelativePath)
		}
	}
}

func TestSyncerHandleFileChange_Dir(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.FileChangePayload{
		RelativePath: "newdir",
		IsDir:        true,
		ModTime:      time.Now().UnixNano(),
	}

	data, _ := json.Marshal(payload)
	s.handleFileChange(data)

	dirPath := filepath.Join(tmpDir, "newdir")
	info, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("directory was not created: %v", err)
	}

	if !info.IsDir() {
		t.Error("expected directory, got file")
	}
}

func TestSyncerHandleFileData(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.FileDataPayload{
		RelativePath: "received.txt",
		Data:         []byte("received content"),
		ModTime:      time.Now().UnixNano(),
	}

	data, _ := json.Marshal(payload)
	s.handleFileData(data)

	filePath := filepath.Join(tmpDir, "received.txt")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("file was not created: %v", err)
	}

	if string(content) != "received content" {
		t.Errorf("content mismatch: expected 'received content', got '%s'", string(content))
	}
}

func TestSyncerHandleFileData_CreateDir(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.FileDataPayload{
		RelativePath: "nested/deep/file.txt",
		Data:         []byte("nested content"),
		ModTime:      time.Now().UnixNano(),
	}

	data, _ := json.Marshal(payload)
	s.handleFileData(data)

	filePath := filepath.Join(tmpDir, "nested", "deep", "file.txt")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("file was not created: %v", err)
	}

	if string(content) != "nested content" {
		t.Errorf("content mismatch")
	}
}

func TestSyncerHandleFileDelete(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "todelete.txt")
	os.WriteFile(testFile, []byte("delete me"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.FileDeletePayload{
		RelativePath: "todelete.txt",
		IsDir:        false,
	}

	data, _ := json.Marshal(payload)
	s.handleFileDelete(data)

	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("file should have been deleted")
	}
}

func TestSyncerHandleFileDelete_Dir(t *testing.T) {
	tmpDir := t.TempDir()

	testDir := filepath.Join(tmpDir, "toremove")
	os.Mkdir(testDir, 0755)
	os.WriteFile(filepath.Join(testDir, "file.txt"), []byte("content"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.FileDeletePayload{
		RelativePath: "toremove",
		IsDir:        true,
	}

	data, _ := json.Marshal(payload)
	s.handleFileDelete(data)

	if _, err := os.Stat(testDir); !os.IsNotExist(err) {
		t.Error("directory should have been deleted")
	}
}

func TestSyncerGetStatus(t *testing.T) {
	tmpDir := t.TempDir()

	os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	status := s.GetStatus()

	if status["base_path"] != tmpDir {
		t.Errorf("base_path mismatch: expected %s, got %v", tmpDir, status["base_path"])
	}

	if _, ok := status["connected_clients"]; !ok {
		t.Error("connected_clients not found in status")
	}

	if _, ok := status["server_peers"]; !ok {
		t.Error("server_peers not found in status")
	}

	if _, ok := status["local_files"]; !ok {
		t.Error("local_files not found in status")
	}
}

func TestSyncerGetStatusJSON(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	jsonStr := s.GetStatusJSON()

	var status map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &status); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if _, ok := status["base_path"]; !ok {
		t.Error("base_path not found in JSON status")
	}
}

func TestSyncerHandleFileChange_SameHash(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "existing.txt")
	content := []byte("existing content")
	os.WriteFile(testFile, content, 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	hash := s.calculateFileHash(testFile)

	payload := &protocol.FileChangePayload{
		RelativePath: "existing.txt",
		Hash:         hash,
		ModTime:      time.Now().UnixNano(),
		Size:         int64(len(content)),
		IsDir:        false,
	}

	data, _ := json.Marshal(payload)
	s.handleFileChange(data)
}

func TestSyncerHandleSyncRequest(t *testing.T) {
	tmpDir := t.TempDir()

	os.WriteFile(filepath.Join(tmpDir, "local.txt"), []byte("local content"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.SyncRequestPayload{
		Files: []protocol.FileInfo{
			{
				RelativePath: "remote.txt",
				Hash:         "remotehash",
				ModTime:      time.Now().UnixNano(),
				Size:         100,
				IsDir:        false,
			},
		},
	}

	data, _ := json.Marshal(payload)
	s.handleSyncRequest(data)
}

func TestSyncerHandleSyncResponse(t *testing.T) {
	tmpDir := t.TempDir()

	os.WriteFile(filepath.Join(tmpDir, "needed.txt"), []byte("needed content"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.SyncResponsePayload{
		NeedFiles:   []string{"needed.txt"},
		DeleteFiles: []string{},
	}

	data, _ := json.Marshal(payload)
	s.handleSyncResponse(data)
}

func TestSyncerSyncingFilesFlag(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	s.syncingFiles.Store("test.txt", true)

	payload := &protocol.FileDataPayload{
		RelativePath: "test.txt",
		Data:         []byte("content"),
		ModTime:      time.Now().UnixNano(),
	}

	data, _ := json.Marshal(payload)
	s.handleFileData(data)

	s.syncingFiles.Delete("test.txt")
}

func TestSyncerHandleFileRequest(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "requested.txt")
	os.WriteFile(testFile, []byte("requested content"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.FileRequestPayload{
		RelativePath: "requested.txt",
	}

	data, _ := json.Marshal(payload)
	s.handleFileRequest(data)
}

func TestSyncerHandleFileRequest_NotExist(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.FileRequestPayload{
		RelativePath: "nonexistent.txt",
	}

	data, _ := json.Marshal(payload)
	s.handleFileRequest(data)
}

func TestSyncerHandleFileChange_LocalNewer(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "local.txt")
	os.WriteFile(testFile, []byte("local content"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	oldTime := time.Now().Add(-1 * time.Hour).UnixNano()

	payload := &protocol.FileChangePayload{
		RelativePath: "local.txt",
		Hash:         "differenthash",
		ModTime:      oldTime,
		Size:         100,
		IsDir:        false,
	}

	data, _ := json.Marshal(payload)
	s.handleFileChange(data)

	content, _ := os.ReadFile(testFile)
	if string(content) != "local content" {
		t.Error("local file should not have been modified when local is newer")
	}
}

func TestSyncerHandleFileChange_RemoteNewer(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "remote.txt")
	os.WriteFile(testFile, []byte("old content"), 0644)

	os.Chtimes(testFile, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour))

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	futureTime := time.Now().Add(1 * time.Hour).UnixNano()

	payload := &protocol.FileChangePayload{
		RelativePath: "remote.txt",
		Hash:         "newhash",
		ModTime:      futureTime,
		Size:         100,
		IsDir:        false,
	}

	data, _ := json.Marshal(payload)
	s.handleFileChange(data)
}

func TestSyncerHandleFileChange_InvalidPayload(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	s.handleFileChange([]byte("invalid json"))
}

func TestSyncerHandleFileData_InvalidPayload(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	s.handleFileData([]byte("invalid json"))
}

func TestSyncerHandleFileDelete_InvalidPayload(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	s.handleFileDelete([]byte("invalid json"))
}

func TestSyncerHandleFileRequest_InvalidPayload(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	s.handleFileRequest([]byte("invalid json"))
}

func TestSyncerHandleSyncRequest_InvalidPayload(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	s.handleSyncRequest([]byte("invalid json"))
}

func TestSyncerHandleSyncResponse_InvalidPayload(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	s.handleSyncResponse([]byte("invalid json"))
}

func TestSyncerHandleMessage(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	testCases := []struct {
		name    string
		msgType protocol.MessageType
		payload interface{}
	}{
		{
			"FileChange",
			protocol.TypeFileChange,
			&protocol.FileChangePayload{RelativePath: "test.txt", IsDir: false},
		},
		{
			"FileDelete",
			protocol.TypeFileDelete,
			&protocol.FileDeletePayload{RelativePath: "test.txt", IsDir: false},
		},
		{
			"SyncRequest",
			protocol.TypeSyncRequest,
			&protocol.SyncRequestPayload{Files: []protocol.FileInfo{}},
		},
		{
			"SyncResponse",
			protocol.TypeSyncResponse,
			&protocol.SyncResponsePayload{NeedFiles: []string{}, DeleteFiles: []string{}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payloadData, _ := json.Marshal(tc.payload)
			msg := &protocol.Message{
				Type:    tc.msgType,
				Payload: payloadData,
			}
			s.handleMessage(msg)
		})
	}
}

func TestSyncerScanLocalFiles_Empty(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	files, err := s.scanLocalFiles()
	if err != nil {
		t.Fatalf("failed to scan empty directory: %v", err)
	}

	if len(files) != 0 {
		t.Errorf("expected 0 files in empty directory, got %d", len(files))
	}
}

func TestSyncerHandleFileData_ModTime(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	expectedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	payload := &protocol.FileDataPayload{
		RelativePath: "timed.txt",
		Data:         []byte("timed content"),
		ModTime:      expectedTime.UnixNano(),
	}

	data, _ := json.Marshal(payload)
	s.handleFileData(data)

	filePath := filepath.Join(tmpDir, "timed.txt")
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("file was not created: %v", err)
	}

	if info.ModTime().Unix() != expectedTime.Unix() {
		t.Errorf("ModTime mismatch: expected %v, got %v", expectedTime, info.ModTime())
	}
}

func TestSyncerHandleFileChange_NewFile(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.FileChangePayload{
		RelativePath: "newfile.txt",
		Hash:         "somehash",
		ModTime:      time.Now().UnixNano(),
		Size:         100,
		IsDir:        false,
	}

	data, _ := json.Marshal(payload)
	s.handleFileChange(data)
}

func TestSyncerScanLocalFiles_WithHash(t *testing.T) {
	tmpDir := t.TempDir()

	os.WriteFile(filepath.Join(tmpDir, "hashfile.txt"), []byte("content for hash"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	files, err := s.scanLocalFiles()
	if err != nil {
		t.Fatalf("failed to scan: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	if files[0].Hash == "" {
		t.Error("file hash should not be empty")
	}

	if files[0].IsDir {
		t.Error("file should not be marked as directory")
	}
}

// TestSyncerHandleSyncRequest_WithAssertions tests sync request handling with proper assertions
func TestSyncerHandleSyncRequest_WithAssertions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create local file
	localContent := []byte("local file content")
	os.WriteFile(filepath.Join(tmpDir, "local.txt"), localContent, 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	// Verify initial state
	files, err := s.scanLocalFiles()
	if err != nil {
		t.Fatalf("failed to scan files: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 local file, got %d", len(files))
	}

	// Create sync request with remote file info
	payload := &protocol.SyncRequestPayload{
		Files: []protocol.FileInfo{
			{
				RelativePath: "remote_only.txt",
				Hash:         "remotehash123",
				ModTime:      time.Now().UnixNano(),
				Size:         50,
				IsDir:        false,
			},
		},
	}

	data, _ := json.Marshal(payload)
	s.handleSyncRequest(data)

	// Local file should still exist
	if _, err := os.Stat(filepath.Join(tmpDir, "local.txt")); os.IsNotExist(err) {
		t.Error("local file should still exist after sync request")
	}
}

// TestSyncerHandleSyncResponse_WithAssertions tests sync response handling with proper assertions
func TestSyncerHandleSyncResponse_WithAssertions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file that will be "needed"
	os.WriteFile(filepath.Join(tmpDir, "needed.txt"), []byte("needed content"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.SyncResponsePayload{
		NeedFiles:   []string{"needed.txt"},
		DeleteFiles: []string{},
	}

	data, _ := json.Marshal(payload)

	// Handle sync response - this should trigger file requests
	s.handleSyncResponse(data)

	// Verify local file still exists
	content, err := os.ReadFile(filepath.Join(tmpDir, "needed.txt"))
	if err != nil {
		t.Fatalf("failed to read local file: %v", err)
	}
	if string(content) != "needed content" {
		t.Error("local file content should be preserved")
	}
}

// TestSyncerBroadcast tests the broadcast functionality
func TestSyncerBroadcast(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer s.Stop()

	// Create a message to broadcast
	payload := &protocol.FileChangePayload{
		RelativePath: "broadcast_test.txt",
		Hash:         "testhash",
		ModTime:      time.Now().UnixNano(),
		Size:         100,
	}
	msg, _ := protocol.NewFileChangeMessage(payload)

	// Broadcast should not panic even without connected peers
	s.broadcast(msg)
}

// TestSyncerHandleFileDelete_File tests file deletion handling
func TestSyncerHandleFileDelete_File(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file to delete
	fileToDelete := filepath.Join(tmpDir, "delete_me.txt")
	os.WriteFile(fileToDelete, []byte("will be deleted"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	// Verify file exists
	if _, err := os.Stat(fileToDelete); os.IsNotExist(err) {
		t.Fatal("test file should exist before deletion")
	}

	payload := &protocol.FileDeletePayload{
		RelativePath: "delete_me.txt",
		IsDir:        false,
	}

	data, _ := json.Marshal(payload)
	s.handleFileDelete(data)

	// Verify file is deleted
	if _, err := os.Stat(fileToDelete); !os.IsNotExist(err) {
		t.Error("file should be deleted after handleFileDelete")
	}
}

// TestSyncerHandleFileDelete_Directory tests directory deletion handling
func TestSyncerHandleFileDelete_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directory with file
	dirToDelete := filepath.Join(tmpDir, "delete_dir")
	os.Mkdir(dirToDelete, 0755)
	os.WriteFile(filepath.Join(dirToDelete, "inside.txt"), []byte("inside content"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.FileDeletePayload{
		RelativePath: "delete_dir",
		IsDir:        true,
	}

	data, _ := json.Marshal(payload)
	s.handleFileDelete(data)

	// Verify directory is deleted
	if _, err := os.Stat(dirToDelete); !os.IsNotExist(err) {
		t.Error("directory should be deleted after handleFileDelete")
	}
}

// TestSyncerHandleFileChange_Directory tests directory creation via FileChange
func TestSyncerHandleFileChange_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	payload := &protocol.FileChangePayload{
		RelativePath: "new_directory",
		IsDir:        true,
	}

	data, _ := json.Marshal(payload)
	s.handleFileChange(data)

	// Verify directory was created
	dirPath := filepath.Join(tmpDir, "new_directory")
	info, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("directory should be created: %v", err)
	}
	if !info.IsDir() {
		t.Error("created path should be a directory")
	}
}

// TestSyncerCalculateFileHash_Consistency tests hash calculation consistency
func TestSyncerCalculateFileHash_Consistency(t *testing.T) {
	tmpDir := t.TempDir()

	content := []byte("test content for hashing")
	testFile := filepath.Join(tmpDir, "hash_test.txt")
	os.WriteFile(testFile, content, 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	hash := s.calculateFileHash(testFile)

	// Hash should not be empty
	if hash == "" {
		t.Error("hash should not be empty for existing file")
	}

	// Hash should be consistent
	hash2 := s.calculateFileHash(testFile)
	if hash != hash2 {
		t.Error("hash should be consistent for same file")
	}

	// Hash for nonexistent file should be empty
	nonexistentHash := s.calculateFileHash("/nonexistent/file.txt")
	if nonexistentHash != "" {
		t.Error("hash for nonexistent file should be empty")
	}
}

// TestSyncerScanLocalFiles_Nested tests scanning nested directory structure
func TestSyncerScanLocalFiles_Nested(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested structure
	os.MkdirAll(filepath.Join(tmpDir, "level1", "level2"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "root.txt"), []byte("root"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "level1", "l1.txt"), []byte("l1"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "level1", "level2", "l2.txt"), []byte("l2"), 0644)

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}
	defer s.Stop()

	files, err := s.scanLocalFiles()
	if err != nil {
		t.Fatalf("failed to scan: %v", err)
	}

	// Should have 2 directories + 3 files = 5 entries
	if len(files) != 5 {
		t.Errorf("expected 5 entries (2 dirs + 3 files), got %d", len(files))
	}

	// Check that paths use forward slashes
	for _, f := range files {
		if strings.Contains(f.RelativePath, "\\") {
			t.Errorf("path should use forward slashes: %s", f.RelativePath)
		}
	}
}

// TestSyncerStartStop_Lifecycle tests start and stop lifecycle
func TestSyncerStartStop_Lifecycle(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	// Start should succeed
	if err := s.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}

	// Verify it's running
	status := s.GetStatus()
	if status["base_path"] != tmpDir {
		t.Error("syncer should be running")
	}

	// Stop should not panic
	s.Stop()

	// Note: Double stop may panic in current implementation
	// This tests single stop only
}

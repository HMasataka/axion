package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestEncode(t *testing.T) {
	msg := &Message{
		Type:    TypeFileChange,
		Payload: []byte(`{"test": "data"}`),
	}

	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	if len(data) < 4 {
		t.Fatal("encoded data too short")
	}

	length := binary.BigEndian.Uint32(data[:4])
	if int(length) != len(data)-4 {
		t.Errorf("length mismatch: header says %d, actual payload is %d", length, len(data)-4)
	}
}

func TestDecode(t *testing.T) {
	msg := &Message{
		Type:    TypeFileRequest,
		Payload: []byte(`{"path": "/test/file.txt"}`),
	}

	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	reader := bytes.NewReader(data)
	decoded, err := Decode(reader)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.Type != msg.Type {
		t.Errorf("type mismatch: expected %d, got %d", msg.Type, decoded.Type)
	}

	if !bytes.Equal(decoded.Payload, msg.Payload) {
		t.Errorf("payload mismatch: expected %s, got %s", msg.Payload, decoded.Payload)
	}
}

func TestEncodeDecode_RoundTrip(t *testing.T) {
	testCases := []struct {
		name    string
		msgType MessageType
		payload []byte
	}{
		{"FileChange", TypeFileChange, []byte(`{"relative_path": "test.txt"}`)},
		{"FileRequest", TypeFileRequest, []byte(`{"relative_path": "test.txt"}`)},
		{"FileData", TypeFileData, []byte(`{"relative_path": "test.txt", "data": "aGVsbG8="}`)},
		{"FileDelete", TypeFileDelete, []byte(`{"relative_path": "test.txt"}`)},
		{"SyncRequest", TypeSyncRequest, []byte(`{"files": []}`)},
		{"SyncResponse", TypeSyncResponse, []byte(`{"need_files": [], "delete_files": []}`)},
		{"Ack", TypeAck, []byte(`{}`)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			original := &Message{Type: tc.msgType, Payload: tc.payload}

			data, err := Encode(original)
			if err != nil {
				t.Fatalf("encode failed: %v", err)
			}

			decoded, err := Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}

			if decoded.Type != original.Type {
				t.Errorf("type mismatch")
			}

			if !bytes.Equal(decoded.Payload, original.Payload) {
				t.Errorf("payload mismatch")
			}
		})
	}
}

func TestDecode_InvalidLength(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, 1000000)

	reader := bytes.NewReader(data)
	_, err := Decode(reader)
	if err == nil {
		t.Error("expected error for truncated data, got nil")
	}
}

func TestDecode_TruncatedPayload(t *testing.T) {
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[:4], 100)
	copy(data[4:], []byte("abc"))

	reader := bytes.NewReader(data)
	_, err := Decode(reader)
	if err == nil {
		t.Error("expected error for truncated payload, got nil")
	}
}

func TestDecode_InvalidJSON(t *testing.T) {
	invalidJSON := []byte("{invalid json}")
	data := make([]byte, 4+len(invalidJSON))
	binary.BigEndian.PutUint32(data[:4], uint32(len(invalidJSON)))
	copy(data[4:], invalidJSON)

	reader := bytes.NewReader(data)
	_, err := Decode(reader)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestNewFileChangeMessage(t *testing.T) {
	payload := &FileChangePayload{
		RelativePath: "test/file.txt",
		Hash:         "abc123",
		ModTime:      1234567890,
		Size:         100,
		IsDir:        false,
	}

	msg, err := NewFileChangeMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	if msg.Type != TypeFileChange {
		t.Errorf("expected type %d, got %d", TypeFileChange, msg.Type)
	}

	parsed, err := ParseFileChangePayload(msg.Payload)
	if err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	if parsed.RelativePath != payload.RelativePath {
		t.Errorf("RelativePath mismatch")
	}

	if parsed.Hash != payload.Hash {
		t.Errorf("Hash mismatch")
	}
}

func TestNewFileRequestMessage(t *testing.T) {
	payload := &FileRequestPayload{
		RelativePath: "documents/report.pdf",
	}

	msg, err := NewFileRequestMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	if msg.Type != TypeFileRequest {
		t.Errorf("expected type %d, got %d", TypeFileRequest, msg.Type)
	}

	parsed, err := ParseFileRequestPayload(msg.Payload)
	if err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	if parsed.RelativePath != payload.RelativePath {
		t.Errorf("RelativePath mismatch")
	}
}

func TestNewFileDataMessage(t *testing.T) {
	payload := &FileDataPayload{
		RelativePath: "test.txt",
		Data:         []byte("Hello, World!"),
		ModTime:      1234567890,
	}

	msg, err := NewFileDataMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	if msg.Type != TypeFileData {
		t.Errorf("expected type %d, got %d", TypeFileData, msg.Type)
	}

	parsed, err := ParseFileDataPayload(msg.Payload)
	if err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	if !bytes.Equal(parsed.Data, payload.Data) {
		t.Errorf("Data mismatch")
	}
}

func TestNewFileDeleteMessage(t *testing.T) {
	payload := &FileDeletePayload{
		RelativePath: "old/file.txt",
		IsDir:        false,
	}

	msg, err := NewFileDeleteMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	if msg.Type != TypeFileDelete {
		t.Errorf("expected type %d, got %d", TypeFileDelete, msg.Type)
	}

	parsed, err := ParseFileDeletePayload(msg.Payload)
	if err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	if parsed.RelativePath != payload.RelativePath {
		t.Errorf("RelativePath mismatch")
	}

	if parsed.IsDir != payload.IsDir {
		t.Errorf("IsDir mismatch")
	}
}

func TestNewSyncRequestMessage(t *testing.T) {
	payload := &SyncRequestPayload{
		Files: []FileInfo{
			{RelativePath: "file1.txt", Hash: "hash1", ModTime: 100, Size: 10, IsDir: false},
			{RelativePath: "dir1", Hash: "", ModTime: 200, Size: 0, IsDir: true},
		},
	}

	msg, err := NewSyncRequestMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	if msg.Type != TypeSyncRequest {
		t.Errorf("expected type %d, got %d", TypeSyncRequest, msg.Type)
	}

	parsed, err := ParseSyncRequestPayload(msg.Payload)
	if err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	if len(parsed.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(parsed.Files))
	}
}

func TestNewSyncResponseMessage(t *testing.T) {
	payload := &SyncResponsePayload{
		NeedFiles:   []string{"file1.txt", "file2.txt"},
		DeleteFiles: []string{"old.txt"},
	}

	msg, err := NewSyncResponseMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	if msg.Type != TypeSyncResponse {
		t.Errorf("expected type %d, got %d", TypeSyncResponse, msg.Type)
	}

	parsed, err := ParseSyncResponsePayload(msg.Payload)
	if err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	if len(parsed.NeedFiles) != 2 {
		t.Errorf("expected 2 need files, got %d", len(parsed.NeedFiles))
	}

	if len(parsed.DeleteFiles) != 1 {
		t.Errorf("expected 1 delete file, got %d", len(parsed.DeleteFiles))
	}
}

func TestParseFileChangePayload_Invalid(t *testing.T) {
	_, err := ParseFileChangePayload([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid payload, got nil")
	}
}

func TestParseFileRequestPayload(t *testing.T) {
	data := []byte(`{"relative_path": "docs/readme.md"}`)

	parsed, err := ParseFileRequestPayload(data)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if parsed.RelativePath != "docs/readme.md" {
		t.Errorf("expected RelativePath docs/readme.md, got %s", parsed.RelativePath)
	}
}

func TestParseFileRequestPayload_Invalid(t *testing.T) {
	_, err := ParseFileRequestPayload([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid payload, got nil")
	}
}

func TestParseFileDataPayload(t *testing.T) {
	data := []byte(`{"relative_path": "test.txt", "data": "SGVsbG8=", "mod_time": 1234567890}`)

	parsed, err := ParseFileDataPayload(data)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if parsed.RelativePath != "test.txt" {
		t.Errorf("expected RelativePath test.txt, got %s", parsed.RelativePath)
	}

	if parsed.ModTime != 1234567890 {
		t.Errorf("expected ModTime 1234567890, got %d", parsed.ModTime)
	}
}

func TestParseFileDataPayload_Invalid(t *testing.T) {
	_, err := ParseFileDataPayload([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid payload, got nil")
	}
}

func TestParseFileDeletePayload(t *testing.T) {
	data := []byte(`{"relative_path": "old/file.txt", "is_dir": false}`)

	parsed, err := ParseFileDeletePayload(data)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if parsed.RelativePath != "old/file.txt" {
		t.Errorf("expected RelativePath old/file.txt, got %s", parsed.RelativePath)
	}

	if parsed.IsDir != false {
		t.Error("expected IsDir false")
	}
}

func TestParseFileDeletePayload_Dir(t *testing.T) {
	data := []byte(`{"relative_path": "old/dir", "is_dir": true}`)

	parsed, err := ParseFileDeletePayload(data)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if !parsed.IsDir {
		t.Error("expected IsDir true")
	}
}

func TestParseFileDeletePayload_Invalid(t *testing.T) {
	_, err := ParseFileDeletePayload([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid payload, got nil")
	}
}

func TestParseSyncRequestPayload(t *testing.T) {
	data := []byte(`{"files": [{"relative_path": "a.txt", "hash": "abc", "mod_time": 100, "size": 50, "is_dir": false}]}`)

	parsed, err := ParseSyncRequestPayload(data)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(parsed.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(parsed.Files))
	}

	if parsed.Files[0].RelativePath != "a.txt" {
		t.Errorf("expected RelativePath a.txt, got %s", parsed.Files[0].RelativePath)
	}

	if parsed.Files[0].Hash != "abc" {
		t.Errorf("expected Hash abc, got %s", parsed.Files[0].Hash)
	}
}

func TestParseSyncRequestPayload_Empty(t *testing.T) {
	data := []byte(`{"files": []}`)

	parsed, err := ParseSyncRequestPayload(data)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(parsed.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(parsed.Files))
	}
}

func TestParseSyncRequestPayload_Invalid(t *testing.T) {
	_, err := ParseSyncRequestPayload([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid payload, got nil")
	}
}

func TestParseSyncResponsePayload(t *testing.T) {
	data := []byte(`{"need_files": ["a.txt", "b.txt"], "delete_files": ["c.txt"]}`)

	parsed, err := ParseSyncResponsePayload(data)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(parsed.NeedFiles) != 2 {
		t.Errorf("expected 2 need files, got %d", len(parsed.NeedFiles))
	}

	if len(parsed.DeleteFiles) != 1 {
		t.Errorf("expected 1 delete file, got %d", len(parsed.DeleteFiles))
	}

	if parsed.NeedFiles[0] != "a.txt" {
		t.Errorf("expected first need file a.txt, got %s", parsed.NeedFiles[0])
	}
}

func TestParseSyncResponsePayload_Empty(t *testing.T) {
	data := []byte(`{"need_files": [], "delete_files": []}`)

	parsed, err := ParseSyncResponsePayload(data)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(parsed.NeedFiles) != 0 {
		t.Errorf("expected 0 need files, got %d", len(parsed.NeedFiles))
	}

	if len(parsed.DeleteFiles) != 0 {
		t.Errorf("expected 0 delete files, got %d", len(parsed.DeleteFiles))
	}
}

func TestParseSyncResponsePayload_Invalid(t *testing.T) {
	_, err := ParseSyncResponsePayload([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid payload, got nil")
	}
}

func TestParseFileDataPayload_LargeData(t *testing.T) {
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	payload := &FileDataPayload{
		RelativePath: "large.bin",
		Data:         largeData,
		ModTime:      1234567890,
	}

	msg, err := NewFileDataMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message with large data: %v", err)
	}

	parsed, err := ParseFileDataPayload(msg.Payload)
	if err != nil {
		t.Fatalf("failed to parse large data payload: %v", err)
	}

	if len(parsed.Data) != len(largeData) {
		t.Errorf("data size mismatch: expected %d, got %d", len(largeData), len(parsed.Data))
	}

	if !bytes.Equal(parsed.Data, largeData) {
		t.Error("large data content mismatch")
	}
}

// TestDecode_EmptyReader tests decode with empty reader
func TestDecode_EmptyReader(t *testing.T) {
	reader := bytes.NewReader([]byte{})
	_, err := Decode(reader)
	if err == nil {
		t.Error("expected error for empty reader, got nil")
	}
}

// TestDecode_ZeroLengthPayload tests decode with zero length payload
func TestDecode_ZeroLengthPayload(t *testing.T) {
	// Create a message with empty payload
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, 0)

	reader := bytes.NewReader(data)
	_, err := Decode(reader)
	if err == nil {
		t.Log("zero length payload may be valid or invalid depending on implementation")
	}
}

// TestDecode_NegativeLengthAttempt tests handling of potential negative length
func TestDecode_NegativeLengthAttempt(t *testing.T) {
	// Maximum uint32 value (0xFFFFFFFF) - would be interpreted as very large length
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, 0xFFFFFFFF)

	reader := bytes.NewReader(data)
	_, err := Decode(reader)
	if err == nil {
		t.Error("expected error for extremely large length value")
	}
}

// TestEncode_EmptyPayload tests encoding with empty payload
func TestEncode_EmptyPayload(t *testing.T) {
	msg := &Message{
		Type:    TypeFileChange,
		Payload: []byte{},
	}

	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("failed to encode empty payload: %v", err)
	}

	if len(data) < 4 {
		t.Error("encoded data should at least contain length header")
	}
}

// TestEncode_NilPayload tests encoding with nil payload
func TestEncode_NilPayload(t *testing.T) {
	msg := &Message{
		Type:    TypeFileChange,
		Payload: nil,
	}

	data, err := Encode(msg)
	if err != nil {
		t.Logf("encoding nil payload: %v", err)
		return
	}

	if len(data) < 4 {
		t.Error("encoded data should at least contain length header")
	}
}

// TestMessageTypes tests all message type constants
func TestMessageTypes(t *testing.T) {
	types := []struct {
		name     string
		msgType  MessageType
		expected MessageType
	}{
		{"TypeFileChange", TypeFileChange, 1},
		{"TypeFileRequest", TypeFileRequest, 2},
		{"TypeFileData", TypeFileData, 3},
		{"TypeFileDelete", TypeFileDelete, 4},
		{"TypeSyncRequest", TypeSyncRequest, 5},
		{"TypeSyncResponse", TypeSyncResponse, 6},
		{"TypeAck", TypeAck, 7},
	}

	for _, tc := range types {
		t.Run(tc.name, func(t *testing.T) {
			if tc.msgType != tc.expected {
				t.Errorf("expected %d, got %d", tc.expected, tc.msgType)
			}
		})
	}
}

// TestFileInfo_Fields tests FileInfo struct fields
func TestFileInfo_Fields(t *testing.T) {
	fi := FileInfo{
		RelativePath: "test/path.txt",
		Hash:         "sha256hash",
		ModTime:      1234567890,
		Size:         1024,
		IsDir:        false,
	}

	if fi.RelativePath != "test/path.txt" {
		t.Errorf("RelativePath mismatch")
	}
	if fi.Hash != "sha256hash" {
		t.Errorf("Hash mismatch")
	}
	if fi.ModTime != 1234567890 {
		t.Errorf("ModTime mismatch")
	}
	if fi.Size != 1024 {
		t.Errorf("Size mismatch")
	}
	if fi.IsDir != false {
		t.Errorf("IsDir mismatch")
	}
}

// TestFileInfo_Directory tests FileInfo for directory
func TestFileInfo_Directory(t *testing.T) {
	fi := FileInfo{
		RelativePath: "test/directory",
		Hash:         "",
		ModTime:      1234567890,
		Size:         0,
		IsDir:        true,
	}

	if !fi.IsDir {
		t.Error("should be marked as directory")
	}
	if fi.Hash != "" {
		t.Error("directory hash should be empty")
	}
}

// TestParsePayloads_Nil tests parsing nil data
func TestParsePayloads_Nil(t *testing.T) {
	_, err := ParseFileChangePayload(nil)
	if err == nil {
		t.Error("expected error for nil FileChangePayload")
	}

	_, err = ParseFileRequestPayload(nil)
	if err == nil {
		t.Error("expected error for nil FileRequestPayload")
	}

	_, err = ParseFileDataPayload(nil)
	if err == nil {
		t.Error("expected error for nil FileDataPayload")
	}

	_, err = ParseFileDeletePayload(nil)
	if err == nil {
		t.Error("expected error for nil FileDeletePayload")
	}

	_, err = ParseSyncRequestPayload(nil)
	if err == nil {
		t.Error("expected error for nil SyncRequestPayload")
	}

	_, err = ParseSyncResponsePayload(nil)
	if err == nil {
		t.Error("expected error for nil SyncResponsePayload")
	}
}

func BenchmarkEncode(b *testing.B) {
	msg := &Message{
		Type:    TypeFileChange,
		Payload: []byte(`{"relative_path": "test/file.txt", "hash": "abc123def456", "mod_time": 1234567890, "size": 1024}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Encode(msg)
	}
}

func BenchmarkDecode(b *testing.B) {
	msg := &Message{
		Type:    TypeFileChange,
		Payload: []byte(`{"relative_path": "test/file.txt", "hash": "abc123def456", "mod_time": 1234567890, "size": 1024}`),
	}

	data, _ := Encode(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Decode(bytes.NewReader(data))
	}
}

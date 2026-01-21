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

// BenchmarkProtocol_Encode benchmarks message encoding
func BenchmarkProtocol_Encode(b *testing.B) {
	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"relative_path":"benchmark.txt","hash":"abc123","mod_time":1234567890,"size":1024}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := protocol.Encode(msg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProtocol_Decode benchmarks message decoding
func BenchmarkProtocol_Decode(b *testing.B) {
	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"relative_path":"benchmark.txt","hash":"abc123","mod_time":1234567890,"size":1024}`),
	}

	encoded, _ := protocol.Encode(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, w, _ := os.Pipe()
		go func() {
			w.Write(encoded)
			w.Close()
		}()

		_, err := protocol.Decode(r)
		r.Close()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProtocol_EncodeDecode benchmarks full encode/decode cycle
func BenchmarkProtocol_EncodeDecode(b *testing.B) {
	msg := &protocol.Message{
		Type:    protocol.TypeFileData,
		Payload: []byte(`{"relative_path":"data.txt","data":"SGVsbG8gV29ybGQh","mod_time":1234567890}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, err := protocol.Encode(msg)
		if err != nil {
			b.Fatal(err)
		}

		r, w, _ := os.Pipe()
		go func() {
			w.Write(encoded)
			w.Close()
		}()

		_, err = protocol.Decode(r)
		r.Close()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHash_SHA256 benchmarks SHA256 hashing
func BenchmarkHash_SHA256(b *testing.B) {
	data := make([]byte, 1024) // 1KB
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := sha256.Sum256(data)
		_ = hex.EncodeToString(h[:])
	}
}

// BenchmarkHash_SHA256_Large benchmarks SHA256 hashing for larger files
func BenchmarkHash_SHA256_Large(b *testing.B) {
	data := make([]byte, 1024*1024) // 1MB
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := sha256.Sum256(data)
		_ = hex.EncodeToString(h[:])
	}
}

// BenchmarkPayload_Parse benchmarks payload parsing
func BenchmarkPayload_Parse(b *testing.B) {
	payload := []byte(`{"relative_path":"path/to/file.txt","hash":"abc123def456","mod_time":1234567890000000000,"size":4096,"is_dir":false}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := protocol.ParseFileChangePayload(payload)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPayload_Create benchmarks payload creation
func BenchmarkPayload_Create(b *testing.B) {
	payload := &protocol.FileChangePayload{
		RelativePath: "path/to/file.txt",
		Hash:         "abc123def456",
		ModTime:      1234567890000000000,
		Size:         4096,
		IsDir:        false,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := protocol.NewFileChangeMessage(payload)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFileWrite benchmarks file writing
func BenchmarkFileWrite(b *testing.B) {
	tmpDir := b.TempDir()
	data := make([]byte, 1024) // 1KB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := filepath.Join(tmpDir, "bench.txt")
		if err := os.WriteFile(path, data, 0644); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFileRead benchmarks file reading
func BenchmarkFileRead(b *testing.B) {
	tmpDir := b.TempDir()
	data := make([]byte, 1024) // 1KB
	path := filepath.Join(tmpDir, "bench.txt")
	os.WriteFile(path, data, 0644)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConnection benchmarks connection establishment
func BenchmarkConnection(b *testing.B) {
	server := peer.NewServer(":0")
	if err := server.Start(); err != nil {
		b.Fatal(err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client := peer.NewClient(addr)
		if err := client.Connect(); err != nil {
			b.Fatal(err)
		}
		client.Close()
	}
}

// BenchmarkMessageSend benchmarks message sending
func BenchmarkMessageSend(b *testing.B) {
	server := peer.NewServer(":0")
	server.SetPeerHandler(func(p *peer.Peer) {
		p.SetMessageHandler(func(msg *protocol.Message) {
			// Discard received messages
		})
	})

	if err := server.Start(); err != nil {
		b.Fatal(err)
	}
	defer server.Stop()

	addr := server.GetListenAddr()

	client := peer.NewClient(addr)
	if err := client.Connect(); err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	time.Sleep(50 * time.Millisecond)

	msg := &protocol.Message{
		Type:    protocol.TypeFileChange,
		Payload: []byte(`{"relative_path":"bench.txt"}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := client.Send(msg); err != nil {
			// May fail due to channel full, continue
		}
	}
}

// BenchmarkSyncerStatus benchmarks syncer status retrieval
func BenchmarkSyncerStatus(b *testing.B) {
	tmpDir := b.TempDir()

	// Create some test files
	for i := 0; i < 10; i++ {
		path := filepath.Join(tmpDir, "file"+string(rune('0'+i))+".txt")
		os.WriteFile(path, []byte("content"), 0644)
	}

	s, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		b.Fatal(err)
	}

	if err := s.Start(); err != nil {
		b.Fatal(err)
	}
	defer s.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.GetStatus()
	}
}

// BenchmarkSyncerStatusJSON benchmarks syncer JSON status retrieval
func BenchmarkSyncerStatusJSON(b *testing.B) {
	tmpDir := b.TempDir()

	s, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		b.Fatal(err)
	}

	if err := s.Start(); err != nil {
		b.Fatal(err)
	}
	defer s.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.GetStatusJSON()
	}
}

// BenchmarkPathConversion benchmarks path conversion operations
func BenchmarkPathConversion(b *testing.B) {
	path := "dir/subdir/deep/nested/file.txt"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		platformPath := filepath.FromSlash(path)
		_ = filepath.ToSlash(platformPath)
	}
}

// BenchmarkFileInfo benchmarks file info retrieval
func BenchmarkFileInfo(b *testing.B) {
	tmpDir := b.TempDir()
	path := filepath.Join(tmpDir, "bench.txt")
	os.WriteFile(path, []byte("content"), 0644)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := os.Stat(path)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDirRead benchmarks directory reading
func BenchmarkDirRead(b *testing.B) {
	tmpDir := b.TempDir()

	// Create some files
	for i := 0; i < 100; i++ {
		path := filepath.Join(tmpDir, "file"+string(rune(i))+".txt")
		os.WriteFile(path, []byte("content"), 0644)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := os.ReadDir(tmpDir)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFilepath_Walk benchmarks directory walking
func BenchmarkFilepath_Walk(b *testing.B) {
	tmpDir := b.TempDir()

	// Create nested structure
	for i := 0; i < 3; i++ {
		subdir := filepath.Join(tmpDir, "dir"+string(rune('0'+i)))
		os.Mkdir(subdir, 0755)
		for j := 0; j < 10; j++ {
			path := filepath.Join(subdir, "file"+string(rune('0'+j))+".txt")
			os.WriteFile(path, []byte("content"), 0644)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
			return nil
		})
	}
}

// BenchmarkLargePayload benchmarks handling of large payloads
func BenchmarkLargePayload(b *testing.B) {
	// 1MB payload
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	payload := &protocol.FileDataPayload{
		RelativePath: "large.bin",
		Data:         data,
		ModTime:      time.Now().UnixNano(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg, err := protocol.NewFileDataMessage(payload)
		if err != nil {
			b.Fatal(err)
		}

		encoded, err := protocol.Encode(msg)
		if err != nil {
			b.Fatal(err)
		}

		r, w, _ := os.Pipe()
		go func() {
			w.Write(encoded)
			w.Close()
		}()

		_, err = protocol.Decode(r)
		r.Close()
		if err != nil {
			b.Fatal(err)
		}
	}
}

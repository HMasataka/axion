package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherNew(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	if w.basePath != tmpDir {
		t.Errorf("basePath mismatch: expected %s, got %s", tmpDir, w.basePath)
	}
}

func TestWatcherStartStop(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	w.Stop()
}

func TestWatcherEvents_Create(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	if err := w.Start(); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	testFile := filepath.Join(tmpDir, "test.txt")

	go func() {
		time.Sleep(50 * time.Millisecond)
		os.WriteFile(testFile, []byte("hello"), 0644)
	}()

	// Wait for at least one relevant event (may receive multiple events)
	timeout := time.After(2 * time.Second)
	foundEvent := false

	for !foundEvent {
		select {
		case event := <-w.Events():
			if event.RelativePath == "test.txt" && (event.Op == OpCreate || event.Op == OpWrite) {
				foundEvent = true
			}
		case err := <-w.Errors():
			t.Fatalf("watcher error: %v", err)
		case <-timeout:
			t.Fatal("timeout waiting for create event")
		}
	}

	if !foundEvent {
		t.Error("expected to find create/write event for test.txt")
	}
}

func TestWatcherEvents_Write(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "existing.txt")
	os.WriteFile(testFile, []byte("initial"), 0644)

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	if err := w.Start(); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		os.WriteFile(testFile, []byte("updated content"), 0644)
	}()

	// Wait for at least one write event (may receive multiple events)
	timeout := time.After(2 * time.Second)
	foundEvent := false

	for !foundEvent {
		select {
		case event := <-w.Events():
			if event.RelativePath == "existing.txt" && event.Op == OpWrite {
				foundEvent = true
			}
		case err := <-w.Errors():
			t.Fatalf("watcher error: %v", err)
		case <-timeout:
			t.Fatal("timeout waiting for write event")
		}
	}

	if !foundEvent {
		t.Error("expected to find write event for existing.txt")
	}
}

func TestWatcherEvents_Remove(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "toremove.txt")
	os.WriteFile(testFile, []byte("delete me"), 0644)

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	if err := w.Start(); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		os.Remove(testFile)
	}()

	// Wait for at least one relevant event (may receive multiple events)
	timeout := time.After(2 * time.Second)
	foundEvent := false

	for !foundEvent {
		select {
		case event := <-w.Events():
			// Accept Remove or Rename (some OS report rename for deletion)
			if event.RelativePath == "toremove.txt" && (event.Op == OpRemove || event.Op == OpRename) {
				foundEvent = true
			}
		case err := <-w.Errors():
			t.Fatalf("watcher error: %v", err)
		case <-timeout:
			t.Fatal("timeout waiting for remove event")
		}
	}

	if !foundEvent {
		t.Error("expected to find remove/rename event for toremove.txt")
	}
}

func TestWatcherEvents_DirCreate(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	if err := w.Start(); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	newDir := filepath.Join(tmpDir, "newdir")

	go func() {
		time.Sleep(50 * time.Millisecond)
		os.Mkdir(newDir, 0755)
	}()

	// Wait for at least one relevant event (may receive multiple events)
	timeout := time.After(2 * time.Second)
	foundEvent := false

	for !foundEvent {
		select {
		case event := <-w.Events():
			if event.RelativePath == "newdir" && event.Op == OpCreate {
				if !event.IsDir {
					t.Error("expected IsDir to be true for directory creation")
				}
				foundEvent = true
			}
		case err := <-w.Errors():
			t.Fatalf("watcher error: %v", err)
		case <-timeout:
			t.Fatal("timeout waiting for dir create event")
		}
	}

	if !foundEvent {
		t.Error("expected to find create event for newdir")
	}
}

func TestWatcherShouldIgnore_Git(t *testing.T) {
	tmpDir := t.TempDir()
	ignoreList := []string{".git"}

	w, err := New(tmpDir, ignoreList)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	gitDir := filepath.Join(tmpDir, ".git")
	if !w.shouldIgnore(gitDir) {
		t.Error("expected .git to be ignored")
	}
}

func TestWatcherShouldIgnore_DSStore(t *testing.T) {
	tmpDir := t.TempDir()
	ignoreList := []string{".DS_Store"}

	w, err := New(tmpDir, ignoreList)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	dsStore := filepath.Join(tmpDir, ".DS_Store")
	if !w.shouldIgnore(dsStore) {
		t.Error("expected .DS_Store to be ignored")
	}
}

func TestWatcherShouldIgnore_ThumbsDb(t *testing.T) {
	tmpDir := t.TempDir()
	ignoreList := []string{"Thumbs.db"}

	w, err := New(tmpDir, ignoreList)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	thumbsDb := filepath.Join(tmpDir, "Thumbs.db")
	if !w.shouldIgnore(thumbsDb) {
		t.Error("expected Thumbs.db to be ignored")
	}
}

func TestWatcherShouldIgnore_TmpFiles(t *testing.T) {
	tmpDir := t.TempDir()
	ignoreList := []string{"*.tmp"}

	w, err := New(tmpDir, ignoreList)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	tmpFile := filepath.Join(tmpDir, "test.tmp")
	if !w.shouldIgnore(tmpFile) {
		t.Error("expected *.tmp to be ignored")
	}
}

func TestWatcherShouldIgnore_SwpFiles(t *testing.T) {
	tmpDir := t.TempDir()
	ignoreList := []string{"*.swp"}

	w, err := New(tmpDir, ignoreList)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	swpFile := filepath.Join(tmpDir, "file.swp")
	if !w.shouldIgnore(swpFile) {
		t.Error("expected *.swp to be ignored")
	}
}

func TestWatcherShouldIgnore_BackupFiles(t *testing.T) {
	tmpDir := t.TempDir()
	ignoreList := []string{"*~"}

	w, err := New(tmpDir, ignoreList)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	backupFile := filepath.Join(tmpDir, "file.txt~")
	if !w.shouldIgnore(backupFile) {
		t.Error("expected *~ to be ignored")
	}
}

func TestWatcherShouldIgnore_CustomPattern(t *testing.T) {
	tmpDir := t.TempDir()
	ignoreList := []string{"node_modules", "*.log"}

	w, err := New(tmpDir, ignoreList)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	nodeModules := filepath.Join(tmpDir, "node_modules")
	if !w.shouldIgnore(nodeModules) {
		t.Error("expected node_modules to be ignored")
	}

	logFile := filepath.Join(tmpDir, "app.log")
	if !w.shouldIgnore(logFile) {
		t.Error("expected *.log to be ignored")
	}
}

func TestWatcherShouldNotIgnore(t *testing.T) {
	tmpDir := t.TempDir()
	ignoreList := []string{".git", "*.tmp"}

	w, err := New(tmpDir, ignoreList)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	normalFile := filepath.Join(tmpDir, "document.txt")
	if w.shouldIgnore(normalFile) {
		t.Error("expected document.txt to not be ignored")
	}
}

func TestWatcherRecursive(t *testing.T) {
	tmpDir := t.TempDir()

	subDir := filepath.Join(tmpDir, "subdir")
	os.Mkdir(subDir, 0755)

	deepDir := filepath.Join(subDir, "deep")
	os.Mkdir(deepDir, 0755)

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	if err := w.Start(); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	deepFile := filepath.Join(deepDir, "nested.txt")

	go func() {
		time.Sleep(50 * time.Millisecond)
		os.WriteFile(deepFile, []byte("nested content"), 0644)
	}()

	// Wait for at least one relevant event (may receive multiple events)
	timeout := time.After(2 * time.Second)
	foundEvent := false
	expectedPath := "subdir/deep/nested.txt"

	for !foundEvent {
		select {
		case event := <-w.Events():
			if event.RelativePath == expectedPath && (event.Op == OpCreate || event.Op == OpWrite) {
				foundEvent = true
			}
		case err := <-w.Errors():
			t.Fatalf("watcher error: %v", err)
		case <-timeout:
			t.Fatal("timeout waiting for nested file event")
		}
	}

	if !foundEvent {
		t.Errorf("expected to find create/write event for %s", expectedPath)
	}
}

func TestWatcherCalculateHash(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "hashtest.txt")
	content := []byte("Hello, World!")
	os.WriteFile(testFile, content, 0644)

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	hash, err := w.calculateHash(testFile)
	if err != nil {
		t.Fatalf("failed to calculate hash: %v", err)
	}

	if hash == "" {
		t.Error("hash should not be empty")
	}

	expectedHash := "dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f"
	if hash != expectedHash {
		t.Errorf("hash mismatch: expected %s, got %s", expectedHash, hash)
	}
}

func TestWatcherCalculateHash_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "empty.txt")
	os.WriteFile(testFile, []byte{}, 0644)

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	hash, err := w.calculateHash(testFile)
	if err != nil {
		t.Fatalf("failed to calculate hash for empty file: %v", err)
	}

	expectedHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if hash != expectedHash {
		t.Errorf("empty file hash mismatch: expected %s, got %s", expectedHash, hash)
	}
}

func TestWatcherGetSetFileHash(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	hash := w.GetFileHash("test.txt")
	if hash != "" {
		t.Error("expected empty hash for non-existent key")
	}

	w.SetFileHash("test.txt", "abc123")

	hash = w.GetFileHash("test.txt")
	if hash != "abc123" {
		t.Errorf("expected hash abc123, got %s", hash)
	}
}

func TestWatcherEvents_Debounce(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "debounce.txt")
	os.WriteFile(testFile, []byte("initial"), 0644)

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	if err := w.Start(); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		for i := 0; i < 5; i++ {
			os.WriteFile(testFile, []byte("update"+string(rune('0'+i))), 0644)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	eventCount := 0
	timeout := time.After(2 * time.Second)

loop:
	for {
		select {
		case <-w.Events():
			eventCount++
		case <-timeout:
			break loop
		}
	}

	if eventCount > 3 {
		t.Errorf("debounce may not be working properly, got %d events for 5 rapid writes", eventCount)
	}
}

func TestWatcherEvents_Rename(t *testing.T) {
	tmpDir := t.TempDir()

	oldFile := filepath.Join(tmpDir, "oldname.txt")
	newFile := filepath.Join(tmpDir, "newname.txt")
	os.WriteFile(oldFile, []byte("content"), 0644)

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	if err := w.Start(); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		os.Rename(oldFile, newFile)
	}()

	eventTypes := make(map[Op]bool)
	timeout := time.After(2 * time.Second)

loop:
	for {
		select {
		case event := <-w.Events():
			eventTypes[event.Op] = true
			if len(eventTypes) >= 2 {
				break loop
			}
		case err := <-w.Errors():
			t.Fatalf("watcher error: %v", err)
		case <-timeout:
			break loop
		}
	}

	if !eventTypes[OpRename] && !eventTypes[OpRemove] {
		t.Error("expected Rename or Remove event for old file")
	}
}

func TestWatcherCalculateHash_NotExist(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	_, err = w.calculateHash(filepath.Join(tmpDir, "nonexistent.txt"))
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestWatcherChannels(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	events := w.Events()
	if events == nil {
		t.Error("Events channel should not be nil")
	}

	errors := w.Errors()
	if errors == nil {
		t.Error("Errors channel should not be nil")
	}
}

func TestWatcherShouldIgnore_NestedPath(t *testing.T) {
	tmpDir := t.TempDir()
	ignoreList := []string{".git"}

	w, err := New(tmpDir, ignoreList)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	nestedGit := filepath.Join(tmpDir, ".git", "objects", "pack")
	if !w.shouldIgnore(nestedGit) {
		t.Error("expected nested .git path to be ignored")
	}
}

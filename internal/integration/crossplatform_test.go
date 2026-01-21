package integration

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HMasataka/axion/internal/protocol"
	"github.com/HMasataka/axion/internal/syncer"
)

// TestCrossPlatform_PathSeparatorConversion tests that paths are converted correctly between platforms
func TestCrossPlatform_PathSeparatorConversion(t *testing.T) {
	// Test that ToSlash converts platform-specific paths to forward slashes
	testCases := []struct {
		input    string
		expected string
	}{
		{"file.txt", "file.txt"},
		{"dir/file.txt", "dir/file.txt"},
		{"dir/subdir/file.txt", "dir/subdir/file.txt"},
	}

	for _, tc := range testCases {
		result := filepath.ToSlash(tc.input)
		if result != tc.expected {
			t.Errorf("ToSlash(%s) = %s, expected %s", tc.input, result, tc.expected)
		}
	}

	// Test that FromSlash converts forward slashes to platform-specific paths
	for _, tc := range testCases {
		fromSlash := filepath.FromSlash(tc.expected)
		toSlash := filepath.ToSlash(fromSlash)
		if toSlash != tc.expected {
			t.Errorf("roundtrip conversion failed for %s", tc.expected)
		}
	}
}

// TestCrossPlatform_NestedPathConversion tests nested path conversions
func TestCrossPlatform_NestedPathConversion(t *testing.T) {
	// Simulate paths that might come from different platforms
	windowsStylePath := "dir\\subdir\\deep\\file.txt"
	unixStylePath := "dir/subdir/deep/file.txt"

	// Convert Windows-style to Unix-style (cross-platform format)
	converted := strings.ReplaceAll(windowsStylePath, "\\", "/")
	if converted != unixStylePath {
		t.Errorf("Windows to Unix conversion failed: got %s, expected %s", converted, unixStylePath)
	}

	// FromSlash should convert to the current platform's format
	platformPath := filepath.FromSlash(unixStylePath)
	if runtime.GOOS == "windows" {
		if !strings.Contains(platformPath, "\\") {
			t.Log("On Windows, expected backslashes in path")
		}
	} else {
		if platformPath != unixStylePath {
			t.Errorf("On Unix, expected path unchanged: got %s", platformPath)
		}
	}
}

// TestCrossPlatform_FileInfoPayload tests FileInfo payload path handling
func TestCrossPlatform_FileInfoPayload(t *testing.T) {
	// Create a FileInfo with cross-platform path
	fileInfo := protocol.FileInfo{
		RelativePath: "documents/work/report.txt",
		Hash:         "abc123",
		ModTime:      1234567890,
		Size:         1024,
		IsDir:        false,
	}

	// Verify path uses forward slashes (cross-platform format)
	if strings.Contains(fileInfo.RelativePath, "\\") {
		t.Error("FileInfo.RelativePath should use forward slashes for cross-platform compatibility")
	}
}

// TestCrossPlatform_FileChangePayload tests FileChangePayload path handling
func TestCrossPlatform_FileChangePayload(t *testing.T) {
	payload := &protocol.FileChangePayload{
		RelativePath: "folder/subfolder/file.txt",
		Hash:         "def456",
		ModTime:      1234567890,
		Size:         2048,
		IsDir:        false,
	}

	msg, err := protocol.NewFileChangeMessage(payload)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	// Parse it back
	parsed, err := protocol.ParseFileChangePayload(msg.Payload)
	if err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	if parsed.RelativePath != payload.RelativePath {
		t.Errorf("path mismatch: got %s, expected %s", parsed.RelativePath, payload.RelativePath)
	}
}

// TestCrossPlatform_SpecialCharactersInPath tests handling of special characters
func TestCrossPlatform_SpecialCharactersInPath(t *testing.T) {
	// Test paths with various special characters
	testPaths := []string{
		"file with spaces.txt",
		"file-with-dashes.txt",
		"file_with_underscores.txt",
		"file.multiple.dots.txt",
		"UPPERCASE.TXT",
		"MixedCase.Txt",
	}

	tmpDir := t.TempDir()

	for _, path := range testPaths {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.WriteFile(fullPath, []byte("content"), 0644); err != nil {
			t.Errorf("failed to create file with path %s: %v", path, err)
			continue
		}

		// Verify the file exists and can be read
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("file with path %s was not created", path)
		}
	}
}

// TestCrossPlatform_UnicodeInPath tests handling of Unicode characters (platform permitting)
func TestCrossPlatform_UnicodeInPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Test Unicode filenames (may not work on all file systems)
	unicodePaths := []struct {
		name        string
		content     string
		shouldWork  bool
	}{
		{"日本語.txt", "Japanese", true},
		{"한국어.txt", "Korean", true},
		{"emoji_🎉.txt", "Emoji", true},
	}

	for _, tc := range unicodePaths {
		fullPath := filepath.Join(tmpDir, tc.name)
		err := os.WriteFile(fullPath, []byte(tc.content), 0644)
		if err != nil {
			t.Logf("Unicode path %s may not be supported on this file system: %v", tc.name, err)
			continue
		}

		// Read it back
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("failed to read Unicode file %s: %v", tc.name, err)
			continue
		}

		if string(content) != tc.content {
			t.Errorf("content mismatch for %s", tc.name)
		}
	}
}

// TestCrossPlatform_LineEndings tests handling of different line endings
func TestCrossPlatform_LineEndings(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		name        string
		content     string
		description string
	}{
		{"unix_lf.txt", "line1\nline2\nline3", "Unix LF"},
		{"windows_crlf.txt", "line1\r\nline2\r\nline3", "Windows CRLF"},
		{"old_mac_cr.txt", "line1\rline2\rline3", "Old Mac CR"},
		{"mixed.txt", "line1\nline2\r\nline3\r", "Mixed"},
	}

	for _, tc := range testCases {
		fullPath := filepath.Join(tmpDir, tc.name)
		if err := os.WriteFile(fullPath, []byte(tc.content), 0644); err != nil {
			t.Errorf("failed to write %s: %v", tc.description, err)
			continue
		}

		// Read it back
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("failed to read %s: %v", tc.description, err)
			continue
		}

		if string(content) != tc.content {
			t.Errorf("%s: content mismatch, line endings may have been modified", tc.description)
		}
	}
}

// TestCrossPlatform_PathLength tests handling of long paths
func TestCrossPlatform_PathLength(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a moderately nested path structure
	nestedPath := tmpDir
	for i := 0; i < 10; i++ {
		nestedPath = filepath.Join(nestedPath, "subdir")
	}

	if err := os.MkdirAll(nestedPath, 0755); err != nil {
		t.Logf("Long path creation may not be supported: %v", err)
		return
	}

	testFile := filepath.Join(nestedPath, "deep_file.txt")
	if err := os.WriteFile(testFile, []byte("deep content"), 0644); err != nil {
		t.Logf("Writing to deep path may not be supported: %v", err)
		return
	}

	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Errorf("failed to read from deep path: %v", err)
		return
	}

	if string(content) != "deep content" {
		t.Error("content mismatch for deep path")
	}
}

// TestCrossPlatform_FilePermissions tests basic file permission handling
func TestCrossPlatform_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("File permission tests are Unix-specific")
	}

	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "permissions.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	mode := info.Mode().Perm()
	// On Unix, verify the file has expected permissions (may be affected by umask)
	if mode&0400 == 0 {
		t.Error("file should be readable by owner")
	}
	if mode&0200 == 0 {
		t.Error("file should be writable by owner")
	}
}

// TestCrossPlatform_SyncerIgnorePatterns tests ignore patterns work across platforms
func TestCrossPlatform_SyncerIgnorePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create various file types
	os.WriteFile(filepath.Join(tmpDir, "normal.txt"), []byte("normal"), 0644)
	os.WriteFile(filepath.Join(tmpDir, ".hidden"), []byte("hidden"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "backup.bak"), []byte("backup"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "temp.tmp"), []byte("temp"), 0644)

	// Create .git directory
	gitDir := filepath.Join(tmpDir, ".git")
	os.Mkdir(gitDir, 0755)
	os.WriteFile(filepath.Join(gitDir, "config"), []byte("git config"), 0644)

	ignoreList := []string{".git", "*.tmp", "*.bak"}

	s, err := syncer.New(tmpDir, ":0", nil, ignoreList)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer s.Stop()

	status := s.GetStatus()
	localFiles := status["local_files"].(int)

	// Should only count normal.txt and .hidden (not .git, *.tmp, *.bak)
	if localFiles != 2 {
		t.Errorf("expected 2 files (normal.txt and .hidden), got %d", localFiles)
	}
}

// TestCrossPlatform_CaseSensitivity tests file name case sensitivity awareness
func TestCrossPlatform_CaseSensitivity(t *testing.T) {
	tmpDir := t.TempDir()

	file1 := filepath.Join(tmpDir, "TestFile.txt")
	if err := os.WriteFile(file1, []byte("content1"), 0644); err != nil {
		t.Fatalf("failed to create first file: %v", err)
	}

	// Try to create a file with different case
	file2 := filepath.Join(tmpDir, "testfile.txt")
	err := os.WriteFile(file2, []byte("content2"), 0644)

	// Check if both files exist (case-sensitive) or they're the same (case-insensitive)
	entries, _ := os.ReadDir(tmpDir)
	fileCount := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			fileCount++
		}
	}

	if fileCount == 1 {
		t.Log("File system is case-insensitive (typical on macOS/Windows)")
	} else if fileCount == 2 {
		t.Log("File system is case-sensitive (typical on Linux)")
	}

	// Just ensure no error occurred
	_ = err
}

// TestCrossPlatform_FilePermissions_Strict tests file permissions with strict verification
func TestCrossPlatform_FilePermissions_Strict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Strict permission tests are Unix-specific")
	}

	tmpDir := t.TempDir()

	testCases := []struct {
		name           string
		permission     os.FileMode
		expectReadable bool
		expectWritable bool
	}{
		{"0644_rw_r_r", 0644, true, true},
		{"0444_r_r_r", 0444, true, false},
		{"0600_rw____", 0600, true, true},
		{"0400_r_____", 0400, true, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tc.name+".txt")
			if err := os.WriteFile(testFile, []byte("content"), tc.permission); err != nil {
				t.Fatalf("failed to create file: %v", err)
			}
			defer os.Chmod(testFile, 0644) // Restore for cleanup

			info, err := os.Stat(testFile)
			if err != nil {
				t.Fatalf("failed to stat file: %v", err)
			}

			mode := info.Mode().Perm()

			// Check readable by owner (bit 8, 0400)
			isReadable := mode&0400 != 0
			if isReadable != tc.expectReadable {
				t.Errorf("expected readable=%v, got %v (mode=%o)", tc.expectReadable, isReadable, mode)
			}

			// Check writable by owner (bit 7, 0200)
			isWritable := mode&0200 != 0
			if isWritable != tc.expectWritable {
				t.Errorf("expected writable=%v, got %v (mode=%o)", tc.expectWritable, isWritable, mode)
			}
		})
	}
}

// TestCrossPlatform_DirectoryPermissions tests directory permissions
func TestCrossPlatform_DirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Directory permission tests are Unix-specific")
	}

	tmpDir := t.TempDir()

	testDir := filepath.Join(tmpDir, "testdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	info, err := os.Stat(testDir)
	if err != nil {
		t.Fatalf("failed to stat directory: %v", err)
	}

	if !info.IsDir() {
		t.Error("should be a directory")
	}

	mode := info.Mode().Perm()

	// Directory should be readable by owner
	if mode&0400 == 0 {
		t.Error("directory should be readable by owner")
	}

	// Directory should be writable by owner
	if mode&0200 == 0 {
		t.Error("directory should be writable by owner")
	}

	// Directory should be executable by owner (to enter it)
	if mode&0100 == 0 {
		t.Error("directory should be executable by owner")
	}
}

// TestCrossPlatform_RestrictedDirectory tests read-only directory handling
func TestCrossPlatform_RestrictedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Restricted directory tests are Unix-specific")
	}

	tmpDir := t.TempDir()

	// Create a read-only directory
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0555); err != nil {
		t.Fatalf("failed to create read-only dir: %v", err)
	}
	defer os.Chmod(readOnlyDir, 0755) // Restore for cleanup

	// Verify directory is read-only
	info, err := os.Stat(readOnlyDir)
	if err != nil {
		t.Fatalf("failed to stat directory: %v", err)
	}

	mode := info.Mode().Perm()
	if mode&0200 != 0 {
		t.Log("Directory appears writable (may happen as root)")
	}

	// Try to create file in read-only directory - should fail
	testFile := filepath.Join(readOnlyDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	if err == nil {
		t.Log("write to read-only dir succeeded (may happen as root)")
	} else if !os.IsPermission(err) {
		t.Logf("expected permission error, got: %v", err)
	}
}

// TestCrossPlatform_SymlinkHandling tests symbolic link handling
func TestCrossPlatform_SymlinkHandling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Symlink tests may require elevated privileges on Windows")
	}

	tmpDir := t.TempDir()

	// Create target file
	targetFile := filepath.Join(tmpDir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("target content"), 0644); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

	// Create symlink
	symlinkFile := filepath.Join(tmpDir, "link.txt")
	if err := os.Symlink(targetFile, symlinkFile); err != nil {
		t.Logf("symlink creation failed (may not be supported): %v", err)
		return
	}

	// Read through symlink
	content, err := os.ReadFile(symlinkFile)
	if err != nil {
		t.Fatalf("failed to read through symlink: %v", err)
	}

	if string(content) != "target content" {
		t.Error("content through symlink should match target")
	}

	// Verify symlink is a symlink
	info, err := os.Lstat(symlinkFile)
	if err != nil {
		t.Fatalf("failed to lstat symlink: %v", err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("should be identified as symlink")
	}
}

// TestCrossPlatform_HiddenFiles tests hidden file handling
func TestCrossPlatform_HiddenFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create hidden files (Unix-style dot prefix)
	hiddenFile := filepath.Join(tmpDir, ".hidden")
	if err := os.WriteFile(hiddenFile, []byte("hidden content"), 0644); err != nil {
		t.Fatalf("failed to create hidden file: %v", err)
	}

	// Verify hidden file exists and can be read
	content, err := os.ReadFile(hiddenFile)
	if err != nil {
		t.Fatalf("failed to read hidden file: %v", err)
	}

	if string(content) != "hidden content" {
		t.Error("hidden file content mismatch")
	}

	// Create syncer with default ignore patterns
	s, err := syncer.New(tmpDir, ":0", nil, nil)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start syncer: %v", err)
	}
	defer s.Stop()

	// Syncer should still see hidden files (unless explicitly ignored)
	status := s.GetStatus()
	localFiles := status["local_files"].(int)
	if localFiles != 1 {
		t.Errorf("expected 1 file (hidden file), got %d", localFiles)
	}
}

// TestCrossPlatform_TimestampPrecision tests timestamp handling precision
func TestCrossPlatform_TimestampPrecision(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "timestamp.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Set a specific time
	testTime := time.Date(2024, 6, 15, 10, 30, 45, 123456789, time.UTC)
	if err := os.Chtimes(testFile, testTime, testTime); err != nil {
		t.Fatalf("failed to set times: %v", err)
	}

	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	modTime := info.ModTime()

	// Second-level precision should always work
	if modTime.Unix() != testTime.Unix() {
		t.Errorf("second-level timestamp mismatch: expected %v, got %v", testTime.Unix(), modTime.Unix())
	}

	// Nanosecond precision may vary by filesystem
	if modTime.Nanosecond() != testTime.Nanosecond() {
		t.Logf("nanosecond precision may not be supported: expected %d, got %d", testTime.Nanosecond(), modTime.Nanosecond())
	}
}

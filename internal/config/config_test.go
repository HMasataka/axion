package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ListenAddr != ":8765" {
		t.Errorf("expected ListenAddr :8765, got %s", cfg.ListenAddr)
	}

	if len(cfg.PeerAddrs) != 0 {
		t.Errorf("expected empty PeerAddrs, got %v", cfg.PeerAddrs)
	}

	expectedIgnoreList := []string{".git", ".DS_Store", "Thumbs.db", "*.tmp", "*.swp", "*~"}
	if len(cfg.IgnoreList) != len(expectedIgnoreList) {
		t.Errorf("expected IgnoreList length %d, got %d", len(expectedIgnoreList), len(cfg.IgnoreList))
	}

	homeDir, _ := os.UserHomeDir()
	expectedSyncPath := filepath.Join(homeDir, "Sync")
	if cfg.SyncPath != expectedSyncPath {
		t.Errorf("expected SyncPath %s, got %s", expectedSyncPath, cfg.SyncPath)
	}
}

func TestConfigLoad(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	content := `{
		"sync_path": "/test/path",
		"listen_addr": ":9999",
		"peer_addrs": ["192.168.1.1:8765"],
		"ignore_list": [".git"]
	}`

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.SyncPath != "/test/path" {
		t.Errorf("expected SyncPath /test/path, got %s", cfg.SyncPath)
	}

	if cfg.ListenAddr != ":9999" {
		t.Errorf("expected ListenAddr :9999, got %s", cfg.ListenAddr)
	}

	if len(cfg.PeerAddrs) != 1 || cfg.PeerAddrs[0] != "192.168.1.1:8765" {
		t.Errorf("expected PeerAddrs [192.168.1.1:8765], got %v", cfg.PeerAddrs)
	}

	if len(cfg.IgnoreList) != 1 || cfg.IgnoreList[0] != ".git" {
		t.Errorf("expected IgnoreList [.git], got %v", cfg.IgnoreList)
	}
}

func TestConfigLoad_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	content := `{invalid json}`

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestConfigLoad_NotExist(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}

	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got %v", err)
	}
}

func TestConfigSave(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := &Config{
		SyncPath:   "/test/save/path",
		ListenAddr: ":7777",
		PeerAddrs:  []string{"10.0.0.1:8765"},
		IgnoreList: []string{".git", ".DS_Store"},
	}

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}

	if loaded.SyncPath != cfg.SyncPath {
		t.Errorf("SyncPath mismatch: expected %s, got %s", cfg.SyncPath, loaded.SyncPath)
	}

	if loaded.ListenAddr != cfg.ListenAddr {
		t.Errorf("ListenAddr mismatch: expected %s, got %s", cfg.ListenAddr, loaded.ListenAddr)
	}
}

func TestConfigSave_CreateDir(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "subdir", "nested", "config.json")

	cfg := DefaultConfig()

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("failed to save config with nested dir: %v", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file was not created")
	}
}

func TestGetConfigPath(t *testing.T) {
	path := GetConfigPath()

	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".config", "filesync", "config.json")

	if path != expected {
		t.Errorf("expected config path %s, got %s", expected, path)
	}
}

func TestLoadOrCreate_Create(t *testing.T) {
	tmpDir := t.TempDir()

	configDir := filepath.Join(tmpDir, ".config", "filesync")
	configPath := filepath.Join(configDir, "config.json")

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("config file should not exist yet")
	}

	originalGetConfigPath := GetConfigPath
	defer func() { _ = originalGetConfigPath }()

	cfg, err := loadOrCreateWithPath(configPath)
	if err != nil {
		t.Fatalf("LoadOrCreate failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("config should not be nil")
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file was not created")
	}

	if cfg.ListenAddr != ":8765" {
		t.Errorf("expected default ListenAddr :8765, got %s", cfg.ListenAddr)
	}
}

func TestLoadOrCreate_Load(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".config", "filesync", "config.json")

	initialCfg := &Config{
		SyncPath:   "/existing/path",
		ListenAddr: ":5555",
		PeerAddrs:  []string{"10.0.0.1:8765"},
		IgnoreList: []string{".git"},
	}

	if err := initialCfg.Save(configPath); err != nil {
		t.Fatalf("failed to save initial config: %v", err)
	}

	cfg, err := loadOrCreateWithPath(configPath)
	if err != nil {
		t.Fatalf("LoadOrCreate failed: %v", err)
	}

	if cfg.SyncPath != "/existing/path" {
		t.Errorf("expected SyncPath /existing/path, got %s", cfg.SyncPath)
	}

	if cfg.ListenAddr != ":5555" {
		t.Errorf("expected ListenAddr :5555, got %s", cfg.ListenAddr)
	}

	if len(cfg.PeerAddrs) != 1 || cfg.PeerAddrs[0] != "10.0.0.1:8765" {
		t.Errorf("expected PeerAddrs [10.0.0.1:8765], got %v", cfg.PeerAddrs)
	}
}

func loadOrCreateWithPath(configPath string) (*Config, error) {
	config, err := Load(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			config = DefaultConfig()
			if err := config.Save(configPath); err != nil {
				return nil, err
			}
			return config, nil
		}
		return nil, err
	}
	return config, nil
}

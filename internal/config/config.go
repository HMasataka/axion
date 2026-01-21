package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	SyncPath   string   `json:"sync_path"`
	ListenAddr string   `json:"listen_addr"`
	PeerAddrs  []string `json:"peer_addrs"`
	IgnoreList []string `json:"ignore_list"`
}

func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	return &Config{
		SyncPath:   filepath.Join(homeDir, "Sync"),
		ListenAddr: ":8765",
		PeerAddrs:  []string{},
		IgnoreList: []string{
			".git",
			".DS_Store",
			"Thumbs.db",
			"*.tmp",
			"*.swp",
			"*~",
		},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	return config, nil
}

func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func GetConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "filesync", "config.json")
}

func LoadOrCreate() (*Config, error) {
	configPath := GetConfigPath()

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

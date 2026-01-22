package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/HMasataka/axion/internal/config"
	"github.com/HMasataka/axion/internal/syncer"
)

func main() {
	var (
		syncPath   = flag.String("path", "", "Path to sync folder")
		listenAddr = flag.String("listen", "", "Address to listen on (e.g., :8765)")
		peerAddrs  = flag.String("peers", "", "Comma-separated list of peer addresses (e.g., 192.168.1.10:8765)")
		configPath = flag.String("config", "", "Path to config file")
		initConfig = flag.Bool("init", false, "Initialize default config file")
		showStatus = flag.Bool("status", false, "Show current status and exit")
	)

	flag.Parse()

	if *initConfig {
		path := config.GetConfigPath()
		if *configPath != "" {
			path = *configPath
		}

		cfg := config.DefaultConfig()
		if err := cfg.Save(path); err != nil {
			slog.Error("Failed to create config", "error", err)
			os.Exit(1)
		}
		fmt.Printf("Config file created at: %s\n", path)
		fmt.Println("Please edit the config file and set your peer addresses.")
		return
	}

	var cfg *config.Config
	var err error

	if *configPath != "" {
		cfg, err = config.Load(*configPath)
		if err != nil {
			slog.Error("Failed to load config", "error", err)
			os.Exit(1)
		}
	} else {
		cfg, err = config.LoadOrCreate()
		if err != nil {
			slog.Error("Failed to load config", "error", err)
			os.Exit(1)
		}
	}

	if *syncPath != "" {
		cfg.SyncPath = *syncPath
	}
	if *listenAddr != "" {
		cfg.ListenAddr = *listenAddr
	}
	if *peerAddrs != "" {
		cfg.PeerAddrs = strings.Split(*peerAddrs, ",")
	}

	if cfg.SyncPath == "" {
		slog.Error("Sync path is required. Use -path flag or set in config file.")
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.SyncPath, 0755); err != nil {
		slog.Error("Failed to create sync directory", "error", err)
		os.Exit(1)
	}

	fmt.Println("=== File Sync ===")
	fmt.Printf("Sync Path: %s\n", cfg.SyncPath)
	fmt.Printf("Listen Address: %s\n", cfg.ListenAddr)
	fmt.Printf("Peer Addresses: %v\n", cfg.PeerAddrs)
	fmt.Printf("Ignore Patterns: %v\n", cfg.IgnoreList)
	fmt.Println()

	s, err := syncer.New(cfg.SyncPath, cfg.ListenAddr, cfg.PeerAddrs, cfg.IgnoreList)
	if err != nil {
		slog.Error("Failed to create syncer", "error", err)
		os.Exit(1)
	}

	if *showStatus {
		fmt.Println(s.GetStatusJSON())
		return
	}

	if err := s.Start(); err != nil {
		slog.Error("Failed to start syncer", "error", err)
		os.Exit(1)
	}

	fmt.Println("Sync started. Press Ctrl+C to stop.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")
	s.Stop()
	fmt.Println("Goodbye!")
}

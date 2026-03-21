package main

import (
	"flag"
	"os"
	"strings"
	"time"
)

type Config struct {
	ListenAddr   string
	RootDirs     []string
	SocketPath   string
	ScanInterval time.Duration
	ScanEnabled  bool
}

func parseConfig() Config {
	cfg := Config{
		ListenAddr:   ":80",
		SocketPath:   "/var/run/localproxy.sock",
		ScanInterval: 3 * time.Second,
		ScanEnabled:  true,
	}

	flag.StringVar(&cfg.ListenAddr, "listen", cfg.ListenAddr, "address to listen on")
	roots := flag.String("roots", "", "comma-separated list of root directories to scan")
	flag.StringVar(&cfg.SocketPath, "socket", cfg.SocketPath, "path to Unix socket for backchannel")
	flag.DurationVar(&cfg.ScanInterval, "scan-interval", cfg.ScanInterval, "interval between scans")
	noScan := flag.Bool("no-scan", false, "disable lsof/docker scanning")
	flag.Parse()

	if *noScan {
		cfg.ScanEnabled = false
	}

	if *roots != "" {
		cfg.RootDirs = strings.Split(*roots, ",")
	}

	// Env var fallbacks
	if len(cfg.RootDirs) == 0 {
		if env := os.Getenv("LOCALPROXY_ROOTS"); env != "" {
			cfg.RootDirs = strings.Split(env, ",")
		}
	}
	if env := os.Getenv("LOCALPROXY_LISTEN"); env != "" {
		cfg.ListenAddr = env
	}
	if env := os.Getenv("LOCALPROXY_SOCKET"); env != "" {
		cfg.SocketPath = env
	}

	// Default root: ~/Code
	if len(cfg.RootDirs) == 0 {
		home, err := os.UserHomeDir()
		if err == nil {
			cfg.RootDirs = []string{home + "/Code"}
		}
	}

	return cfg
}

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "register":
			runRegister()
			return
		case "unregister":
			runUnregister()
			return
		case "list":
			runList()
			return
		case "install":
			runInstall()
			return
		case "uninstall":
			runUninstall()
			return
		}
	}

	runDaemon()
}

func runDaemon() {
	cfg := parseConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	registry := NewRegistry()

	// Start scanners
	if cfg.ScanEnabled {
		scanner := NewScanner(registry, cfg.RootDirs, cfg.ScanInterval, logger)
		go scanner.Run(ctx)

		docker := NewDockerScanner(registry, cfg.RootDirs, cfg.ScanInterval, logger)
		go docker.Run(ctx)
	}

	// Start backchannel
	bc := NewBackchannel(registry, cfg.SocketPath, logger)
	go bc.Run(ctx)

	// Start HTTP server
	proxy := NewProxyHandler(registry, logger)
	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: proxy,
	}

	go func() {
		<-ctx.Done()
		logger.Info("shutting down")
		server.Close()
	}()

	logger.Info("starting", "addr", cfg.ListenAddr, "roots", cfg.RootDirs, "socket", cfg.SocketPath)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

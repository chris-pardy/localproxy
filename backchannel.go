package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
)

type Backchannel struct {
	registry   *Registry
	socketPath string
	logger     *slog.Logger
}

func NewBackchannel(registry *Registry, socketPath string, logger *slog.Logger) *Backchannel {
	return &Backchannel{
		registry:   registry,
		socketPath: socketPath,
		logger:     logger,
	}
}

func (b *Backchannel) Run(ctx context.Context) {
	// Remove stale socket
	os.Remove(b.socketPath)

	ln, err := net.Listen("unix", b.socketPath)
	if err != nil {
		b.logger.Error("backchannel listen failed", "path", b.socketPath, "err", err)
		return
	}
	defer ln.Close()

	// Allow any user to connect
	os.Chmod(b.socketPath, 0666)

	b.logger.Info("backchannel listening", "path", b.socketPath)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.logger.Error("accept error", "err", err)
			continue
		}
		go b.handleConn(conn)
	}
}

type request struct {
	Action string `json:"action"`
	Name   string `json:"name,omitempty"`
	Port   int    `json:"port,omitempty"`
}

type response struct {
	OK      bool             `json:"ok"`
	Error   string           `json:"error,omitempty"`
	Entries []responseEntry  `json:"entries,omitempty"`
}

type responseEntry struct {
	Name   string `json:"name"`
	Port   int    `json:"port"`
	Source string `json:"source"`
	Dir    string `json:"dir,omitempty"`
	PID    int    `json:"pid,omitempty"`
}

func (b *Backchannel) handleConn(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}

	var req request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		b.writeResponse(conn, response{OK: false, Error: "invalid JSON"})
		return
	}

	switch req.Action {
	case "register":
		if req.Name == "" {
			b.writeResponse(conn, response{OK: false, Error: "name is required"})
			return
		}
		if req.Port < 1 || req.Port > 65535 {
			b.writeResponse(conn, response{OK: false, Error: "port must be 1-65535"})
			return
		}
		b.registry.Register(req.Name, req.Port, SourceBackchannel, 0, "")
		b.logger.Info("registered via backchannel", "name", req.Name, "port", req.Port)
		b.writeResponse(conn, response{OK: true})

	case "unregister":
		if req.Name == "" {
			b.writeResponse(conn, response{OK: false, Error: "name is required"})
			return
		}
		b.registry.Unregister(req.Name, SourceBackchannel)
		b.logger.Info("unregistered via backchannel", "name", req.Name)
		b.writeResponse(conn, response{OK: true})

	case "list":
		all := b.registry.All()
		entries := make([]responseEntry, len(all))
		for i, reg := range all {
			entries[i] = responseEntry{
				Name:   reg.Name,
				Port:   reg.Port,
				Source: reg.Source.String(),
				Dir:    reg.Dir,
				PID:    reg.PID,
			}
		}
		b.writeResponse(conn, response{OK: true, Entries: entries})

	default:
		b.writeResponse(conn, response{OK: false, Error: "unknown action"})
	}
}

func (b *Backchannel) writeResponse(conn net.Conn, resp response) {
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	conn.Write(data)
}

// CLI subcommands — these connect to the socket as a client

func runRegister() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "usage: localproxy register <name> <port>\n")
		os.Exit(1)
	}
	name := os.Args[2]
	port, err := strconv.Atoi(os.Args[3])
	if err != nil || port < 1 || port > 65535 {
		fmt.Fprintf(os.Stderr, "invalid port: %s\n", os.Args[3])
		os.Exit(1)
	}

	resp := socketRequest(request{Action: "register", Name: name, Port: port})
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "error: %s\n", resp.Error)
		os.Exit(1)
	}
	fmt.Printf("registered %s -> port %d\n", name, port)
}

func runUnregister() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: localproxy unregister <name>\n")
		os.Exit(1)
	}
	name := os.Args[2]

	resp := socketRequest(request{Action: "unregister", Name: name})
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "error: %s\n", resp.Error)
		os.Exit(1)
	}
	fmt.Printf("unregistered %s\n", name)
}

func runList() {
	resp := socketRequest(request{Action: "list"})
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "error: %s\n", resp.Error)
		os.Exit(1)
	}
	if len(resp.Entries) == 0 {
		fmt.Println("no registered projects")
		return
	}
	for _, e := range resp.Entries {
		fmt.Printf("%-20s port %-6d source: %-12s %s\n", e.Name, e.Port, e.Source, e.Dir)
	}
}

func socketRequest(req request) response {
	socketPath := os.Getenv("LOCALPROXY_SOCKET")
	if socketPath == "" {
		socketPath = "/var/run/localproxy.sock"
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot connect to localproxy daemon: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	data, _ := json.Marshal(req)
	data = append(data, '\n')
	conn.Write(data)

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		fmt.Fprintf(os.Stderr, "no response from daemon\n")
		os.Exit(1)
	}

	var resp response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		fmt.Fprintf(os.Stderr, "invalid response: %v\n", err)
		os.Exit(1)
	}
	return resp
}

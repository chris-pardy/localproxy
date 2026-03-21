package main

import (
	"net"
	"net/http"
	"testing"
)

func TestParseLsofListeners(t *testing.T) {
	output := `p1234
cnode
n*:3000
n127.0.0.1:3001
p5678
cvite
n*:5173
p9999
cmDNSResponder
n*:5353
`

	result := parseLsofListeners(output)

	if len(result) != 2 {
		t.Fatalf("expected 2 PIDs, got %d", len(result))
	}

	// PID 1234 should have ports 3000, 3001
	ports := result[1234]
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports for PID 1234, got %d", len(ports))
	}
	if ports[0] != 3000 || ports[1] != 3001 {
		t.Fatalf("expected ports [3000 3001], got %v", ports)
	}

	// PID 5678 should have port 5173
	ports = result[5678]
	if len(ports) != 1 || ports[0] != 5173 {
		t.Fatalf("expected [5173], got %v", ports)
	}

	// PID 9999 (mDNSResponder) should be excluded
	if _, ok := result[9999]; ok {
		t.Fatal("mDNSResponder should be excluded")
	}
}

func TestParseLsofListenersIPv6(t *testing.T) {
	output := `p100
cnode
n[::1]:8080
`
	result := parseLsofListeners(output)
	ports := result[100]
	if len(ports) != 1 || ports[0] != 8080 {
		t.Fatalf("expected [8080], got %v", ports)
	}
}

func TestParseLsofListenersEmpty(t *testing.T) {
	result := parseLsofListeners("")
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %v", result)
	}
}

func TestParsePort(t *testing.T) {
	tests := []struct {
		addr string
		want int
	}{
		{"*:3000", 3000},
		{"127.0.0.1:8080", 8080},
		{"[::1]:5173", 5173},
		{"localhost:0", 0},
		{"noport", 0},
	}
	for _, tt := range tests {
		got := parsePort(tt.addr)
		if got != tt.want {
			t.Errorf("parsePort(%q) = %d, want %d", tt.addr, got, tt.want)
		}
	}
}

func TestParseLsofCWDs(t *testing.T) {
	output := `p1234
n/Users/chris/Code/my-app
p5678
n/Users/chris/Code/other-app/src
`
	result := parseLsofCWDs(output)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if result[1234] != "/Users/chris/Code/my-app" {
		t.Fatalf("unexpected cwd for 1234: %s", result[1234])
	}
	if result[5678] != "/Users/chris/Code/other-app/src" {
		t.Fatalf("unexpected cwd for 5678: %s", result[5678])
	}
}

func TestMatchProject(t *testing.T) {
	s := &Scanner{
		rootDirs: []string{"/Users/chris/Code"},
	}

	tests := []struct {
		cwd      string
		wantName string
		wantDir  string
	}{
		{"/Users/chris/Code/my-app", "my-app", "/Users/chris/Code/my-app"},
		{"/Users/chris/Code/my-app/src", "src.my-app", "/Users/chris/Code/my-app/src"},
		{"/Users/chris/Code/My-App/packages/web", "web.packages.my-app", "/Users/chris/Code/My-App/packages/web"},
		{"/Users/chris/Code/shift-posts/shiftposts-ui-usa", "shiftposts-ui-usa.shift-posts", "/Users/chris/Code/shift-posts/shiftposts-ui-usa"},
		{"/Users/chris/Code/shift-posts/shiftposts-ui-usa/src", "src.shiftposts-ui-usa.shift-posts", "/Users/chris/Code/shift-posts/shiftposts-ui-usa/src"},
		{"/other/path", "", ""},
		{"/Users/chris/Code", "", ""}, // must be under root, not at root
	}

	for _, tt := range tests {
		name, dir := s.matchProject(tt.cwd)
		if name != tt.wantName {
			t.Errorf("matchProject(%q) name = %q, want %q", tt.cwd, name, tt.wantName)
		}
		if dir != tt.wantDir {
			t.Errorf("matchProject(%q) dir = %q, want %q", tt.cwd, dir, tt.wantDir)
		}
	}
}

func TestPickPort(t *testing.T) {
	s := &Scanner{}

	// Single port: skip probing, return it directly
	port := s.pickPort("/nonexistent", []int{3000})
	if port != 3000 {
		t.Fatalf("expected 3000, got %d", port)
	}

	// Multiple ports, none HTTP-responsive: falls back to lowest non-system
	// Use high ports unlikely to be in use
	port = s.pickPort("/nonexistent", []int{59001, 58001, 59501})
	if port != 58001 {
		t.Fatalf("expected 58001 (lowest non-system), got %d", port)
	}

	// With only system ports, should fallback to first
	port = s.pickPort("/nonexistent", []int{80, 443})
	if port != 80 {
		t.Fatalf("expected 80 (fallback to first), got %d", port)
	}

	// HTTP-responsive port should win over lower non-responsive port
	// Start a real HTTP server to test probing
	srv := &http.Server{Addr: "127.0.0.1:19877", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:19877")
	if err != nil {
		t.Skipf("cannot bind test port: %v", err)
	}
	go srv.Serve(ln)
	defer srv.Close()

	port = s.pickPort("/nonexistent", []int{19877, 3000, 8080})
	if port != 19877 {
		t.Fatalf("expected 19877 (HTTP-responsive), got %d", port)
	}
}

func TestAppendUnique(t *testing.T) {
	s := appendUnique([]int{1, 2, 3}, 2)
	if len(s) != 3 {
		t.Fatalf("should not duplicate: %v", s)
	}
	s = appendUnique(s, 4)
	if len(s) != 4 || s[3] != 4 {
		t.Fatalf("should append new: %v", s)
	}
}

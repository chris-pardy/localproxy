package main

import (
	"sync"
	"testing"
	"time"
)

func TestRegisterAndResolve(t *testing.T) {
	r := NewRegistry()
	r.Register("app", 3000, SourceScanner, 100, "/code/app")

	reg, ok := r.Resolve("app")
	if !ok {
		t.Fatal("expected to resolve app")
	}
	if reg.Port != 3000 {
		t.Fatalf("expected port 3000, got %d", reg.Port)
	}
}

func TestResolveNotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Resolve("missing")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestPriorityResolution(t *testing.T) {
	r := NewRegistry()
	r.Register("app", 3000, SourceScanner, 100, "/code/app")
	r.Register("app", 4000, SourceDotfile, 0, "/code/app")
	r.Register("app", 5000, SourceBackchannel, 0, "/code/app")

	reg, ok := r.Resolve("app")
	if !ok {
		t.Fatal("expected to resolve app")
	}
	if reg.Port != 5000 {
		t.Fatalf("backchannel should win, got port %d", reg.Port)
	}
	if reg.Source != SourceBackchannel {
		t.Fatalf("expected source backchannel, got %s", reg.Source)
	}
}

func TestDockerPriority(t *testing.T) {
	r := NewRegistry()
	r.Register("app", 3000, SourceScanner, 100, "/code/app")
	r.Register("app", 4000, SourceDocker, 0, "/code/app")

	reg, _ := r.Resolve("app")
	if reg.Port != 4000 {
		t.Fatalf("docker should beat scanner, got port %d", reg.Port)
	}
}

func TestUpsertSameSource(t *testing.T) {
	r := NewRegistry()
	r.Register("app", 3000, SourceScanner, 100, "/code/app")
	r.Register("app", 4000, SourceScanner, 200, "/code/app")

	reg, _ := r.Resolve("app")
	if reg.Port != 4000 {
		t.Fatalf("expected updated port 4000, got %d", reg.Port)
	}
	if reg.PID != 200 {
		t.Fatalf("expected updated PID 200, got %d", reg.PID)
	}
}

func TestUnregister(t *testing.T) {
	r := NewRegistry()
	r.Register("app", 3000, SourceScanner, 100, "/code/app")
	r.Register("app", 5000, SourceBackchannel, 0, "/code/app")

	r.Unregister("app", SourceBackchannel)

	reg, ok := r.Resolve("app")
	if !ok {
		t.Fatal("scanner registration should remain")
	}
	if reg.Port != 3000 {
		t.Fatalf("expected scanner port 3000, got %d", reg.Port)
	}
}

func TestUnregisterLast(t *testing.T) {
	r := NewRegistry()
	r.Register("app", 3000, SourceScanner, 100, "/code/app")
	r.Unregister("app", SourceScanner)

	_, ok := r.Resolve("app")
	if ok {
		t.Fatal("expected not found after unregistering last source")
	}
}

func TestUnregisterNonexistent(t *testing.T) {
	r := NewRegistry()
	r.Unregister("missing", SourceScanner) // should not panic
}

func TestPurgeStale(t *testing.T) {
	r := NewRegistry()
	r.Register("old", 3000, SourceScanner, 100, "/code/old")
	time.Sleep(10 * time.Millisecond)
	cutoff := time.Now()
	r.Register("new", 4000, SourceScanner, 200, "/code/new")

	r.PurgeStale(SourceScanner, cutoff)

	_, ok := r.Resolve("old")
	if ok {
		t.Fatal("old should have been purged")
	}
	_, ok = r.Resolve("new")
	if !ok {
		t.Fatal("new should remain")
	}
}

func TestPurgeStaleLeavesOtherSources(t *testing.T) {
	r := NewRegistry()
	r.Register("app", 3000, SourceScanner, 100, "/code/app")
	r.Register("app", 5000, SourceBackchannel, 0, "/code/app")
	time.Sleep(10 * time.Millisecond)
	cutoff := time.Now()

	r.PurgeStale(SourceScanner, cutoff)

	reg, ok := r.Resolve("app")
	if !ok {
		t.Fatal("backchannel should remain")
	}
	if reg.Port != 5000 {
		t.Fatalf("expected backchannel port 5000, got %d", reg.Port)
	}
}

func TestAll(t *testing.T) {
	r := NewRegistry()
	r.Register("a", 3000, SourceScanner, 100, "/code/a")
	r.Register("b", 4000, SourceDocker, 0, "/code/b")
	r.Register("a", 5000, SourceBackchannel, 0, "/code/a")

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}

	byName := make(map[string]Registration)
	for _, reg := range all {
		byName[reg.Name] = reg
	}

	if byName["a"].Port != 5000 {
		t.Fatalf("expected a to resolve to backchannel port 5000, got %d", byName["a"].Port)
	}
	if byName["b"].Port != 4000 {
		t.Fatalf("expected b port 4000, got %d", byName["b"].Port)
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Register("app", 3000+i, SourceScanner, i, "/code/app")
			r.Resolve("app")
			r.All()
		}(i)
	}
	wg.Wait()

	_, ok := r.Resolve("app")
	if !ok {
		t.Fatal("expected to resolve app after concurrent writes")
	}
}

func TestSourceString(t *testing.T) {
	tests := []struct {
		s    Source
		want string
	}{
		{SourceBackchannel, "backchannel"},
		{SourceDotfile, "dotfile"},
		{SourceDocker, "docker"},
		{SourceScanner, "scanner"},
		{Source(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Source(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotLocalhost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".localhost")

	content := `# This is a comment
name = my-cool-app
port = 3000
`
	os.WriteFile(path, []byte(content), 0644)

	dl, err := ParseDotLocalhost(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dl.Name != "my-cool-app" {
		t.Errorf("expected name my-cool-app, got %s", dl.Name)
	}
	if dl.Port != 3000 {
		t.Errorf("expected port 3000, got %d", dl.Port)
	}
}

func TestParseDotLocalhostNameOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".localhost")

	os.WriteFile(path, []byte("name = webapp\n"), 0644)

	dl, err := ParseDotLocalhost(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dl.Name != "webapp" {
		t.Errorf("expected name webapp, got %s", dl.Name)
	}
	if dl.Port != 0 {
		t.Errorf("expected port 0, got %d", dl.Port)
	}
}

func TestParseDotLocalhostEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".localhost")

	os.WriteFile(path, []byte(""), 0644)

	dl, err := ParseDotLocalhost(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dl.Name != "" || dl.Port != 0 {
		t.Errorf("expected zero values, got %+v", dl)
	}
}

func TestParseDotLocalhostCommentsOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".localhost")

	os.WriteFile(path, []byte("# just a comment\n# another\n"), 0644)

	dl, err := ParseDotLocalhost(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dl.Name != "" || dl.Port != 0 {
		t.Errorf("expected zero values, got %+v", dl)
	}
}

func TestParseDotLocalhostInvalidPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".localhost")

	os.WriteFile(path, []byte("port = notanumber\n"), 0644)

	dl, err := ParseDotLocalhost(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dl.Port != 0 {
		t.Errorf("expected port 0 for invalid, got %d", dl.Port)
	}
}

func TestParseDotLocalhostPortOutOfRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".localhost")

	os.WriteFile(path, []byte("port = 99999\n"), 0644)

	dl, err := ParseDotLocalhost(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dl.Port != 0 {
		t.Errorf("expected port 0 for out of range, got %d", dl.Port)
	}
}

func TestParseDotLocalhostUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".localhost")

	os.WriteFile(path, []byte("name = app\nunknown = value\nport = 8080\n"), 0644)

	dl, err := ParseDotLocalhost(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dl.Name != "app" || dl.Port != 8080 {
		t.Errorf("expected app:8080, got %s:%d", dl.Name, dl.Port)
	}
}

func TestParseDotLocalhostSpacing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".localhost")

	os.WriteFile(path, []byte("  name   =   spaced-app  \n  port=4000\n"), 0644)

	dl, err := ParseDotLocalhost(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dl.Name != "spaced-app" {
		t.Errorf("expected name spaced-app, got %q", dl.Name)
	}
	if dl.Port != 4000 {
		t.Errorf("expected port 4000, got %d", dl.Port)
	}
}

func TestParseDotLocalhostMissing(t *testing.T) {
	_, err := ParseDotLocalhost("/nonexistent/.localhost")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

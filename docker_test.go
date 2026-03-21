package main

import (
	"log/slog"
	"os"
	"testing"
)

func TestExtractPorts(t *testing.T) {
	d := &DockerScanner{logger: slog.New(slog.NewTextHandler(os.Stderr, nil))}

	tests := []struct {
		input string
		want  []int
	}{
		{"0.0.0.0:3000->3000/tcp", []int{3000}},
		{"0.0.0.0:3000->3000/tcp, 0.0.0.0:3001->3001/tcp", []int{3000, 3001}},
		{":::8080->8080/tcp", []int{8080}},
		{"3000/tcp", nil},         // not published
		{"", nil},
		{"0.0.0.0:80->80/tcp, 0.0.0.0:443->443/tcp", []int{80, 443}},
	}

	for _, tt := range tests {
		got := d.extractPorts(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("extractPorts(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i, p := range got {
			if p != tt.want[i] {
				t.Errorf("extractPorts(%q)[%d] = %d, want %d", tt.input, i, p, tt.want[i])
			}
		}
	}
}

func TestParseLabels(t *testing.T) {
	d := &DockerScanner{logger: slog.New(slog.NewTextHandler(os.Stderr, nil))}

	labels := d.parseLabels("com.docker.compose.project=myapp,com.docker.compose.service=web,com.docker.compose.project.working_dir=/Users/chris/Code/myapp")
	if labels["com.docker.compose.project"] != "myapp" {
		t.Errorf("unexpected project: %s", labels["com.docker.compose.project"])
	}
	if labels["com.docker.compose.service"] != "web" {
		t.Errorf("unexpected service: %s", labels["com.docker.compose.service"])
	}
	if labels["com.docker.compose.project.working_dir"] != "/Users/chris/Code/myapp" {
		t.Errorf("unexpected working_dir: %s", labels["com.docker.compose.project.working_dir"])
	}
}

func TestDockerMatchProject(t *testing.T) {
	d := &DockerScanner{
		rootDirs: []string{"/Users/chris/Code"},
		logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	name, dir := d.matchProject("/Users/chris/Code/myapp")
	if name != "myapp" {
		t.Errorf("expected myapp, got %s", name)
	}
	if dir != "/Users/chris/Code/myapp" {
		t.Errorf("expected /Users/chris/Code/myapp, got %s", dir)
	}

	name, dir = d.matchProject("/Users/chris/Code/myapp/services/api")
	if name != "api.services.myapp" {
		t.Errorf("expected api.services.myapp, got %s", name)
	}
	if dir != "/Users/chris/Code/myapp/services/api" {
		t.Errorf("expected /Users/chris/Code/myapp/services/api, got %s", dir)
	}

	name, _ = d.matchProject("/other/path")
	if name != "" {
		t.Errorf("expected empty, got %s", name)
	}
}

func TestResolveNamesCompose(t *testing.T) {
	d := &DockerScanner{
		rootDirs: []string{"/Users/chris/Code"},
		logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	c := dockerContainer{
		Names:  "myapp-web-1",
		Ports:  "0.0.0.0:3000->3000/tcp",
		Labels: "com.docker.compose.project.working_dir=/Users/chris/Code/myapp,com.docker.compose.service=web",
	}
	labels := d.parseLabels(c.Labels)
	names := d.resolveNames(c, labels)

	// Single port compose: should get bare name, service-qualified, and port-qualified
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d: %+v", len(names), names)
	}
	if names[0].name != "myapp" || names[0].port != 3000 {
		t.Errorf("expected myapp:3000, got %s:%d", names[0].name, names[0].port)
	}
	if names[1].name != "myapp-web" || names[1].port != 3000 {
		t.Errorf("expected myapp-web:3000, got %s:%d", names[1].name, names[1].port)
	}
	if names[2].name != "myapp-3000" || names[2].port != 3000 || names[2].project != "myapp" {
		t.Errorf("expected myapp-3000:3000 (project=myapp), got %s:%d (project=%s)", names[2].name, names[2].port, names[2].project)
	}
}

func TestResolveNamesStandalone(t *testing.T) {
	d := &DockerScanner{
		rootDirs: []string{"/Users/chris/Code"},
		logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	c := dockerContainer{
		Names:  "my-redis",
		Ports:  "0.0.0.0:6379->6379/tcp",
		Labels: "",
	}
	labels := d.parseLabels(c.Labels)
	names := d.resolveNames(c, labels)

	// Standalone: bare name + port-qualified
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d: %+v", len(names), names)
	}
	if names[0].name != "my-redis" || names[0].port != 6379 {
		t.Errorf("expected my-redis:6379, got %s:%d", names[0].name, names[0].port)
	}
	if names[1].name != "my-redis-6379" || names[1].port != 6379 || names[1].project != "my-redis" {
		t.Errorf("expected my-redis-6379:6379 (project=my-redis), got %s:%d (project=%s)", names[1].name, names[1].port, names[1].project)
	}
}

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type DockerScanner struct {
	registry  *Registry
	rootDirs  []string
	interval  time.Duration
	logger    *slog.Logger
	warned    bool
	dockerBin string
}

func NewDockerScanner(registry *Registry, rootDirs []string, interval time.Duration, logger *slog.Logger) *DockerScanner {
	return &DockerScanner{
		registry: registry,
		rootDirs: rootDirs,
		interval: interval,
		logger:   logger,
	}
}

// findDocker returns the path to the docker binary.
// exec.LookPath only searches PATH, which is minimal under LaunchDaemons,
// so we also check common install locations.
func findDocker() string {
	if p, err := exec.LookPath("docker"); err == nil {
		return p
	}
	for _, p := range []string{
		"/opt/homebrew/bin/docker",
		"/usr/local/bin/docker",
		"/Applications/Docker.app/Contents/Resources/bin/docker",
	} {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}
	return ""
}

func (d *DockerScanner) Run(ctx context.Context) {
	// Check if docker is available
	dockerBin := findDocker()
	if dockerBin == "" {
		d.logger.Info("docker not found, docker scanner disabled")
		return
	}
	d.dockerBin = dockerBin

	d.logger.Info("docker scanner started", "interval", d.interval)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	d.scan(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.scan(ctx)
		}
	}
}

type dockerContainer struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	Ports  string `json:"Ports"`
	Labels string `json:"Labels"`
}

// portPattern matches published ports like "0.0.0.0:3000->3000/tcp"
var portPattern = regexp.MustCompile(`(?:\d+\.\d+\.\d+\.\d+|::):(\d+)->(\d+)/\w+`)

func (d *DockerScanner) scan(ctx context.Context) {
	cycleStart := time.Now()

	out, err := exec.CommandContext(ctx, d.dockerBin, "ps", "--format", "{{json .}}").Output()
	if err != nil {
		if !d.warned {
			d.logger.Warn("docker ps failed, is Docker running?", "err", err)
			d.warned = true
		}
		return
	}
	d.warned = false

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var c dockerContainer
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}

		ports := d.extractPorts(c.Ports)
		if len(ports) == 0 {
			continue
		}

		labels := d.parseLabels(c.Labels)
		names := d.resolveNames(c, labels)

		for _, nm := range names {
			if nm.project != "" {
				d.registry.RegisterFull(Registration{
					Name:    nm.name,
					Port:    nm.port,
					Source:  SourceDocker,
					Dir:     nm.dir,
					Project: nm.project,
				})
			} else {
				d.registry.Register(nm.name, nm.port, SourceDocker, 0, nm.dir)
			}
		}
	}

	d.registry.PurgeStale(SourceDocker, cycleStart)
}

type namedPort struct {
	name    string
	port    int
	dir     string
	project string // base project name for grouping (empty = same as name)
}

func (d *DockerScanner) resolveNames(c dockerContainer, labels map[string]string) []namedPort {
	ports := d.extractPorts(c.Ports)
	if len(ports) == 0 {
		return nil
	}

	// Strategy 1: Compose working_dir label → match against root dirs
	if workDir, ok := labels["com.docker.compose.project.working_dir"]; ok {
		projectName, projectDir := d.matchProject(workDir)
		if projectName != "" {
			service := labels["com.docker.compose.service"]

			// Check dotfile
			dl, err := ParseDotLocalhost(filepath.Join(projectDir, ".localhost"))
			if err == nil && dl.Name != "" {
				projectName = dl.Name
			}

			var result []namedPort

			if service != "" {
				// Multi-service: qualify with service name, pick first port as bare name
				if len(ports) == 1 {
					result = append(result, namedPort{name: projectName, port: ports[0], dir: projectDir})
				}
				for _, p := range ports {
					qualifiedName := projectName + "-" + service
					result = append(result, namedPort{name: qualifiedName, port: p, dir: projectDir})
				}
			} else if len(ports) > 0 {
				// Single service: register best port as bare name
				result = append(result, namedPort{name: projectName, port: ports[0], dir: projectDir})
			}

			// Register port-qualified names for all ports
			for _, p := range ports {
				result = append(result, namedPort{
					name:    PortQualifiedName(projectName, p),
					port:    p,
					dir:     projectDir,
					project: projectName,
				})
			}

			// Register dotfile [ports] named subdomains
			if dl, err := ParseDotLocalhost(filepath.Join(projectDir, ".localhost")); err == nil {
				for _, pm := range dl.Ports {
					result = append(result, namedPort{
						name:    pm.Subdomain + "." + projectName,
						port:    pm.Port,
						dir:     projectDir,
						project: projectName,
					})
				}
			}

			return result
		}
	}

	// Strategy 2: Container name
	name := strings.TrimPrefix(c.Names, "/")
	if name == "" {
		return nil
	}
	name = strings.ToLower(name)

	if len(ports) > 0 {
		var result []namedPort
		result = append(result, namedPort{name: name, port: ports[0]})
		for _, p := range ports {
			result = append(result, namedPort{
				name:    PortQualifiedName(name, p),
				port:    p,
				project: name,
			})
		}
		return result
	}
	return nil
}

func (d *DockerScanner) extractPorts(portsStr string) []int {
	matches := portPattern.FindAllStringSubmatch(portsStr, -1)
	var ports []int
	for _, m := range matches {
		hostPort, err := strconv.Atoi(m[1])
		if err == nil && hostPort > 0 {
			ports = appendUnique(ports, hostPort)
		}
	}
	return ports
}

func (d *DockerScanner) parseLabels(labelsStr string) map[string]string {
	result := make(map[string]string)
	for _, pair := range strings.Split(labelsStr, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if ok {
			result[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return result
}

func (d *DockerScanner) matchProject(dir string) (string, string) {
	for _, root := range d.rootDirs {
		root = strings.TrimSuffix(root, "/")
		if !strings.HasPrefix(dir, root+"/") {
			continue
		}
		rel := dir[len(root)+1:]
		parts := strings.Split(rel, "/")
		if len(parts) == 0 {
			continue
		}

		for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
			parts[i], parts[j] = parts[j], parts[i]
		}
		for i := range parts {
			parts[i] = strings.ToLower(parts[i])
		}
		return strings.Join(parts, "."), dir
	}
	return "", ""
}

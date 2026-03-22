package main

import (
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Scanner struct {
	registry *Registry
	rootDirs []string
	interval time.Duration
	logger   *slog.Logger
}

func NewScanner(registry *Registry, rootDirs []string, interval time.Duration, logger *slog.Logger) *Scanner {
	return &Scanner{
		registry: registry,
		rootDirs: rootDirs,
		interval: interval,
		logger:   logger,
	}
}

func (s *Scanner) Run(ctx context.Context) {
	s.logger.Info("scanner started", "interval", s.interval, "roots", s.rootDirs)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.scan(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scan(ctx)
		}
	}
}

func (s *Scanner) scan(ctx context.Context) {
	cycleStart := time.Now()

	// Step 1: Get listening ports
	listeners, cmds, err := s.getListeners(ctx)
	if err != nil {
		s.logger.Error("lsof failed", "err", err)
		return
	}

	if len(listeners) == 0 {
		s.registry.PurgeStale(SourceScanner, cycleStart)
		s.registry.PurgeStale(SourceDotfile, cycleStart)
		return
	}

	// Step 2: Resolve CWDs
	cwds, err := s.getCWDs(ctx, listeners)
	if err != nil {
		s.logger.Error("cwd resolution failed", "err", err)
		return
	}

	// Step 3: Group all ports by project name
	type projectInfo struct {
		dir   string
		ports []portEntry
		pid   int
	}
	projects := make(map[string]*projectInfo)

	for pid, ports := range listeners {
		cwd, ok := cwds[pid]
		if !ok {
			continue
		}

		projectName, projectDir := s.matchProject(cwd)
		if projectName == "" {
			continue
		}

		// Check dotfile for name override
		dl, err := ParseDotLocalhost(filepath.Join(projectDir, ".localhost"))
		if err == nil && dl.Name != "" {
			projectName = dl.Name
		}

		cmd := cmds[pid]
		p, ok := projects[projectName]
		if !ok {
			p = &projectInfo{dir: projectDir, pid: pid}
			projects[projectName] = p
		}
		for _, port := range ports {
			p.ports = append(p.ports, portEntry{port: port, cmd: cmd})
		}
	}

	// Step 4: Register best port + all port-qualified variants per project
	for name, p := range projects {
		// Deduplicate ports
		seen := make(map[int]bool)
		var unique []portEntry
		for _, pe := range p.ports {
			if !seen[pe.port] {
				seen[pe.port] = true
				unique = append(unique, pe)
			}
		}

		// Register the best port as the bare project name
		bestPort := s.pickPort(p.dir, unique)
		if bestPort == 0 {
			continue
		}
		s.registry.Register(name, bestPort, SourceScanner, p.pid, p.dir)

		// Register all ports with port-qualified names
		for _, pe := range unique {
			qualifiedName := PortQualifiedName(name, pe.port)
			reg := Registration{
				Name:      qualifiedName,
				Port:      pe.port,
				Source:    SourceScanner,
				PID:       p.pid,
				Dir:       p.dir,
				Project:   name,
				UpdatedAt: time.Now(),
			}
			s.registry.RegisterFull(reg)
		}

		// Register dotfile [ports] named subdomains
		dl, err := ParseDotLocalhost(filepath.Join(p.dir, ".localhost"))
		if err == nil {
			for _, pm := range dl.Ports {
				subName := pm.Subdomain + "." + name
				reg := Registration{
					Name:      subName,
					Port:      pm.Port,
					Source:    SourceDotfile,
					Dir:       p.dir,
					Project:   name,
					UpdatedAt: time.Now(),
				}
				s.registry.RegisterFull(reg)
			}
		}
	}

	s.registry.PurgeStale(SourceScanner, cycleStart)
	s.registry.PurgeStale(SourceDotfile, cycleStart)
}

// listener info: pid → list of ports
type listenerMap map[int][]int

// commandMap: pid → command name
type commandMap map[int]string

// portEntry pairs a port number with the command that owns it.
type portEntry struct {
	port int
	cmd  string
}

var excludedCommands = map[string]bool{
	// System services
	"mDNSResponder":      true,
	"airportd":           true,
	"configd":            true,
	"syslogd":            true,
	"identityservicesd":  true,
	"sharingd":           true,
	"rapportd":           true,
	"ControlCenter":      true,
	"SystemUIServer":     true,
	"WiFiAgent":          true,
	"UserEventAgent":     true,
	"launchd":            true,
	// Browsers (their CWDs can match project dirs, polluting results)
	"Google":             true, // Chrome
	"Google Chrome":      true,
	"firefox":            true,
	"Safari":             true,
	"Arc":                true,
	"Brave Browser":      true,
	"Microsoft Edge":     true,
	// Editors/IDEs (same CWD issue)
	"Cursor":             true,
	"Cursor Helper":      true,
	"Cursor Helper (Plugin)": true,
	"Cursor Helper (GPU)": true,
	"Cursor Helper (Renderer)": true,
	"Code Helper":        true,
	"Code Helper (Plugin)": true,
	"Code Helper (GPU)":  true,
	"Code Helper (Renderer)": true,
	"Electron":           true,
}

// getListeners parses lsof output to find listening TCP ports.
func (s *Scanner) getListeners(ctx context.Context) (listenerMap, commandMap, error) {
	out, err := exec.CommandContext(ctx, "lsof", "-i", "-P", "-n", "-sTCP:LISTEN", "-F", "pcn").Output()
	if err != nil {
		// lsof returns exit code 1 when no results found
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	listeners, cmds := parseLsofListeners(string(out))
	return listeners, cmds, nil
}

// parseLsofListeners parses the -F pcn output format.
// Lines: p<pid>, c<command>, n<address>
func parseLsofListeners(output string) (listenerMap, commandMap) {
	result := make(listenerMap)
	cmds := make(commandMap)
	var currentPID int
	var currentCmd string

	for _, line := range strings.Split(output, "\n") {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, err := strconv.Atoi(line[1:])
			if err == nil {
				currentPID = pid
			}
		case 'c':
			currentCmd = line[1:]
		case 'n':
			if currentPID == 0 || excludedCommands[currentCmd] {
				continue
			}
			addr := line[1:]
			port := parsePort(addr)
			if port > 0 && port < 65536 {
				result[currentPID] = appendUnique(result[currentPID], port)
				cmds[currentPID] = currentCmd
			}
		}
	}

	return result, cmds
}

// parsePort extracts port from addresses like "*:3000", "127.0.0.1:3000", "[::1]:3000"
func parsePort(addr string) int {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return 0
	}
	port, err := strconv.Atoi(addr[idx+1:])
	if err != nil {
		return 0
	}
	return port
}

func appendUnique(slice []int, val int) []int {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	return append(slice, val)
}

// getCWDs resolves working directories for a set of PIDs.
func (s *Scanner) getCWDs(ctx context.Context, listeners listenerMap) (map[int]string, error) {
	pids := make([]string, 0, len(listeners))
	for pid := range listeners {
		pids = append(pids, strconv.Itoa(pid))
	}

	out, err := exec.CommandContext(ctx, "lsof", "-a", "-p", strings.Join(pids, ","), "-d", "cwd", "-F", "pn").Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	return parseLsofCWDs(string(out)), nil
}

// parseLsofCWDs parses -F pn output: p<pid>, n<path>
func parseLsofCWDs(output string) map[int]string {
	result := make(map[int]string)
	var currentPID int

	for _, line := range strings.Split(output, "\n") {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, err := strconv.Atoi(line[1:])
			if err == nil {
				currentPID = pid
			}
		case 'n':
			if currentPID > 0 {
				result[currentPID] = line[1:]
			}
		}
	}
	return result
}

// matchProject checks if a CWD is under any root directory and extracts the project name.
// All path components are reversed into DNS subdomain order:
//   ~/Code/app            → "app"
//   ~/Code/app/service    → "service.app"
//   ~/Code/app/pkg/web    → "web.pkg.app"
func (s *Scanner) matchProject(cwd string) (name string, dir string) {
	for _, root := range s.rootDirs {
		root = strings.TrimSuffix(root, "/")
		if !strings.HasPrefix(cwd, root+"/") {
			continue
		}
		rel := cwd[len(root)+1:]
		parts := strings.Split(rel, "/")
		if len(parts) == 0 {
			continue
		}

		// Reverse parts into DNS subdomain order
		for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
			parts[i], parts[j] = parts[j], parts[i]
		}
		for i := range parts {
			parts[i] = strings.ToLower(parts[i])
		}
		return strings.Join(parts, "."), cwd
	}
	return "", ""
}

// webServerCommands are process names that indicate an HTTP/web server.
// Ports owned by these processes are preferred over others.
var webServerCommands = map[string]bool{
	"node":       true,
	"deno":       true,
	"bun":        true,
	"python":     true,
	"python3":    true,
	"ruby":       true,
	"php":        true,
	"go":         true,
	"java":       true,
	"beam.smp":   true, // Erlang/Elixir
	"dotnet":     true,
	"next-serve": true,
	"vite":       true,
	"nginx":      true,
	"caddy":      true,
	"uvicorn":    true,
	"gunicorn":   true,
	"puma":       true,
}

// pickPort selects the best port for a project.
// Priority: dotfile > web-server process port > lowest non-system port.
// No connections are attempted; selection is based on process name heuristics.
func (s *Scanner) pickPort(projectDir string, ports []portEntry) int {
	dl, err := ParseDotLocalhost(filepath.Join(projectDir, ".localhost"))
	if err == nil && dl.Port > 0 {
		return dl.Port
	}

	// Deduplicate
	seen := make(map[int]bool)
	var unique []portEntry
	for _, p := range ports {
		if !seen[p.port] {
			seen[p.port] = true
			unique = append(unique, p)
		}
	}

	if len(unique) == 1 {
		return unique[0].port
	}

	// Prefer ports owned by known web server processes
	var webPorts []portEntry
	for _, p := range unique {
		if webServerCommands[p.cmd] {
			webPorts = append(webPorts, p)
		}
	}

	pick := unique
	if len(webPorts) > 0 {
		pick = webPorts
	}

	// Pick lowest non-system port from the candidates
	best := 0
	for _, p := range pick {
		if p.port >= 1024 && (best == 0 || p.port < best) {
			best = p.port
		}
	}
	if best == 0 && len(pick) > 0 {
		best = pick[0].port
	}
	return best
}

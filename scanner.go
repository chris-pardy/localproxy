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
	listeners, err := s.getListeners(ctx)
	if err != nil {
		s.logger.Error("lsof failed", "err", err)
		return
	}

	if len(listeners) == 0 {
		s.registry.PurgeStale(SourceScanner, cycleStart)
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
		ports []int
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

		p, ok := projects[projectName]
		if !ok {
			p = &projectInfo{dir: projectDir, pid: pid}
			projects[projectName] = p
		}
		p.ports = append(p.ports, ports...)
	}

	// Step 4: Pick best port per project and register
	for name, p := range projects {
		port := s.pickPort(p.dir, p.ports)
		if port == 0 {
			continue
		}
		s.registry.Register(name, port, SourceScanner, p.pid, p.dir)
	}

	s.registry.PurgeStale(SourceScanner, cycleStart)
}

// listener info: pid → list of ports
type listenerMap map[int][]int

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
func (s *Scanner) getListeners(ctx context.Context) (listenerMap, error) {
	out, err := exec.CommandContext(ctx, "lsof", "-i", "-P", "-n", "-sTCP:LISTEN", "-F", "pcn").Output()
	if err != nil {
		// lsof returns exit code 1 when no results found
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	return parseLsofListeners(string(out)), nil
}

// parseLsofListeners parses the -F pcn output format.
// Lines: p<pid>, c<command>, n<address>
func parseLsofListeners(output string) listenerMap {
	result := make(listenerMap)
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
			}
		}
	}

	return result
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

// pickPort selects the best port for a project.
// Priority: dotfile > HTTP-responsive port > lowest non-system port.
func (s *Scanner) pickPort(projectDir string, ports []int) int {
	dl, err := ParseDotLocalhost(filepath.Join(projectDir, ".localhost"))
	if err == nil && dl.Port > 0 {
		return dl.Port
	}

	// Deduplicate
	seen := make(map[int]bool)
	var unique []int
	for _, p := range ports {
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}

	// If only one candidate, skip probing
	if len(unique) == 1 {
		return unique[0]
	}

	// Probe candidates with HTTP OPTIONS, prefer responders
	var httpPorts []int
	for _, p := range unique {
		if isHTTPAlive(p) {
			httpPorts = append(httpPorts, p)
		}
	}

	pick := unique
	if len(httpPorts) > 0 {
		pick = httpPorts
	}

	// Pick lowest non-system port from the candidates
	best := 0
	for _, p := range pick {
		if p >= 1024 && (best == 0 || p < best) {
			best = p
		}
	}
	if best == 0 && len(pick) > 0 {
		best = pick[0]
	}
	return best
}

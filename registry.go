package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Source int

const (
	SourceBackchannel Source = iota
	SourceDotfile
	SourceDocker
	SourceScanner
)

func (s Source) String() string {
	switch s {
	case SourceBackchannel:
		return "backchannel"
	case SourceDotfile:
		return "dotfile"
	case SourceDocker:
		return "docker"
	case SourceScanner:
		return "scanner"
	default:
		return "unknown"
	}
}

type Registration struct {
	Name      string
	Port      int
	Source    Source
	PID       int
	Dir       string
	Project   string // base project name for grouping (empty = same as Name)
	UpdatedAt time.Time
}

// ProjectName returns the base project name for grouping.
func (r Registration) ProjectName() string {
	if r.Project != "" {
		return r.Project
	}
	return r.Name
}

// PortQualifiedName returns the project name with port qualifier on the first label.
// e.g. "app" + 3001 → "app-3001", "web.pkg.app" + 3001 → "web-3001.pkg.app"
func PortQualifiedName(projectName string, port int) string {
	parts := strings.SplitN(projectName, ".", 2)
	qualified := fmt.Sprintf("%s-%d", parts[0], port)
	if len(parts) > 1 {
		return qualified + "." + parts[1]
	}
	return qualified
}

type Registry struct {
	mu      sync.RWMutex
	entries map[string][]Registration // name → registrations, one per source
}

func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string][]Registration),
	}
}

// Register adds or updates a registration for the given name and source.
// If a registration from the same source already exists, it is replaced.
func (r *Registry) Register(name string, port int, source Source, pid int, dir string) {
	r.RegisterFull(Registration{
		Name:      name,
		Port:      port,
		Source:    source,
		PID:       pid,
		Dir:       dir,
		UpdatedAt: time.Now(),
	})
}

// RegisterFull adds or updates a registration using a complete Registration struct.
func (r *Registry) RegisterFull(reg Registration) {
	if reg.UpdatedAt.IsZero() {
		reg.UpdatedAt = time.Now()
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	regs := r.entries[reg.Name]
	for i, existing := range regs {
		if existing.Source == reg.Source {
			regs[i] = reg
			return
		}
	}
	r.entries[reg.Name] = append(regs, reg)
}

// Unregister removes the registration for the given name and source.
func (r *Registry) Unregister(name string, source Source) {
	r.mu.Lock()
	defer r.mu.Unlock()

	regs := r.entries[name]
	for i, existing := range regs {
		if existing.Source == source {
			r.entries[name] = append(regs[:i], regs[i+1:]...)
			if len(r.entries[name]) == 0 {
				delete(r.entries, name)
			}
			return
		}
	}
}

// Resolve returns the highest-priority registration for the given name.
func (r *Registry) Resolve(name string) (Registration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	regs := r.entries[name]
	if len(regs) == 0 {
		return Registration{}, false
	}

	best := regs[0]
	for _, reg := range regs[1:] {
		if reg.Source < best.Source {
			best = reg
		}
	}
	return best, true
}

// All returns the resolved (highest-priority) registration for each name.
func (r *Registry) All() []Registration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Registration, 0, len(r.entries))
	for _, regs := range r.entries {
		best := regs[0]
		for _, reg := range regs[1:] {
			if reg.Source < best.Source {
				best = reg
			}
		}
		result = append(result, best)
	}
	return result
}

// ProjectGroup is a project with its default registration and port variants.
type ProjectGroup struct {
	Default  Registration
	Variants []Registration // port-qualified and subdomain variants
}

// AllGrouped returns registrations grouped by project name.
// Each group has a default (the bare project name) and variants (port-qualified entries).
func (r *Registry) AllGrouped() []ProjectGroup {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// First resolve all entries
	resolved := make(map[string]Registration, len(r.entries))
	for name, regs := range r.entries {
		best := regs[0]
		for _, reg := range regs[1:] {
			if reg.Source < best.Source {
				best = reg
			}
		}
		resolved[name] = best
	}

	// Group by project name
	groups := make(map[string]*ProjectGroup)
	for _, reg := range resolved {
		project := reg.ProjectName()
		g, ok := groups[project]
		if !ok {
			g = &ProjectGroup{}
			groups[project] = g
		}
		if reg.Name == project {
			g.Default = reg
		} else {
			g.Variants = append(g.Variants, reg)
		}
	}

	// For projects that only have variants (no bare name registered),
	// pick the first variant as the default
	result := make([]ProjectGroup, 0, len(groups))
	for _, g := range groups {
		if g.Default.Name == "" && len(g.Variants) > 0 {
			g.Default = g.Variants[0]
			g.Variants = g.Variants[1:]
		}
		result = append(result, *g)
	}
	return result
}

// PurgeStale removes registrations from the given source that were
// last updated before the given time.
func (r *Registry) PurgeStale(source Source, before time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, regs := range r.entries {
		for i, reg := range regs {
			if reg.Source == source && reg.UpdatedAt.Before(before) {
				r.entries[name] = append(regs[:i], regs[i+1:]...)
				if len(r.entries[name]) == 0 {
					delete(r.entries, name)
				}
				break // at most one per source per name
			}
		}
	}
}

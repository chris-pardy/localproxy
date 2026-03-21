package main

import (
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
	UpdatedAt time.Time
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
	r.mu.Lock()
	defer r.mu.Unlock()

	reg := Registration{
		Name:      name,
		Port:      port,
		Source:    source,
		PID:       pid,
		Dir:       dir,
		UpdatedAt: time.Now(),
	}

	regs := r.entries[name]
	for i, existing := range regs {
		if existing.Source == source {
			regs[i] = reg
			return
		}
	}
	r.entries[name] = append(regs, reg)
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

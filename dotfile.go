package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// PortMapping is a named subdomain mapped to a port.
type PortMapping struct {
	Subdomain string
	Port      int
}

type DotLocalhost struct {
	Name  string        // empty = use directory name
	Port  int           // 0 = auto-detect
	Ports []PortMapping // named subdomain → port mappings
}

// ParseDotLocalhost reads a .localhost file and returns its configuration.
// Supports a [ports] section for mapping named subdomains to ports:
//
//	name = myapp
//	port = 3000
//
//	[ports]
//	api = 3001
//	docs = 4000
//
// This registers api.myapp.localhost → 3001, docs.myapp.localhost → 4000
func ParseDotLocalhost(path string) (DotLocalhost, error) {
	f, err := os.Open(path)
	if err != nil {
		return DotLocalhost{}, err
	}
	defer f.Close()

	var dl DotLocalhost
	section := "" // "" = top-level, "ports" = [ports] section
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Section headers
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch section {
		case "":
			switch key {
			case "name":
				dl.Name = val
			case "port":
				p, err := strconv.Atoi(val)
				if err == nil && p > 0 && p <= 65535 {
					dl.Port = p
				}
			}
		case "ports":
			p, err := strconv.Atoi(val)
			if err == nil && p > 0 && p <= 65535 {
				dl.Ports = append(dl.Ports, PortMapping{Subdomain: key, Port: p})
			}
		}
	}
	return dl, scanner.Err()
}

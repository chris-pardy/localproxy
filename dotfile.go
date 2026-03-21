package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type DotLocalhost struct {
	Name string // empty = use directory name
	Port int    // 0 = auto-detect
}

// ParseDotLocalhost reads a .localhost file and returns its configuration.
func ParseDotLocalhost(path string) (DotLocalhost, error) {
	f, err := os.Open(path)
	if err != nil {
		return DotLocalhost{}, err
	}
	defer f.Close()

	var dl DotLocalhost
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch key {
		case "name":
			dl.Name = val
		case "port":
			p, err := strconv.Atoi(val)
			if err == nil && p > 0 && p <= 65535 {
				dl.Port = p
			}
		}
	}
	return dl, scanner.Err()
}

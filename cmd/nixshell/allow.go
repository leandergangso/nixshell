package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The trust database lives outside the repo. Cache cleanup must not remove it.
func configRoot() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "nixshell"), nil
}

func allowFile() (string, error) {
	dir, err := configRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "allow"), nil
}

func readAllowList() (map[string]bool, error) {
	path, err := allowFile()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]bool), nil
	}
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allowed[filepath.Clean(line)] = true
	}
	return allowed, nil
}

func saveAllowList(allowed map[string]bool) error {
	path, err := allowFile()
	if err != nil {
		return err
	}
	lines := make([]string, 0, len(allowed))
	for dir := range allowed {
		lines = append(lines, dir)
	}
	sort.Strings(lines)
	var b strings.Builder
	for _, dir := range lines {
		fmt.Fprintln(&b, dir)
	}
	return writeFileAtomic(path, []byte(b.String()), 0o600)
}

func isAllowed(dir string) bool {
	allowed, err := readAllowList()
	return err == nil && allowed[filepath.Clean(dir)]
}

func allowDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	allowed, err := readAllowList()
	if err != nil {
		return "", err
	}
	if !allowed[dir] {
		allowed[dir] = true
		if err := saveAllowList(allowed); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func revokeDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	allowed, err := readAllowList()
	if err != nil {
		return "", err
	}
	if allowed[dir] {
		delete(allowed, dir)
		if err := saveAllowList(allowed); err != nil {
			return "", err
		}
	}
	return dir, nil
}

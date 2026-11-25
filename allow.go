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
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseAllowList(data), nil
}

func parseAllowList(data []byte) map[string]bool {
	allowed := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allowed[filepath.Clean(line)] = true
	}
	return allowed
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
	data := []byte(strings.Join(lines, "\n"))
	if len(data) > 0 {
		data = append(data, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func allowDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := projectRoot(dir)
	if err != nil {
		return "", err
	}
	allowed, err := readAllowList()
	if err != nil {
		return "", err
	}
	if !allowed[root] {
		allowed[root] = true
		if err := saveAllowList(allowed); err != nil {
			return "", err
		}
	}
	return root, nil
}

func denyDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := projectRoot(dir)
	if err != nil {
		return "", err
	}
	allowed, err := readAllowList()
	if err != nil {
		return "", err
	}
	if allowed[root] {
		delete(allowed, root)
		if err := saveAllowList(allowed); err != nil {
			return "", err
		}
	}
	return root, nil
}

// projectRoot returns the nearest ancestor containing a nix file. It is used
// by --allow/--deny so the trust database stores project roots, not arbitrary
// subdirectories. With no project file, it falls back to the current dir.
func projectRoot(dir string) (string, error) {
	for current := filepath.Clean(dir); ; {
		_, ok, err := nixFiles(current)
		if err != nil {
			return "", err
		}
		if ok {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(dir), nil
		}
		current = parent
	}
}

// purgeApprovals removes the trust database and its directory only when the
// directory is empty. Cache cleanup stays separate in --clean.
func purgeApprovals() error {
	path, err := allowFile()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}

	root, err := configRoot()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		if err := os.Remove(root); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", root, err)
		}
	}
	return nil
}

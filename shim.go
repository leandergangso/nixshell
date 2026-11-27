package main

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
)

//go:embed shims
var shimFS embed.FS

func bashShim() (string, error) {
	dir, err := shimDir("bash")
	if err != nil {
		return "", err
	}
	return writeShimFile(dir, "bashrc")
}

func zshShim() (string, error) {
	dir, err := shimDir("zsh")
	if err != nil {
		return "", err
	}
	if _, err := writeShimFile(dir, ".zshenv"); err != nil {
		return "", err
	}
	if _, err := writeShimFile(dir, ".zshrc"); err != nil {
		return "", err
	}
	return dir, nil
}

func fishShim() (string, error) {
	content, err := shimSource("init.fish")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(content), nil
}

// ---

func writeShimFile(dir, name string) (string, error) {
	content, err := shimSource(strings.TrimPrefix(name, "."))
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	return path, writeShim(path, content)
}

func shimDir(kind string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cacheDir, "nixshell", "rc", kind)
	return dir, os.MkdirAll(dir, 0o700)
}

func shimSource(name string) (string, error) {
	data, err := shimFS.ReadFile("shims/" + name)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeShim(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var nixFeatures = []string{"--extra-experimental-features", "nix-command flakes"}

type printDevEnv struct {
	Variables map[string]nixVar `json:"variables"`
}

type nixVar struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

func devEnv(ctx context.Context, devShell string, base []string) ([]string, error) {
	dir, err := profileDir()
	if err != nil {
		return nil, err
	}
	profile := filepath.Join(dir, cacheKey(devShell))

	args := append([]string{}, nixFeatures...)
	args = append(args, "print-dev-env", "--json", "--profile", profile, devShell)

	cmd := exec.CommandContext(ctx, "nix", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("nix print-dev-env: %w", err)
	}

	var doc printDevEnv
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		return nil, fmt.Errorf("parse print-dev-env json: %w", err)
	}

	shellPath := stringVar(doc, "shell")
	if shellPath == "" {
		shellPath = stringVar(doc, "BASH")
	}
	if shellPath == "" {
		shellPath = stringVar(doc, "CONFIG_SHELL")
	}
	if shellPath == "" {
		return nil, fmt.Errorf("print-dev-env did not define variables.shell")
	}

	env := envMap(base)
	for key := range ignoredEnv {
		delete(env, key)
	}

	exported := make(map[string]bool)
	for key, entry := range doc.Variables {
		if entry.Type != "exported" || ignoredEnv[key] {
			continue
		}
		value, ok := stringValue(entry.Value)
		if !ok {
			continue
		}
		env[key] = value
		exported[key] = true
	}

	captured, err := runHook(ctx, shellPath, stringVar(doc, "shellHook"), env)
	if err != nil {
		return nil, err
	}
	for key, value := range captured {
		env[key] = value
	}
	for key := range exported {
		if _, ok := captured[key]; !ok {
			delete(env, key)
		}
	}

	return envSlice(env), nil
}

func stringVar(doc printDevEnv, key string) string {
	entry, ok := doc.Variables[key]
	if !ok {
		return ""
	}
	value, _ := stringValue(entry.Value)
	return value
}

func stringValue(raw json.RawMessage) (string, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func runHook(ctx context.Context, shellPath, hook string, env map[string]string) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, shellPath, "-c", "eval \"${NIXSHELL_HOOK-}\" >&2\nenv -0")
	cmd.Env = append(envSlice(env), "NIXSHELL_HOOK="+hook)
	cmd.Stdin = nil
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("run shellHook: %w", err)
	}

	changes := make(map[string]string)
	for key, value := range parseEnv0(stdout.Bytes()) {
		if ignoredEnv[key] || !validEnvKey(key) {
			continue
		}
		changes[key] = value
	}
	return changes, nil
}

func parseEnv0(data []byte) map[string]string {
	out := make(map[string]string)
	for _, entry := range bytes.Split(data, []byte{0}) {
		key, value, ok := bytes.Cut(entry, []byte{'='})
		if ok {
			out[string(key)] = string(value)
		}
	}
	return out
}

var ignoredEnv = map[string]bool{
	"HOME": true, "shellHook": true, "NIX_BUILD_TOP": true,
	"TMP": true, "TMPDIR": true, "TEMP": true, "TEMPDIR": true,
	"terminfo": true, "SHLVL": true, "_": true, "PWD": true, "OLDPWD": true,
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z'):
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

func envMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok {
			out[key] = value
		}
	}
	return out
}

func envSlice(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func cacheRoot() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "nixshell"), nil
}

func profileDir() (string, error) {
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "profiles")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func cacheKey(devShell string) string {
	key := devShell
	if isLocalRef(devShell) {
		if cwd, err := os.Getwd(); err == nil {
			key = cwd + "#" + devShell
		}
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:16]
}

func isLocalRef(devShell string) bool {
	ref := devShell
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		ref = ref[:i]
	}
	return ref == "" || ref == "." || strings.HasPrefix(ref, "./") ||
		strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "path:")
}

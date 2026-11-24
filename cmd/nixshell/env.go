package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var nixFeatures = []string{"--extra-experimental-features", "nix-command flakes"}

// envDiff is intentionally similar to direnv's exported diff: Prev holds the
// values before entry, Next holds the values to apply. A nil value means the
// variable was, or should be, unset.
type envDiff struct {
	Prev map[string]*string `json:"prev"`
	Next map[string]*string `json:"next"`
}

// devEnv resolves extra nix print-dev-env arguments into a diff against base.
// A GC root keeps the referenced store paths alive while the env is in use.
func devEnv(cacheID string, extra []string, base []string) (envDiff, error) {
	dir, err := profileDir()
	if err != nil {
		return envDiff{}, err
	}
	key := cacheKey(cacheID)
	stableProfile := filepath.Join(dir, key)
	tmpProfile := filepath.Join(dir, key+".tmp."+strconv.Itoa(os.Getpid()))
	defer removeMatching(tmpProfile + "*")

	workDir, err := os.MkdirTemp("", "nixshell-")
	if err != nil {
		return envDiff{}, err
	}
	defer os.RemoveAll(workDir)
	rcPath := filepath.Join(workDir, "env.sh")
	envPath := filepath.Join(workDir, "env0")

	args := append([]string{}, nixFeatures...)
	args = append(args, "print-dev-env", "--profile", tmpProfile)
	args = append(args, extra...)
	if err := runNix(args, rcPath); err != nil {
		return envDiff{}, err
	}

	// Nix writes the profile symlink with a relative target.
	storePath, err := filepath.EvalSymlinks(tmpProfile)
	if err != nil {
		return envDiff{}, fmt.Errorf("nix did not create a profile at %s: %w", tmpProfile, err)
	}
	args = append([]string{}, nixFeatures...)
	args = append(args, "build", "--out-link", stableProfile, storePath)
	if err := runNix(args, os.DevNull); err != nil {
		return envDiff{}, err
	}

	// Sourcing Nix's script runs shellHook in bash. Only exported variables
	// are kept; functions, arrays and shell-local state cannot cross shells.
	if err := captureEnv(rcPath, envPath, base); err != nil {
		return envDiff{}, err
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		return envDiff{}, fmt.Errorf("read captured environment: %w", err)
	}
	return buildDiff(base, parseEnv0(data)), nil
}

func runNix(args []string, stdoutPath string) error {
	cmd := exec.Command("nix", args...)
	cmd.Stderr = os.Stderr
	if stdoutPath == os.DevNull {
		cmd.Stdout = io.Discard
	} else {
		out, err := os.Create(stdoutPath)
		if err != nil {
			return err
		}
		cmd.Stdout = out
		defer out.Close()
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nix %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func captureEnv(rcPath, envPath string, env []string) error {
	cmd := exec.Command("bash", "-c", `source "$1"; env -0 > "$2"`, "bash", rcPath, envPath)
	cmd.Env = env
	// A shellHook must not consume the user's interactive input.
	cmd.Stdin = nil
	// stdout must contain only the generated shell code in hook mode.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run shellHook: %w", err)
	}
	return nil
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

// Build temp dirs and shell-only state must not migrate into the user's shell.
var ignoredEnv = map[string]bool{
	"HOME": true, "shellHook": true, "NIX_BUILD_TOP": true,
	"TMP": true, "TMPDIR": true, "TEMP": true, "TEMPDIR": true,
	"terminfo": true, "SHLVL": true, "_": true, "PWD": true, "OLDPWD": true,
}

// validEnvKey keeps generated shell code valid. Exotic env names can still be
// passed to child processes in direct mode, but cannot be set from a shell.
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

func ptr(s string) *string { return &s }

func buildDiff(base []string, captured map[string]string) envDiff {
	current := envMap(base)
	diff := envDiff{
		Prev: make(map[string]*string),
		Next: make(map[string]*string),
	}
	keep := func(key string) bool {
		return !ignoredEnv[key] && !strings.HasPrefix(key, "BASH_FUNC_") && validEnvKey(key)
	}
	for key, old := range current {
		if !keep(key) {
			continue
		}
		value, exists := captured[key]
		switch {
		case !exists:
			diff.Prev[key] = ptr(old)
			diff.Next[key] = nil
		case value != old:
			diff.Prev[key] = ptr(old)
			diff.Next[key] = ptr(value)
		}
	}
	for key, value := range captured {
		if !keep(key) {
			continue
		}
		if _, exists := current[key]; !exists {
			diff.Prev[key] = nil
			diff.Next[key] = ptr(value)
		}
	}
	return diff
}

// patchEnv applies a diff. nil values unset the key; other values set it.
func patchEnv(env []string, changes map[string]*string) []string {
	out := envMap(env)
	for key, value := range changes {
		if value == nil {
			delete(out, key)
		} else {
			out[key] = *value
		}
	}
	keys := make([]string, 0, len(out))
	for key := range out {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+out[key])
	}
	return result
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

func cacheDir() (string, error) {
	dir, err := cacheRoot()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return dir, nil
}

// cleanCache removes cached profiles and hook state. It does not touch the
// trust database or run Nix GC.
func cleanCache() error {
	dir, err := cacheRoot()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	return nil
}

func profileDir() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	profiles := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		return "", err
	}
	return profiles, nil
}

func cacheKey(installable string) string {
	key := installable
	if isLocalRef(installable) {
		if cwd, err := os.Getwd(); err == nil {
			key = cwd + "#" + installable
		}
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:16]
}

func isLocalRef(installable string) bool {
	ref := installable
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		ref = ref[:i]
	}
	return ref == "" || ref == "." || strings.HasPrefix(ref, "./") ||
		strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "path:")
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	name := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(name)
		}
	}()
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func removeMatching(pattern string) {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, path := range paths {
		os.RemoveAll(path)
	}
}

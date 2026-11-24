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

// devEnv returns the exported variables whose values differ from base. A GC
// root is kept so the referenced store paths survive while the env is in use.
func devEnv(installable string, base []string) (map[string]string, error) {
	dir, err := profileDir()
	if err != nil {
		return nil, err
	}
	key := cacheKey(installable)
	stableProfile := filepath.Join(dir, key)
	tmpProfile := filepath.Join(dir, key+".tmp."+strconv.Itoa(os.Getpid()))
	defer removeMatching(tmpProfile + "*")

	workDir, err := os.MkdirTemp("", "nixshell-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)
	rcPath := filepath.Join(workDir, "env.sh")
	envPath := filepath.Join(workDir, "env0")

	args := append([]string{}, nixFeatures...)
	args = append(args, "print-dev-env", "--profile", tmpProfile, installable)
	if err := runNix(args, rcPath); err != nil {
		return nil, err
	}

	// Nix writes the profile symlink with a relative target.
	storePath, err := filepath.EvalSymlinks(tmpProfile)
	if err != nil {
		return nil, fmt.Errorf("nix did not create a profile at %s: %w", tmpProfile, err)
	}
	args = append([]string{}, nixFeatures...)
	args = append(args, "build", "--out-link", stableProfile, storePath)
	if err := runNix(args, os.DevNull); err != nil {
		return nil, err
	}

	// Sourcing Nix's script runs shellHook in bash. Only exported variables
	// are kept; functions, arrays and shell-local state cannot cross shells.
	if err := captureEnv(rcPath, envPath, base); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		return nil, fmt.Errorf("read captured environment: %w", err)
	}
	return changesFrom(base, parseEnv0(data)), nil
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
	cmd.Stdin = os.Stdin
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

func changesFrom(base []string, captured map[string]string) map[string]string {
	current := make(map[string]string, len(base))
	for _, entry := range base {
		if key, value, ok := strings.Cut(entry, "="); ok {
			current[key] = value
		}
	}
	changes := make(map[string]string)
	for key, value := range captured {
		if ignoredEnv[key] || strings.HasPrefix(key, "BASH_FUNC_") {
			continue
		}
		if old, exists := current[key]; !exists || old != value {
			changes[key] = value
		}
	}
	return changes
}

func applyChanges(base []string, changes map[string]string) []string {
	env := make(map[string]string, len(base)+len(changes))
	for _, entry := range base {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	for key, value := range changes {
		env[key] = value
	}
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

func envMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok {
			out[key] = value
		}
	}
	return out
}

// For each changed variable, remember its previous value (nil if unset).
func oldValues(base []string, changes map[string]string) map[string]*string {
	current := envMap(base)
	old := make(map[string]*string, len(changes))
	for key := range changes {
		if value, exists := current[key]; exists {
			v := value
			old[key] = &v
		} else {
			old[key] = nil
		}
	}
	return old
}

func restoreEnv(env []string, old map[string]*string) []string {
	out := envMap(env)
	for key, value := range old {
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return dir, nil
}

// cleanCache removes everything nixshell owns. It does not run Nix GC.
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
	if err := os.MkdirAll(profiles, 0o755); err != nil {
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

func removeMatching(pattern string) {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, path := range paths {
		os.RemoveAll(path)
	}
}

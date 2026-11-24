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

// Nix needs these features for print-dev-env; passing them explicitly means
// the tool works even when they are not enabled in nix.conf.
var nixFeatures = []string{"--extra-experimental-features", "nix-command flakes"}

// devEnv resolves installable into a set of environment variables ready for
// the user's shell. It also leaves a GC root behind so the store paths stay
// alive for as long as the environment is in use.
func devEnv(installable string, verbose bool) ([]string, error) {
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
	if err := runNix(args, rcPath, verbose); err != nil {
		return nil, err
	}

	// Nix writes the profile symlink with a relative target, so resolve it
	// before handing the store path to "nix build".
	storePath, err := filepath.EvalSymlinks(tmpProfile)
	if err != nil {
		return nil, fmt.Errorf("nix did not create a profile at %s: %w", tmpProfile, err)
	}
	// An out-link is a GC root. The temporary profile from --profile is not
	// needed afterwards, so it is removed by the deferred cleanup above.
	args = append([]string{}, nixFeatures...)
	args = append(args, "build", "--out-link", stableProfile, storePath)
	if err := runNix(args, os.DevNull, verbose); err != nil {
		return nil, err
	}

	// The script printed by nix print-dev-env sets the build environment and
	// evaluates shellHook. Sourcing it in bash is the same approach nix-direnv
	// uses; we only keep the resulting exported variables.
	if err := captureEnv(rcPath, envPath); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		return nil, fmt.Errorf("read captured environment: %w", err)
	}
	return applyEnvDelta(os.Environ(), parseEnv0(data)), nil
}

func runNix(args []string, stdoutPath string, verbose bool) error {
	if verbose {
		fmt.Fprintf(os.Stderr, "nixshell: nix %s\n", strings.Join(args, " "))
	}
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

func captureEnv(rcPath, envPath string) error {
	cmd := exec.Command("bash", "-c", `source "$1"; env -0 > "$2"`, "bash", rcPath, envPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
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

// Variables that must not be copied into the user's shell. Build temp dirs are
// created by nix print-dev-env and may be garbage collected; shellHook is code,
// not data; the rest are bash bookkeeping or shell-local state.
var ignoredEnv = map[string]bool{
	"HOME":          true,
	"shellHook":     true,
	"NIX_BUILD_TOP": true,
	"TMP":           true,
	"TMPDIR":        true,
	"TEMP":          true,
	"TEMPDIR":       true,
	"terminfo":      true,
	"SHLVL":         true,
	"_":             true,
	"PWD":           true,
	"OLDPWD":        true,
}

// Only variables whose value changed are copied. Everything else in the
// user's environment is left alone.
func applyEnvDelta(base []string, captured map[string]string) []string {
	env := make(map[string]string, len(base)+len(captured))
	for _, entry := range base {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	for key, value := range captured {
		if skipEnv(key) {
			continue
		}
		if old, exists := env[key]; !exists || old != value {
			env[key] = value
		}
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

func skipEnv(key string) bool {
	return ignoredEnv[key] || strings.HasPrefix(key, "BASH_FUNC_")
}

func profileDir() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, "nixshell", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return dir, nil
}

// The default installable "." would otherwise share one cache entry between
// all projects. Local flake refs are therefore keyed by their directory too.
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

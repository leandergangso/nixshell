package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

// One map holds both sides of a change: Prev restores the caller's shell, Next
// enters the dev shell. nil means the variable is unset on that side.
type envChange struct {
	Prev *string
	Next *string
}

type envDiff map[string]envChange

func devEnv(cacheID string, extra []string, base []string) (diff envDiff, err error) {
	root, err := cacheDir()
	if err != nil {
		return nil, err
	}
	profiles := filepath.Join(root, "profiles")
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		return nil, err
	}
	key, err := cacheKey(cacheID)
	if err != nil {
		return nil, err
	}
	stableProfile := filepath.Join(profiles, key)
	tmpProfile := filepath.Join(profiles, key+".tmp."+strconv.Itoa(os.Getpid()))

	workDir, err := os.MkdirTemp("", "nixshell-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if cleanErr := os.RemoveAll(workDir); err == nil {
			err = cleanErr
		}
		if cleanErr := os.RemoveAll(tmpProfile); err == nil {
			err = cleanErr
		}
	}()

	rcPath := filepath.Join(workDir, "env.sh")
	envPath := filepath.Join(workDir, "env0")

	args := append([]string{}, nixFeatures...)
	args = append(args, "print-dev-env", "--no-write-lock-file", "--profile", tmpProfile)
	args = append(args, extra...)
	if err := runNix(args, rcPath); err != nil {
		return nil, err
	}

	storePath, err := filepath.EvalSymlinks(tmpProfile)
	if err != nil {
		return nil, fmt.Errorf("nix did not create a profile at %s: %w", tmpProfile, err)
	}
	args = append([]string{}, nixFeatures...)
	args = append(args, "build", "--out-link", stableProfile, storePath)
	if err := runNix(args, os.DevNull); err != nil {
		return nil, err
	}

	// Source Nix's script with the bash selected by the dev environment so
	// shellHook runs there, not in whatever shell is on the caller's PATH.
	devShell, err := devShellBash(rcPath)
	if err != nil {
		return nil, err
	}
	if err := captureEnv(devShell, rcPath, envPath, base); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		return nil, fmt.Errorf("read captured environment: %w", err)
	}
	return buildDiff(base, parseEnv0(data)), nil
}

func runNix(args []string, stdoutPath string) error {
	cmd := exec.Command("nix", args...)
	cmd.Stderr = os.Stderr
	if stdoutPath == os.DevNull {
		cmd.Stdout = io.Discard
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("nix %s: %w", strings.Join(args, " "), err)
		}
		return nil
	}

	out, err := os.Create(stdoutPath)
	if err != nil {
		return err
	}
	cmd.Stdout = out
	runErr := cmd.Run()
	closeErr := out.Close()
	if runErr != nil {
		return fmt.Errorf("nix %s: %w", strings.Join(args, " "), errors.Join(runErr, closeErr))
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", stdoutPath, closeErr)
	}
	return nil
}

func captureEnv(shellPath, rcPath, envPath string, env []string) error {
	cmd := exec.Command(shellPath, "-c", `source "$1"; env -0 > "$2"`, "bash", rcPath, envPath)
	cmd.Env = env
	cmd.Stdin = nil
	// Generated shell code must stay on stdout in hook mode.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run shellHook: %w", err)
	}
	return nil
}

// devShellBash reads SHELL, BASH, then CONFIG_SHELL from Nix's script without
// executing it. The Nix script is bash-specific, so only bash paths are used.
func devShellBash(rcPath string) (string, error) {
	data, err := os.ReadFile(rcPath)
	if err != nil {
		return "", fmt.Errorf("read dev env: %w", err)
	}
	var candidateErrs []error
outer:
	for _, name := range []string{"SHELL", "BASH", "CONFIG_SHELL"} {
		prefixes := [][]byte{[]byte(name + "="), []byte("export " + name + "=")}
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			line = bytes.TrimSuffix(line, []byte{'\r'})
			for _, prefix := range prefixes {
				if !bytes.HasPrefix(line, prefix) {
					continue
				}
				value := line[len(prefix):]
				if len(value) > 0 && (value[0] == '\'' || value[0] == '"') {
					quote := value[0]
					end := bytes.IndexByte(value[1:], quote)
					if end < 0 {
						continue outer
					}
					value = value[1 : 1+end]
				}
				if !filepath.IsAbs(string(value)) {
					continue outer
				}
				resolved, err := filepath.EvalSymlinks(string(value))
				if err != nil {
					candidateErrs = append(candidateErrs, fmt.Errorf("resolve %s: %w", value, err))
					continue outer
				}
				if filepath.Base(resolved) == "bash" {
					return string(value), nil
				}
				continue outer
			}
		}
	}
	if len(candidateErrs) > 0 {
		return "", fmt.Errorf("nix dev env has no usable bash: %w", errors.Join(candidateErrs...))
	}
	return "", fmt.Errorf("nix dev env does not specify a bash shell")
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

func envList(data []byte) []string {
	var out []string
	for _, entry := range bytes.Split(data, []byte{0}) {
		if len(entry) > 0 {
			out = append(out, string(entry))
		}
	}
	return out
}

func writeEnv0(path string, env []string) error {
	return os.WriteFile(path, []byte(strings.Join(env, "\x00")+"\x00"), 0o600)
}

// Build temp dirs and shell-only state must not migrate into the user's shell.
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

func ptr(s string) *string { return &s }

func buildDiff(base []string, captured map[string]string) envDiff {
	current := envMap(base)
	diff := make(envDiff)
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
			diff[key] = envChange{Prev: ptr(old), Next: nil}
		case value != old:
			diff[key] = envChange{Prev: ptr(old), Next: ptr(value)}
		}
	}
	for key, value := range captured {
		if !keep(key) {
			continue
		}
		if _, exists := current[key]; !exists {
			diff[key] = envChange{Prev: nil, Next: ptr(value)}
		}
	}
	return diff
}

func patchEnv(env []string, diff envDiff, next bool) []string {
	out := envMap(env)
	for key, change := range diff {
		value := change.Prev
		if next {
			value = change.Next
		}
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

func cacheKey(installable string) (string, error) {
	ref := installable
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		ref = ref[:i]
	}
	key := installable
	if ref == "" || ref == "." || strings.HasPrefix(ref, "./") ||
		strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "path:") {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
		key = cwd + "#" + installable
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:16], nil
}

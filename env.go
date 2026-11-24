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

// envDiff is intentionally similar to direnv's exported diff: Prev holds the
// values before entry, Next holds the values to apply. A nil value means the
// variable was, or should be, unset.
type envDiff struct {
	Prev map[string]*string `json:"prev"`
	Next map[string]*string `json:"next"`
}

// devEnv resolves extra nix print-dev-env arguments into a diff against base.
// A GC root keeps the referenced store paths alive while the env is in use.
func devEnv(cacheID string, extra []string, base []string) (diff envDiff, err error) {
	dir, err := profileDir()
	if err != nil {
		return envDiff{}, err
	}
	key, err := cacheKey(cacheID)
	if err != nil {
		return envDiff{}, err
	}
	stableProfile := filepath.Join(dir, key)
	tmpProfile := filepath.Join(dir, key+".tmp."+strconv.Itoa(os.Getpid()))

	workDir, err := os.MkdirTemp("", "nixshell-")
	if err != nil {
		cleanErr := removeMatching(tmpProfile + "*")
		return envDiff{}, joinErr(err, cleanErr)
	}
	defer func() {
		err = joinErr(err, removeMatching(tmpProfile+"*"))
		err = joinErr(err, os.RemoveAll(workDir))
	}()

	rcPath := filepath.Join(workDir, "env.sh")
	envPath := filepath.Join(workDir, "env0")

	args := append([]string{}, nixFeatures...)
	args = append(args, "print-dev-env", "--no-write-lock-file", "--profile", tmpProfile)
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

	// Sourcing Nix's script runs shellHook in the bash selected by the dev
	// environment, not in whatever shell happens to be on the caller's PATH.
	// Only exported variables are kept; functions, arrays and shell-local
	// state cannot cross shells.
	devShell, err := devShellBash(rcPath)
	if err != nil {
		return envDiff{}, err
	}
	if err := captureEnv(devShell, rcPath, envPath, base); err != nil {
		return envDiff{}, err
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		return envDiff{}, fmt.Errorf("read captured environment: %w", err)
	}
	return buildDiff(base, parseEnv0(data)), nil
}

func joinErr(err, next error) error {
	if next == nil {
		return err
	}
	if err == nil {
		return next
	}
	return errors.Join(err, next)
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
		return fmt.Errorf("nix %s: %w", strings.Join(args, " "), joinErr(runErr, closeErr))
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", stdoutPath, closeErr)
	}
	return nil
}

func captureEnv(shellPath, rcPath, envPath string, env []string) error {
	cmd := exec.Command(shellPath, "-c", `source "$1"; env -0 > "$2"`, "bash", rcPath, envPath)
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

// devShellBash reads the bash selected by the dev environment from the
// generated script. Reading the script avoids sourcing it with the caller's
// shell, which would run shellHook in the wrong shell. SHELL is preferred;
// BASH and CONFIG_SHELL are fallbacks for older or unusual print-dev-env
// output. The Nix script is bash-specific, so non-bash values are skipped.
func devShellBash(rcPath string) (string, error) {
	data, err := os.ReadFile(rcPath)
	if err != nil {
		return "", fmt.Errorf("read dev env: %w", err)
	}
	var candidateErrs []error
	for _, name := range []string{"SHELL", "BASH", "CONFIG_SHELL"} {
		value, ok := scriptAssignment(data, name)
		if !ok || !filepath.IsAbs(value) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(value)
		if err != nil {
			candidateErrs = append(candidateErrs, fmt.Errorf("resolve %s: %w", value, err))
			continue
		}
		if filepath.Base(resolved) == "bash" {
			return value, nil
		}
	}
	if len(candidateErrs) > 0 {
		return "", fmt.Errorf("nix dev env has no usable bash: %w", errors.Join(candidateErrs...))
	}
	return "", fmt.Errorf("nix dev env does not specify a bash shell")
}

// scriptAssignment finds a top-level shell assignment without executing the
// script. Nix writes store paths in single quotes, but double quotes are
// accepted as well.
func scriptAssignment(data []byte, name string) (string, bool) {
	prefixes := [][]byte{[]byte(name + "="), []byte("export " + name + "=")}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		for _, prefix := range prefixes {
			if !bytes.HasPrefix(line, prefix) {
				continue
			}
			value := line[len(prefix):]
			if len(value) == 0 {
				return "", false
			}
			switch value[0] {
			case '\'', '"':
				quote := value[0]
				if end := bytes.IndexByte(value[1:], quote); end >= 0 {
					return string(value[1 : 1+end]), true
				}
			}
			return string(value), true
		}
	}
	return "", false
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

func cacheKey(installable string) (string, error) {
	key := installable
	if isLocalRef(installable) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
		key = cwd + "#" + installable
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:16], nil
}

func isLocalRef(installable string) bool {
	ref := installable
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		ref = ref[:i]
	}
	return ref == "" || ref == "." || strings.HasPrefix(ref, "./") ||
		strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "path:")
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() {
		if err != nil {
			if removeErr := os.Remove(name); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
		}
	}()

	if err := f.Chmod(perm); err != nil {
		return errors.Join(err, f.Close())
	}
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}

	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		return errors.Join(err, d.Close())
	}
	return d.Close()
}

func removeMatching(pattern string) error {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

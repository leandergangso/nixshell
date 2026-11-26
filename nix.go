package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var nixFeatures = []string{"--extra-experimental-features", "nix-command flakes"}

var ignoredEnv = map[string]bool{
	"HOME": true, "shellHook": true, "NIX_BUILD_TOP": true,
	"TMP": true, "TMPDIR": true, "TEMP": true, "TEMPDIR": true,
	"terminfo": true, "SHLVL": true, "_": true, "PWD": true, "OLDPWD": true,
	"ZDOTDIR": true, "BASH_ENV": true, "ENV": true,
}

func devEnv(ctx context.Context, devShell string, base []string) ([]string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(userCacheDir, "nixshell", "profiles")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, err
	}
	profilePath := filepath.Join(cacheDir, cacheKey(devShell))

	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer r.Close()

	args := append([]string{}, nixFeatures...)
	args = append(args, "develop", "--profile", profilePath, devShell,
		"--command", "bash", "-c", "env -0 >&3")

	cmd := exec.CommandContext(ctx, "nix", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{w}

	if err := cmd.Start(); err != nil {
		w.Close()
		return nil, err
	}
	w.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("nix develop: %w", err)
	}

	captured := parseEnv0(data)
	env := envMap(base)
	filtered := make(map[string]string, len(captured))
	for key, value := range captured {
		if ignoredEnv[key] || strings.HasPrefix(key, "BASH_FUNC_") {
			continue
		}
		filtered[key] = value
	}
	maps.Copy(env, filtered)

	env["NIXSHELL_PATH"] = env["PATH"]
	return envSlice(env), nil
}

func parseEnv0(data []byte) map[string]string {
	out := make(map[string]string)
	for entry := range bytes.SplitSeq(data, []byte{0}) {
		key, value, ok := bytes.Cut(entry, []byte{'='})
		if ok {
			out[string(key)] = string(value)
		}
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

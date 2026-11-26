package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	NixShellPath        = "NIXSHELL_PATH"
	NixShellRealZDOTDIR = "NIXSHELL_REAL_ZDOTDIR"
)

var nixFeatures = []string{"--extra-experimental-features", "nix-command flakes"}

var internalEnv = map[string]bool{
	NixShellPath: true, NixShellRealZDOTDIR: true,
}

// Keys from Nix that must not replace the caller's own state.
var ignoredEnv = map[string]bool{
	"HOME": true, "ZDOTDIR": true, "TMPDIR": true,
}

func devEnv(devShell string) (map[string]string, error) {
	raw := os.Environ()
	env := make(map[string]string, len(raw))
	cmdEnv := make([]string, 0, len(raw))
	for _, entry := range raw {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		env[key] = value
		cmdEnv = append(cmdEnv, entry)
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	cacheDir = filepath.Join(cacheDir, "nixshell", "profiles")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, err
	}

	args := append([]string{}, nixFeatures...)
	args = append(args, "develop", "--profile", filepath.Join(cacheDir, cacheKey(devShell)),
		devShell, "--command", "bash", "-c", `printf __NIXSHELL_ENV__; env -0`)

	var buf bytes.Buffer
	cmd := exec.Command("nix", args...)
	cmd.Env = cmdEnv
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("nix develop: %w", err)
	}

	data := buf.Bytes()
	marker := []byte("__NIXSHELL_ENV__")
	i := bytes.Index(data, marker)
	if i < 0 {
		return nil, fmt.Errorf("nix develop returned no environment")
	}
	data = data[i+len(marker):]

	for key, value := range parseEnv0(data) {
		if ignoredEnv[key] {
			continue
		}
		env[key] = value
	}
	if env["PATH"] == "" {
		return nil, fmt.Errorf("nix develop returned an empty PATH")
	}
	env[NixShellPath] = env["PATH"]
	return env, nil
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

func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

func cacheKey(devShell string) string {
	cwd, _ := os.Getwd()
	sum := sha256.Sum256([]byte(cwd + "#" + devShell))
	return hex.EncodeToString(sum[:])[:16]
}

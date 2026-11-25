package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var nixFeatures = []string{"--extra-experimental-features", "nix-command flakes"}

// NOTE: unsure if i want to keep this or not..
//var ignoredEnv = map[string]bool{
//	"HOME": true, "shellHook": true, "NIX_BUILD_TOP": true,
//	"TMP": true, "TMPDIR": true, "TEMP": true, "TEMPDIR": true,
//	"terminfo": true, "SHLVL": true, "_": true, "PWD": true, "OLDPWD": true,
//	"ZDOTDIR": true, "BASH_ENV": true, "ENV": true,
//}

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

	args := append([]string{}, nixFeatures...)
	args = append(args, "print-dev-env", "--json", "--profile", profilePath, devShell)

	cmd := exec.CommandContext(ctx, "nix", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("nix print-dev-env: %w", err)
	}

	var doc printDevEnv
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		return nil, fmt.Errorf("parse dev env json: %w", err)
	}

	shellPath := doc.stringVar("SHELL")
	if shellPath == "" {
		return nil, fmt.Errorf("print-dev-env did not define a shell")
	}

	// NOTE: i would like to avoid needing tmp files
	workDir, err := os.MkdirTemp("", "nixshell-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	scriptPath := filepath.Join(workDir, "env.sh")
	envPath := filepath.Join(workDir, "env0")
	if err := os.WriteFile(scriptPath, []byte(doc.script()), 0o600); err != nil {
		return nil, err
	}

	hookCmd := exec.CommandContext(ctx, shellPath, "-c", `source "$1" >&2; env -0 > "$2"`, "nixshell", scriptPath, envPath)
	hookCmd.Env = base
	hookCmd.Stdin = nil
	hookCmd.Stdout = os.Stderr
	hookCmd.Stderr = os.Stderr
	if err := hookCmd.Run(); err != nil {
		return nil, fmt.Errorf("run dev env shellHook: %w", err)
	}

	envData, err := os.ReadFile(envPath)
	if err != nil {
		return nil, fmt.Errorf("read captured environment: %w", err)
	}
	captured := parseEnv0(envData)

	env := envMap(base)
	filtered := make(map[string]string, len(captured))
	for key, value := range captured {
		//if ignoredEnv[key] || strings.HasPrefix(key, "BASH_FUNC_") {
		if strings.HasPrefix(key, "BASH_FUNC_") {
			// NOTE: why not this?
			continue
		}
		filtered[key] = value
	}
	maps.Copy(env, filtered)

	return envSlice(env), nil
}

func jsonString(raw jsontext.Value) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellQuotes(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, shellQuote(value))
	}
	return strings.Join(quoted, " ")
}

func shellAssoc(values map[string]string) string {
	quoted := make([]string, 0, len(values))
	for _, key := range sortedKeys(values) {
		quoted = append(quoted, "["+shellQuote(key)+"]="+shellQuote(values[key]))
	}
	return strings.Join(quoted, " ")
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

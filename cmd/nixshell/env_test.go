package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The filtering rules are the part most likely to break silently.
func TestApplyEnvDelta(t *testing.T) {
	base := []string{"HOME=/home/user", "PATH=/usr/bin", "KEEP=yes"}
	captured := map[string]string{
		"HOME":          "/homeless-shelter",
		"PATH":          "/nix/store/env/bin:/usr/bin",
		"FOO":           "bar",
		"shellHook":     "echo hi",
		"TMPDIR":        "/nix/store/leaked",
		"BASH_FUNC_x%%": "() { echo hi; }",
		"FROM_HOOK":     "yes",
	}
	values := make(map[string]string)
	for _, entry := range applyEnvDelta(base, captured) {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	want := map[string]string{
		"HOME":      "/home/user",
		"PATH":      "/nix/store/env/bin:/usr/bin",
		"KEEP":      "yes",
		"FOO":       "bar",
		"FROM_HOOK": "yes",
	}
	for key, value := range want {
		if values[key] != value {
			t.Errorf("%s = %q, want %q", key, values[key], value)
		}
	}
	for _, key := range []string{"shellHook", "TMPDIR", "BASH_FUNC_x%%"} {
		if _, exists := values[key]; exists {
			t.Errorf("%s should not be exported", key)
		}
	}
}

// A fake nix keeps this test independent of a Nix installation while still
// exercising profile creation, env capture and shellHook handling.
func TestDevEnvEndToEnd(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeNix := `#!/bin/sh
set -eu
if [ "$1" = "--extra-experimental-features" ]; then shift 2; fi
cmd="$1"; shift
case "$cmd" in
  print-dev-env)
    profile=""
    while [ $# -gt 0 ]; do
      if [ "$1" = "--profile" ]; then profile="$2"; shift 2; else shift; fi
    done
    mkdir -p "$profile.store"
    ln -s "$profile.store" "$profile"
    cat <<'EOF'
export FOO=bar
export PATH="/opt/bin:$PATH"
shellHook='export FROM_HOOK=yes'
eval "${shellHook:-}"
export TMPDIR=/nix/store/leaked
EOF
    ;;
  build)
    out=""
    while [ $# -gt 0 ]; do
      if [ "$1" = "--out-link" ]; then out="$2"; shift 2; else shift; fi
    done
    mkdir -p "$out.target"
    ln -s "$out.target" "$out"
    ;;
  *) echo "unexpected command: $cmd" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "nix"), []byte(fakeNix), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("HOME", root)
	baselineTmpDir := os.Getenv("TMPDIR")

	env, err := devEnv(".#test", false)
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string)
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for key, want := range map[string]string{"FOO": "bar", "FROM_HOOK": "yes", "HOME": root} {
		if values[key] != want {
			t.Errorf("%s = %q, want %q", key, values[key], want)
		}
	}
	if !strings.HasPrefix(values["PATH"], "/opt/bin:") {
		t.Errorf("PATH was not extended: %q", values["PATH"])
	}
	if _, exists := values["shellHook"]; exists {
		t.Error("shellHook should not be exported")
	}
	if values["TMPDIR"] != baselineTmpDir {
		t.Errorf("TMPDIR = %q, want caller's value %q", values["TMPDIR"], baselineTmpDir)
	}

	stableProfile := filepath.Join(root, "nixshell", "profiles", cacheKey(".#test"))
	if info, err := os.Lstat(stableProfile); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("no GC root at %s: %v", stableProfile, err)
	}
}

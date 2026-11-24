package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The diff is the part most likely to break silently.
func TestBuildDiff(t *testing.T) {
	base := []string{"HOME=/home/user", "PATH=/usr/bin", "KEEP=yes", "GONE=old"}
	captured := map[string]string{
		"HOME": "/homeless-shelter", "PATH": "/nix/store/env/bin:/usr/bin", "KEEP": "yes",
		"FOO": "bar", "shellHook": "echo hi", "TMPDIR": "/nix/store/leaked",
		"BASH_FUNC_x%%": "() { echo hi; }", "FROM_HOOK": "yes", "bad-key": "x",
	}
	diff := buildDiff(base, captured)

	wantNext := map[string]*string{
		"PATH": ptr("/nix/store/env/bin:/usr/bin"), "FOO": ptr("bar"),
		"FROM_HOOK": ptr("yes"), "GONE": nil,
	}
	if len(diff.Next) != len(wantNext) {
		t.Fatalf("Next = %#v, want %#v", diff.Next, wantNext)
	}
	for key, want := range wantNext {
		got, ok := diff.Next[key]
		if !ok || (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("Next[%s] = %v, want %v", key, got, want)
		}
	}
	if diff.Prev["GONE"] == nil || *diff.Prev["GONE"] != "old" {
		t.Errorf("Prev[GONE] = %v, want old", diff.Prev["GONE"])
	}
	if diff.Prev["FOO"] != nil {
		t.Errorf("Prev[FOO] = %v, want nil", diff.Prev["FOO"])
	}
	for _, key := range []string{"HOME", "KEEP", "shellHook", "TMPDIR", "BASH_FUNC_x%%", "bad-key"} {
		if _, ok := diff.Next[key]; ok {
			t.Errorf("unexpected Next[%s]", key)
		}
	}
}

// A fake nix keeps this test independent of a Nix installation.
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
unset GONE
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
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "nix"), []byte(fakeNix), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("HOME", root)
	t.Setenv("GONE", "old")
	baselineTmpDir := os.Getenv("TMPDIR")

	base := os.Environ()
	diff, err := devEnv(".#test", []string{".#test"}, base)
	if err != nil {
		t.Fatal(err)
	}
	values := envMap(patchEnv(base, diff.Next))
	for key, want := range map[string]string{"FOO": "bar", "FROM_HOOK": "yes", "HOME": root} {
		if values[key] != want {
			t.Errorf("%s = %q, want %q", key, values[key], want)
		}
	}
	if !strings.HasPrefix(values["PATH"], "/opt/bin:") {
		t.Errorf("PATH was not extended: %q", values["PATH"])
	}
	if values["TMPDIR"] != baselineTmpDir {
		t.Errorf("TMPDIR = %q, want caller's value %q", values["TMPDIR"], baselineTmpDir)
	}
	if _, ok := values["GONE"]; ok {
		t.Error("GONE was not unset")
	}
	stableProfile := filepath.Join(root, "nixshell", "profiles", cacheKey(".#test"))
	if info, err := os.Lstat(stableProfile); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("no GC root at %s: %v", stableProfile, err)
	}
}

func TestQuoteCode(t *testing.T) {
	value := "a'b\"c$d`e"
	for _, tc := range []struct {
		name, got, want string
	}{
		{"bash", quoteBash(value), `'a'\''b"c$d` + "`e'"},
		{"fish", quoteFish(value), "\"a'b\\\"c\\$d\\" + "`" + "e\""},
	} {
		if tc.got != tc.want {
			t.Errorf("%s quoting = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

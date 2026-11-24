package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllowRevoke(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	if isAllowed(project) {
		t.Fatal("project was trusted before allow")
	}
	dir, err := allowDir()
	if err != nil || dir != project {
		t.Fatalf("allowDir() = %q, %v", dir, err)
	}
	if !isAllowed(project) {
		t.Fatal("project was not trusted after allow")
	}
	data, err := os.ReadFile(filepath.Join(root, "nixshell", "allow"))
	if err != nil || !strings.Contains(string(data), project) {
		t.Fatalf("allow file = %q, %v", data, err)
	}
	if _, err := revokeDir(); err != nil {
		t.Fatal(err)
	}
	if isAllowed(project) {
		t.Fatal("project was still trusted after revoke")
	}
}

func TestCurrentProject(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "shell.nix"), []byte("{ }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	if _, found := currentProject(); found {
		t.Fatal("currentProject found an untrusted directory")
	}
	if _, err := allowDir(); err != nil {
		t.Fatal(err)
	}
	p, found := currentProject()
	if !found {
		t.Fatal("currentProject did not find an allowed directory")
	}
	want := []string{"--file", "shell.nix"}
	if strings.Join(p.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %#v, want %#v", p.Args, want)
	}
}

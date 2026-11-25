package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func shellCommand(ctx context.Context, args []string) error {
	if os.Getenv("IN_NIX_SHELL") != "" {
		return fmt.Errorf("already in a dev shell; exit it before starting another")
	}
	if len(args) > 1 {
		return fmt.Errorf("nixshell takes at most one devShell")
	}
	devShell := "."
	if len(args) == 1 {
		devShell = normalizeDevShell(args[0])
	}

	shell := currentShell()
	if shell == "" {
		return fmt.Errorf("cannot find a shell; set SHELL")
	}

	base := os.Environ()
	env, err := devEnv(ctx, devShell, base)
	if err != nil {
		return err
	}
	env = setEnv(env, "SHELL", shell)

	return syscall.Exec(shell, []string{shell}, env)
}

// A bare name means that devShell in the current flake. Anything containing
// '#' or '/' is already a usable Nix devShell reference.
func normalizeDevShell(arg string) string {
	if arg == "" || arg == "." {
		return "."
	}
	if strings.ContainsAny(arg, "#/") {
		return arg
	}
	return ".#" + arg
}

func currentShell() string {
	target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", os.Getppid()))
	if err == nil {
		clean := strings.TrimSuffix(target, " (deleted)")
		switch filepath.Base(clean) {
		case "bash", "zsh", "fish", "sh":
			return clean
		}
	}
	return os.Getenv("SHELL")
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

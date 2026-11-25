package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
)

func shellCommand(ctx context.Context, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("nixshell takes at most one devShell")
	}
	devShell := "."
	if len(args) == 1 {
		devShell = normalizeDevShell(args[0])
	}

	shell := currentShell()
	if shell == "" {
		return fmt.Errorf("cannot find a usable shell; set SHELL")
	}

	base := os.Environ() // NOTE: should get env from a pure shell instance rather than inherit current process?
	env, err := devEnv(ctx, devShell, base)
	if err != nil {
		return err
	}
	env = setEnv(env, "SHELL", shell)

	return syscall.Exec(shell, []string{shell}, env)
}

func currentShell() string {
	if target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", os.Getppid())); err == nil {
		return strings.TrimSuffix(target, " (deleted)")
	}
	return os.Getenv("SHELL")
}

func normalizeDevShell(arg string) string {
	if arg == "" || arg == "." {
		return "."
	}
	if strings.ContainsAny(arg, "#/") {
		return arg
	}
	return ".#" + arg
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

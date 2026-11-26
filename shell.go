package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func shellCommand(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("takes at most one devShell")
	}
	devShell := "."
	if len(args) == 1 {
		devShell = normalizeDevShell(args[0])
	}

	shell, err := currentShell()
	if err != nil {
		return err
	}
	env, err := devEnv(devShell)
	if err != nil {
		return err
	}

	env["SHELL"] = shell
	name := filepath.Base(shell)

	switch name {
	case "bash":
		shim, err := bashShim()
		if err != nil {
			return err
		}
		return syscall.Exec(shell, []string{name, "--noprofile", "--rcfile", shim, "-i"}, envSlice(env))

	case "zsh":
		if err := zshShim(env); err != nil {
			return err
		}
		return syscall.Exec(shell, []string{name, "-i"}, envSlice(env))

	case "fish":
		shim, err := fishShim()
		if err != nil {
			return err
		}
		return syscall.Exec(shell, []string{name, "-i", "--init-command", shim}, envSlice(env))

	default:
		for key := range internalEnv {
			delete(env, key)
		}
		return syscall.Exec(shell, []string{name}, envSlice(env))
	}
}

func currentShell() (string, error) {
	if target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", os.Getppid())); err == nil {
		return target, nil
	}

	shell := os.Getenv("SHELL")
	resolved, err := exec.LookPath(shell)
	if err != nil {
		return "", fmt.Errorf("SHELL=%q: %w", shell, err)
	}
	return resolved, nil
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

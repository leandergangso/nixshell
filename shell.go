package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const launchScript = `
while [ "$1" != -- ]; do
    key=${1%%=*}
    value=${1#*=}
    if [ -n "$value" ]; then
        export "$key=$value"
    else
        unset "$key"
    fi
    shift
done
shift
export NIXSHELL_PATH="$PATH"
exec "$@"
`

func shellCommand(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("takes at most one devShell")
	}
	devShell := "."
	if len(args) == 1 {
		devShell = normalizeDevShell(args[0])
	}

	nix, err := exec.LookPath("nix")
	if err != nil {
		return err
	}

	shell, err := currentShell()
	if err != nil {
		return err
	}
	name := filepath.Base(shell)

	envArgs := []string{
		"HOME=" + os.Getenv("HOME"),
		"TMPDIR=" + os.Getenv("TMPDIR"),
	}
	var shellArgs []string

	switch name {
	case "bash":
		shim, err := bashShim()
		if err != nil {
			return err
		}
		shellArgs = []string{"--noprofile", "--rcfile", shim, "-i"}

	case "zsh":
		dir, err := zshShim()
		if err != nil {
			return err
		}
		real := os.Getenv("ZDOTDIR")
		if real == "" || real == dir {
			real = os.Getenv("HOME")
		}
		envArgs = append(envArgs, "ZDOTDIR="+dir, "NIXSHELL_REAL_ZDOTDIR="+real)
		shellArgs = []string{"-i"}

	case "fish":
		shim, err := fishShim()
		if err != nil {
			return err
		}
		shellArgs = []string{"-i", "--init-command", shim}
	}

	handoff := append(envArgs, "--", shell)
	handoff = append(handoff, shellArgs...)
	// "nixshell" is only bash -c's $0. Handoff values must start at $1
	// so launchScript can consume them with shift.
	nixArgv := []string{
		"--extra-experimental-features", "nix-command flakes",
		"develop", devShell,
		"--command", "bash", "-c", launchScript, "nixshell",
	}
	nixArgv = append(nixArgv, handoff...)
	return syscall.Exec(nix, append([]string{"nix"}, nixArgv...), os.Environ())
}

func currentShell() (string, error) {
	if target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", os.Getppid())); err == nil {
		switch filepath.Base(target) {
		case "bash", "zsh", "fish":
			return target, nil
		}
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

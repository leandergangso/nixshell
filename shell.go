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
# nixshell handoff: KEY=VALUE
while [ "$1" != -- ]; do
    export "$1"
    shift
done
shift

# Nix appends the caller's PATH to the dev PATH. Keep the entries Nix added,
# restore the caller PATH for rc files, and let the shims prepend them again.
base=$NIXSHELL_BASE_PATH
if [ -n "$base" ]; then
    IFS=:
    read -r -a entries <<< "$PATH"
    dev=""
    for p in "${entries[@]}"; do
        [ -n "$p" ] || continue
        case ":$base:" in *":$p:"*) continue ;; esac
        case ":$dev:" in *":$p:"*) continue ;; esac
        dev=${dev:+$dev:}$p
    done
    export NIXSHELL_PATH="$dev"
    PATH=$base
else
    unset NIXSHELL_PATH
fi

unset NIXSHELL_BASE_PATH
exec "$@"
`

func shellCommand(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("expected at most one devShell")
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
		"NIXSHELL_BASE_PATH=" + os.Getenv("PATH"),
		"SHELL=" + shell,
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
		realDir := os.Getenv("ZDOTDIR")
		if realDir == "" || realDir == dir {
			realDir = os.Getenv("HOME")
		}
		envArgs = append(envArgs, "ZDOTDIR="+dir, "NIXSHELL_REAL_ZDOTDIR="+realDir)
		shellArgs = []string{"-i"}

	case "fish":
		shim, err := fishShim()
		if err != nil {
			return err
		}
		shellArgs = []string{"-i", "--init-command", shim}

	default:
		argv := []string{
			"--extra-experimental-features", "nix-command flakes",
			"develop", devShell,
			"--command", shell,
		}
		return syscall.Exec(nix, append([]string{"nix"}, argv...), os.Environ())
	}

	handoff := append(envArgs, "--", shell)
	handoff = append(handoff, shellArgs...)

	nixArgv := []string{
		"--extra-experimental-features", "nix-command flakes",
		"develop", devShell,
		"--command", "bash", "-c", launchScript, "nixshell",
	}
	nixArgv = append(nixArgv, handoff...)

	return syscall.Exec(nix, append([]string{"nix"}, nixArgv...), os.Environ())
}

func currentShell() (string, error) {
	target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", os.Getppid()))
	if err == nil {
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

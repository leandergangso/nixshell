package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "nixshell: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Internal entry point used by the generated hook code.
	if len(args) > 0 && args[0] == "hook-env" {
		if len(args) < 2 || len(args) > 4 {
			return fmt.Errorf("usage: nixshell hook-env <bash|zsh|fish> [pwd] [allow-mod]")
		}
		lastPWD := ""
		if len(args) > 2 {
			lastPWD = args[2]
		}
		var lastAllowMod int64
		if len(args) > 3 {
			lastAllowMod, _ = strconv.ParseInt(args[3], 10, 64)
		}
		return hookEnv(args[1], lastPWD, lastAllowMod)
	}

	fs := flag.NewFlagSet("nixshell", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage) }

	showVersion := fs.Bool("version", false, "print version")
	hook := fs.String("hook", "", "print shell hook setup: bash, zsh or fish")
	clean := fs.Bool("clean", false, "remove cached profiles and hook state")
	allow := fs.Bool("allow", false, "trust the current project for hook mode")
	deny := fs.Bool("deny", false, "stop trusting the current project")
	purge := fs.Bool("purge", false, "remove all trusted directories")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if *clean {
		if fs.NArg() != 0 {
			return fmt.Errorf("--clean takes no installable")
		}
		return cleanCache()
	}
	if *allow {
		if fs.NArg() != 0 {
			return fmt.Errorf("--allow takes no installable")
		}
		dir, err := allowDir()
		if err != nil {
			return err
		}
		fmt.Println("allowed", dir)
		return nil
	}
	if *deny {
		if fs.NArg() != 0 {
			return fmt.Errorf("--deny takes no installable")
		}
		dir, err := denyDir()
		if err != nil {
			return err
		}
		fmt.Println("denied", dir)
		return nil
	}
	if *purge {
		if fs.NArg() != 0 {
			return fmt.Errorf("--purge takes no installable")
		}
		if err := purgeApprovals(); err != nil {
			return err
		}
		fmt.Println("purged approvals")
		return nil
	}
	if *hook != "" {
		return printHook(*hook)
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("expected at most one installable")
	}
	if inDevShell() {
		return fmt.Errorf("already in a dev shell; use 'exec nixshell ...' to replace this shell instead of nesting")
	}

	installable := normalizeInstallable(fs.Arg(0))
	shell := currentShell()
	if shell == "" {
		return fmt.Errorf("cannot find a shell; set SHELL")
	}

	base := os.Environ()
	diff, err := devEnv(installable, []string{installable}, base)
	if err != nil {
		return err
	}

	env := setEnv(patchEnv(base, diff.Next), "SHELL", shell)
	if err := syscall.Exec(shell, []string{shell}, env); err != nil {
		return fmt.Errorf("exec %s: %w", shell, err)
	}

	return nil
}

// A bare name means that devShell in the current flake.
func normalizeInstallable(arg string) string {
	if arg == "" {
		return "."
	}
	if strings.ContainsAny(arg, "#/") {
		return arg
	}
	return ".#" + arg
}

// The invoking shell is normally the right one. Fall back to SHELL; if that
// is missing, report it instead of guessing further.
func currentShell() string {
	target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", os.Getppid()))
	if err == nil {
		clean := strings.TrimSuffix(target, " (deleted)")
		name := filepath.Base(clean)
		if name == "bash" || name == "zsh" || name == "fish" || name == "sh" {
			return clean
		}
	}
	return os.Getenv("SHELL")
}

// inDevShell detects both nixshell's hook state and a plain Nix dev shell so
// a manual nixshell call does not silently create another nested shell.
func inDevShell() bool {
	return os.Getenv(stateVar) != "" || os.Getenv("IN_NIX_SHELL") != ""
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

const usage = `Enter a Nix dev shell using your own shell.

Usage:
  nixshell [flags] [installable]

Flags must come before the installable. Installable defaults to .

Examples:
  nixshell                       # .#default
  nixshell ci                    # .#ci
  nixshell .#ci
  nixshell github:org/repo#dev

Flags:
      --allow         trust the current project for hook mode
      --clean         drop cached profiles and hook state
      --deny          stop trusting the current project
      --hook string   print shell hook setup: bash, zsh or fish
      --purge         remove all trusted directories
      --version       print version
      --help          show this help

Hook setup:
  bash: eval "$(nixshell --hook bash)"
  zsh:  eval "$(nixshell --hook zsh)"
  fish: nixshell --hook fish | source
`

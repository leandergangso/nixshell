package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "nixshell:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "hook-env" {
		if len(args) != 2 {
			return fmt.Errorf("usage: nixshell hook-env <bash|zsh|fish>")
		}
		return hookEnv(args[1])
	}

	fs := flag.NewFlagSet("nixshell", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `Enter a Nix dev shell using your own shell.

Usage:
  nixshell [flags] [installable]

Examples:
  nixshell                       # .#default
  nixshell ci                    # .#ci
  nixshell .#ci
  nixshell github:org/repo#dev

Flags:
`)
		fs.VisitAll(func(f *flag.Flag) {
			name := "  --" + f.Name
			if f.Name == "hook" {
				name += " string"
			}
			fmt.Fprintf(fs.Output(), "%-20s %s\n", name, f.Usage)
		})
		fmt.Fprint(fs.Output(), `
Hook setup:
  bash: eval "$(nixshell --hook bash)"
  zsh:  eval "$(nixshell --hook zsh)"
  fish: nixshell --hook fish | source
`)
	}

	showHelp := fs.Bool("help", false, "show this help")
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

	actions := 0
	for _, set := range []bool{*showHelp, *showVersion, *hook != "", *clean, *allow, *deny, *purge} {
		if set {
			actions++
		}
	}
	if actions > 1 {
		return fmt.Errorf("only one action flag may be used")
	}

	if *showHelp {
		fs.Usage()
		return nil
	}
	if *showVersion {
		if fs.NArg() != 0 {
			return fmt.Errorf("--version takes no installable")
		}
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
		if fs.NArg() != 0 {
			return fmt.Errorf("--hook takes no installable")
		}
		return printHook(*hook)
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("expected at most one installable")
	}
	if inDevShell() {
		return fmt.Errorf("already in a dev shell; use 'exec nixshell ...' to replace this shell instead of nesting")
	}

	installable := fs.Arg(0)
	if installable == "" {
		installable = "."
	} else if !strings.ContainsAny(installable, "#/") {
		installable = ".#" + installable
	}
	shell := currentShell()
	if shell == "" {
		return fmt.Errorf("cannot find a shell; set SHELL")
	}

	base := os.Environ()
	diff, err := devEnv(installable, []string{installable}, base)
	if err != nil {
		return err
	}
	env := setEnv(patchEnv(base, diff, true), "SHELL", shell)
	if err := syscall.Exec(shell, []string{shell}, env); err != nil {
		return fmt.Errorf("exec %s: %w", shell, err)
	}
	return nil
}

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

func inDevShell() bool {
	return os.Getenv(activeVar) != "" || os.Getenv("IN_NIX_SHELL") != ""
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

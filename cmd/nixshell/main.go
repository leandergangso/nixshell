// Command nixshell enters a Nix development shell using the user's own shell.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "nixshell: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Internal entry point used by the generated hook code.
	if len(args) > 0 && args[0] == "hook-env" {
		if len(args) != 2 {
			return fmt.Errorf("usage: nixshell hook-env <shell>")
		}
		return hookEnv(args[1])
	}

	fs := flag.NewFlagSet("nixshell", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	showVersion := fs.Bool("version", false, "print version")
	hook := fs.String("hook", "", "print shell hook setup: bash, zsh or fish")
	clean := fs.Bool("clean", false, "remove cached profiles and hook state")

	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "Usage: nixshell [options] [installable]\n\n"+
			"Enter a Nix dev shell with your own shell. Installable defaults to .\n\nOptions:\n")
		fs.PrintDefaults()
		fmt.Fprint(fs.Output(), "\nAdd '-hook bash', '-hook zsh' or '-hook fish' output to your shell rc.\n"+
			"Use '-clean' to drop cached profiles and hook state.\n")
	}

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
			return fmt.Errorf("-clean takes no installable")
		}
		return cleanCache()
	}
	if *hook != "" {
		return printHook(*hook)
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("expected at most one installable")
	}

	installable := normalizeInstallable(fs.Arg(0))
	shell := currentShell()
	if shell == "" {
		return fmt.Errorf("cannot find a shell; set SHELL")
	}

	base := os.Environ()
	changes, err := devEnv(installable, base)
	if err != nil {
		return err
	}

	env := setEnv(applyChanges(base, changes), "SHELL", shell)
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
	if _, err := os.Stat(arg); err == nil {
		return arg
	}
	return ".#" + arg
}

// The invoking shell is normally the right one. Fall back to SHELL; if that
// is missing, report it instead of guessing further.
func currentShell() string {
	target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", os.Getppid()))
	if err == nil {
		name := filepath.Base(strings.TrimSuffix(target, " (deleted)"))
		if name == "bash" || name == "zsh" || name == "fish" || name == "sh" {
			return target
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

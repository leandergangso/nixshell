// Command nixshell enters a Nix development shell using the user's own shell.
package main

import (
	"flag"
	"fmt"
	"os"
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
	fs := flag.NewFlagSet("nixshell", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	shellFlag := fs.String("shell", "", "shell to start (default: current shell, then $SHELL, then passwd entry)")
	versionFlag := fs.Bool("version", false, "print version and exit")
	verbose := fs.Bool("v", false, "log each step to stderr")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `Usage: nixshell [options] [installable]

Enter a Nix development shell using your own shell instead of nixpkgs bash.

Options:
`)
		fs.PrintDefaults()
		fmt.Fprint(fs.Output(), `
Examples:
  nixshell                       # .#default (the flake in the current directory)
  nixshell ci                    # shorthand for .#ci
  nixshell .#ci                  # devShells.<system>.ci in the current flake
  nixshell github:org/repo#dev   # any flake installable
  nixshell --shell /bin/bash .#dev

The installable is passed straight to "nix print-dev-env", so all Nix flake
reference syntax works. Put options before the installable.
`)
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *versionFlag {
		fmt.Println(version)
		return nil
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("expected at most one installable, got %d", fs.NArg())
	}

	installable := normalizeInstallable(fs.Arg(0))
	shell, err := chooseShell(*shellFlag)
	if err != nil {
		return err
	}
	if *verbose {
		fmt.Fprintf(os.Stderr, "nixshell: installable: %s\n", installable)
		fmt.Fprintf(os.Stderr, "nixshell: shell: %s\n", shell)
	}

	env, err := devEnv(installable, *verbose)
	if err != nil {
		return err
	}
	env = setEnv(env, "SHELL", shell)

	if *verbose {
		fmt.Fprintf(os.Stderr, "nixshell: entering %s\n", shell)
	}
	if err := syscall.Exec(shell, []string{shell}, env); err != nil {
		return fmt.Errorf("exec %s: %w", shell, err)
	}
	return nil
}

// A bare name is shorthand for that devShell in the current flake. Anything
// that looks like a flake reference or a file path is passed through untouched.
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

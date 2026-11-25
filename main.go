package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "nixshell:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx := context.Background()

	fs := flag.NewFlagSet("nixshell", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = usage(fs)

	_ = fs.Bool("help", false, "show help usage")
	showVersion := fs.Bool("version", false, "show version")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *showVersion {
		fmt.Println("nixshell: " + version)
		return nil
	}

	return shellCommand(ctx, fs.Args())
}

func usage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprint(fs.Output(), `Enter a Nix devShell using your own shell.

Usage:
  nixshell [flags] [devShell]

With no devShell, nixshell uses the default devShell in the current flake.

Examples:
  nixshell
  nixshell ci
  nixshell .#ci
  nixshell github:org/repo#dev

Flags:
`)
		fs.VisitAll(func(f *flag.Flag) {
			name := "-" + f.Name
			if f.DefValue != "" && f.DefValue != "false" {
				fmt.Fprintf(fs.Output(), "  %-10s\t%s (%s)\n", name, f.Usage, f.DefValue)
			} else {
				fmt.Fprintf(fs.Output(), "  %-10s\t%s\n", name, f.Usage)
			}
		})
	}
}

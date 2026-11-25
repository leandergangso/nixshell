package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fs := flag.NewFlagSet("nixshell", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = usage(fs)

	_ = fs.Bool("help", false, "help for nixshell")
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

	if len(args) == 0 {
		fs.Usage()
		return nil
	}

	switch args[0] {
	case "hook":
		return nil
	case "allow":
		return nil
	case "deny":
		return nil
	case "clean":
		return nil
	case "prune":
		return nil
	case "shell":
		return shellCommand(ctx, args[1:])
	default:
		return fmt.Errorf("invalid command: %s", args[0])
	}
}

func usage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprint(fs.Output(), `Enter a NIX devShell using your own shell.

Usage:
  nixshell [flags] [command]

Examples:
  nixshell shell                       # .#default
  nixshell shell ci                    # .#ci
  nixshell shell .#ci
  nixshell shell github:org/repo#dev

Commands:
  shell		enter nix devShell with current shell
  hook		print shell hook setup: bash, zsh or fish
  allow		trust the current project for hook mode
  deny		stop trusting the current project
  ...		remove all trusted directories (use deny -all instead?)
  clean		remove cached profiles and hook state
  prune		remove all system files

Global flags:
`)
		fs.VisitAll(func(f *flag.Flag) {
			name := "-" + f.Name
			if f.DefValue != "" && f.DefValue != "false" {
				fmt.Fprintf(fs.Output(), "  %-10s\t%s (%s)\n", name, f.Usage, f.DefValue)
			} else {
				fmt.Fprintf(fs.Output(), "  %-10s\t%s\n", name, f.Usage)
			}
		})
		fmt.Fprint(fs.Output(), `
Hook setup:
  bash: eval "$(nixshell --hook bash)"
  zsh:  eval "$(nixshell --hook zsh)"
  fish: nixshell --hook fish | source
`)
	}
}

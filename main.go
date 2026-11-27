package main

import (
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
	if len(args) == 0 {
		return shellCommand(nil)
	}

	switch args[0] {
	case "help":
		usage()
		return nil

	case "version":
		fmt.Println("nixshell: " + version)
		return nil

	case "shell":
		return shellCommand(args[1:])

	default:
		return shellCommand(args)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Enter a Nix devShell using your own shell.

Usage:
  nixshell [devShell]
  nixshell [command]

Commands:
  shell		Enter devShell
  version	Print version
  help		Show this usage
`)
}

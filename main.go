package main

import (
	"fmt"
	"os"
	"runtime/debug"
)

var version = "dev"

const usageText = `nixshell: enter a Nix devShell in your own shell

usage: nixshell [devShell]
       nixshell -v --version
       nixshell -h --help
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(os.Stderr, usageText)
		return nil
	}
	if args[0] == "-v" || args[0] == "--version" {
		fmt.Printf("nixshell: %s\n", displayVersion())
		return nil
	}
	return shellCommand(args)
}

func displayVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

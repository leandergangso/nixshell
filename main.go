package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		if writeErr := writeStderr("nixshell: " + err.Error() + "\n"); writeErr != nil {
			os.Exit(1)
		}
		os.Exit(1)
	}
}

func writeStdout(s string) error {
	_, err := io.WriteString(os.Stdout, s)
	return err
}

func writeStderr(s string) error {
	_, err := io.WriteString(os.Stderr, s)
	return err
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
			value, err := parseAllowMod(args[3])
			if err != nil {
				return err
			}
			lastAllowMod = value
		}
		return hookEnv(args[1], lastPWD, lastAllowMod)
	}

	var parseOutput strings.Builder
	fs := flag.NewFlagSet("nixshell", flag.ContinueOnError)
	fs.SetOutput(&parseOutput)
	fs.Usage = func() {}

	showVersion := fs.Bool("version", false, "")
	hook := fs.String("hook", "", "")
	clean := fs.Bool("clean", false, "")
	allow := fs.Bool("allow", false, "")
	deny := fs.Bool("deny", false, "")
	purge := fs.Bool("purge", false, "")

	parseErr := fs.Parse(args)
	if text := parseOutput.String(); text != "" {
		if err := writeStderr(text); err != nil {
			return fmt.Errorf("write flag output: %w", err)
		}
	}
	if parseErr != nil {
		if parseErr == flag.ErrHelp {
			if err := writeStderr(usage); err != nil {
				return fmt.Errorf("write help: %w", err)
			}
			return nil
		}
		return parseErr
	}

	var activeActions []string
	hookAction := false
	fs.Visit(func(f *flag.Flag) {
		active := true
		switch f.Name {
		case "version", "clean", "allow", "deny", "purge":
			active = f.Value.String() == "true"
		case "hook":
			hookAction = true
		default:
			return
		}
		if active {
			activeActions = append(activeActions, "--"+f.Name)
		}
	})
	if len(activeActions) > 1 {
		return fmt.Errorf("only one action flag may be used: %s", strings.Join(activeActions, ", "))
	}

	if *showVersion {
		if fs.NArg() != 0 {
			return fmt.Errorf("--version takes no installable")
		}
		if err := writeStdout(version + "\n"); err != nil {
			return fmt.Errorf("print version: %w", err)
		}
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
		if err := writeStdout("allowed " + dir + "\n"); err != nil {
			return fmt.Errorf("print allowed directory: %w", err)
		}
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
		if err := writeStdout("denied " + dir + "\n"); err != nil {
			return fmt.Errorf("print denied directory: %w", err)
		}
		return nil
	}
	if *purge {
		if fs.NArg() != 0 {
			return fmt.Errorf("--purge takes no installable")
		}
		if err := purgeApprovals(); err != nil {
			return err
		}
		if err := writeStdout("purged approvals\n"); err != nil {
			return fmt.Errorf("print purge result: %w", err)
		}
		return nil
	}
	if hookAction {
		if fs.NArg() != 0 {
			return fmt.Errorf("--hook takes no installable")
		}
		if *hook == "" {
			return fmt.Errorf("--hook requires bash, zsh or fish")
		}
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

func parseAllowMod(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse allow-mod %q: %w", s, err)
	}
	return value, nil
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

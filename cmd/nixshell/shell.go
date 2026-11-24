package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Only these process names are considered a "current shell". Anything else
// (nix, sudo, an IDE, ...) falls through to $SHELL and then to /etc/passwd.
var knownShells = map[string]bool{
	"bash": true, "dash": true, "sh": true, "zsh": true, "fish": true,
	"ksh": true, "ksh93": true, "mksh": true, "csh": true, "tcsh": true,
	"elvish": true, "nu": true, "xonsh": true,
}

func chooseShell(requested string) (string, error) {
	if requested != "" {
		return resolveExecutable(requested)
	}
	if path, ok := parentShell(); ok {
		return path, nil
	}
	if path := os.Getenv("SHELL"); path != "" {
		if resolved, err := resolveExecutable(path); err == nil {
			return resolved, nil
		}
	}
	if path, ok := passwdShell(); ok {
		return path, nil
	}
	return "", fmt.Errorf("could not determine shell; use --shell")
}

// The parent process is normally the interactive shell the command was typed
// into. /proc/<pid>/exe is used instead of $SHELL because $SHELL can be stale.
func parentShell() (string, bool) {
	target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", os.Getppid()))
	if err != nil {
		return "", false
	}
	target = strings.TrimSuffix(target, " (deleted)")
	if !knownShells[filepath.Base(target)] {
		return "", false
	}
	if err := checkExecutable(target); err != nil {
		return "", false
	}
	return target, true
}

func passwdShell() (string, bool) {
	file, err := os.Open("/etc/passwd")
	if err != nil {
		return "", false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) != 7 {
			continue
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil || uid != os.Getuid() {
			continue
		}
		if fields[6] != "" {
			if path, err := resolveExecutable(fields[6]); err == nil {
				return path, true
			}
		}
	}
	if scanner.Err() != nil {
		return "", false
	}
	return "", false
}

func resolveExecutable(path string) (string, error) {
	if !strings.ContainsRune(path, os.PathSeparator) {
		return exec.LookPath(path)
	}
	if err := checkExecutable(path); err != nil {
		return "", err
	}
	return path, nil
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("shell %q: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("shell %q is not an executable file", path)
	}
	return nil
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

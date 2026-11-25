package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// One env var marks the active cache dir; the cache holds the shell snippets
// and the environment to restore. No diff is serialized into the environment.
const activeVar = "NIXSHELL_ACTIVE"

func printHook(shell string) error {
	switch shell {
	case "bash":
		fmt.Print(`__nixshell_hook() { eval "$(nixshell hook-env bash)"; }
case ";${PROMPT_COMMAND-};" in
  *";__nixshell_hook;"*) ;;
  *) PROMPT_COMMAND="__nixshell_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}" ;;
esac
`)
	case "zsh":
		fmt.Print(`__nixshell_hook() { eval "$(nixshell hook-env zsh)"; }
autoload -Uz add-zsh-hook
add-zsh-hook -d precmd __nixshell_hook 2>/dev/null || true
precmd_functions=(__nixshell_hook ${precmd_functions[@]})
`)
	case "fish":
		fmt.Print(`function __nixshell_hook --on-variable PWD --on-event fish_prompt
    nixshell hook-env fish | source
end
`)
	default:
		return fmt.Errorf("hook shell must be bash, zsh or fish")
	}
	return nil
}

type hookProject struct {
	Root      string
	CacheID   string
	Args      []string
	Signature string
}

func (p hookProject) key() string { return p.Root + "#" + p.CacheID }

func hookEnv(shell string) error {
	if shell != "bash" && shell != "zsh" && shell != "fish" {
		return fmt.Errorf("hook shell must be bash, zsh or fish")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	active := os.Getenv(activeVar)

	project, found, err := findAllowedProject(cwd)
	if err != nil {
		return err
	}
	if !found {
		code := ""
		if active != "" {
			leave := filepath.Join(active, shell+".leave")
			if _, err := os.Stat(leave); err != nil {
				return fmt.Errorf("read active shell state: %w", err)
			}
			code += sourceCode(shell, leave)
		}
		code += unsetCode(shell, activeVar)
		fmt.Print(code + "\n")
		return nil
	}

	dir, err := hookCacheDir(project)
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(filepath.Join(dir, "signature"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if active == dir && string(sig) == project.Signature {
		enter := filepath.Join(dir, shell+".enter")
		if _, err := os.Stat(enter); err != nil {
			return fmt.Errorf("read cached shell state: %w", err)
		}
		fmt.Print(sourceCode(shell, enter) + exportCode(shell, activeVar, dir) + "\n\n")
		return nil
	}

	oldLeave := ""
	if active != "" {
		leave := filepath.Join(active, shell+".leave")
		if _, err := os.Stat(leave); err != nil {
			return fmt.Errorf("read active shell state: %w", err)
		}
		oldLeave = sourceCode(shell, leave)
	}

	base := os.Environ()
	if active != "" {
		data, err := os.ReadFile(filepath.Join(active, "base0"))
		if err != nil {
			return fmt.Errorf("read active base env: %w", err)
		}
		base = envList(data)
	}
	diff, err := devEnv(project.CacheID, project.Args, base)
	if err != nil {
		return err
	}

	if err := writeEnv0(filepath.Join(dir, "base0"), base); err != nil {
		return fmt.Errorf("write base env: %w", err)
	}
	files := map[string]string{
		"signature":      project.Signature,
		shell + ".enter": applyCode(shell, diff, true),
		shell + ".leave": applyCode(shell, diff, false),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return fmt.Errorf("write cache %s: %w", name, err)
		}
	}

	enter := filepath.Join(dir, shell+".enter")
	fmt.Print(oldLeave + sourceCode(shell, enter) + exportCode(shell, activeVar, dir) + "\n\n")
	return nil
}

func nixFiles(dir string) ([]string, bool, error) {
	pattern := filepath.Join(dir, "*.nix")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, false, fmt.Errorf("glob %s: %w", pattern, err)
	}
	if len(files) == 0 {
		return nil, false, nil
	}
	sort.Strings(files)
	return files, true, nil
}

func projectFrom(root string, files []string) (hookProject, error) {
	names := make(map[string]bool, len(files))
	for _, path := range files {
		names[filepath.Base(path)] = true
	}

	var cacheID string
	var args []string
	switch {
	case names["flake.nix"]:
		args = []string{"path:" + root}
		cacheID = args[0]
	case names["shell.nix"]:
		args = []string{"--file", filepath.Join(root, "shell.nix")}
		cacheID = root + "#shell.nix"
	case names["default.nix"]:
		args = []string{"--file", filepath.Join(root, "default.nix")}
		cacheID = root + "#default.nix"
	default:
		entry := filepath.Base(files[0])
		args = []string{"--file", filepath.Join(root, entry)}
		cacheID = root + "#" + entry
	}
	sig, err := signature(root, files)
	if err != nil {
		return hookProject{}, err
	}
	return hookProject{Root: root, CacheID: cacheID, Args: args, Signature: sig}, nil
}

func findAllowedProject(cwd string) (hookProject, bool, error) {
	allowed, err := readAllowList()
	if err != nil {
		return hookProject{}, false, err
	}

	stale := false
	for dir := filepath.Clean(cwd); ; {
		if allowed[dir] {
			files, ok, err := nixFiles(dir)
			if err != nil {
				return hookProject{}, false, err
			}
			if ok {
				project, err := projectFrom(dir, files)
				if err != nil {
					return hookProject{}, false, err
				}
				if stale {
					if err := saveAllowList(allowed); err != nil {
						return hookProject{}, false, err
					}
				}
				return project, true, nil
			}
			delete(allowed, dir)
			stale = true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if stale {
		if err := saveAllowList(allowed); err != nil {
			return hookProject{}, false, err
		}
	}
	return hookProject{}, false, nil
}

func signature(root string, files []string) (string, error) {
	h := sha256.New()
	if _, err := fmt.Fprintf(h, "%s\x00", root); err != nil {
		return "", err
	}
	paths := append([]string{}, files...)
	lock := filepath.Join(root, "flake.lock")
	if _, err := os.Stat(lock); err == nil {
		paths = append(paths, lock)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat %s: %w", lock, err)
	}
	sort.Strings(paths)
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		if _, err := fmt.Fprintf(h, "%s\x00", rel); err != nil {
			return "", err
		}
		if _, err := h.Write(data); err != nil {
			return "", err
		}
		if _, err := h.Write([]byte{0}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hookCacheDir(project hookProject) (string, error) {
	root, err := cacheDir()
	if err != nil {
		return "", err
	}
	hooks := filepath.Join(root, "hook")
	if err := os.MkdirAll(hooks, 0o700); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(project.key()))
	dir := filepath.Join(hooks, hex.EncodeToString(sum[:])[:16])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func applyCode(shell string, diff envDiff, next bool) string {
	keys := make([]string, 0, len(diff))
	for key := range diff {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if !validEnvKey(key) {
			continue
		}
		change := diff[key]
		value := change.Prev
		if next {
			value = change.Next
		}
		if value == nil {
			parts = append(parts, unsetCode(shell, key))
		} else {
			parts = append(parts, exportCode(shell, key, *value))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n") + "\n"
}

func sourceCode(shell, path string) string {
	if shell == "fish" {
		return "source " + quoteFish(path) + "\n"
	}
	return "source " + quoteBash(path) + "\n"
}

func exportCode(shell, key, value string) string {
	if shell == "fish" {
		return "set -gx " + key + " " + quoteFish(value)
	}
	return "export " + key + "=" + quoteBash(value)
}

func unsetCode(shell, key string) string {
	if shell == "fish" {
		return "set -e " + key
	}
	return "unset " + key
}

func quoteBash(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func quoteFish(value string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`")
	return `"` + r.Replace(value) + `"`
}

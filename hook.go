package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	stateVar    = "NIXSHELL_DIFF"
	pwdVar      = "NIXSHELL_PWD"
	allowModVar = "NIXSHELL_ALLOW"
)

// printHook emits the few lines needed in a shell rc file. The hook itself
// calls back into nixshell on every prompt; nix is only run when files change.
func printHook(shell string) error {
	var code string
	switch shell {
	case "bash":
		code = `__nixshell_hook() { eval "$(nixshell hook-env bash "${NIXSHELL_PWD-}" "${NIXSHELL_ALLOW-}")"; }
case ";${PROMPT_COMMAND-};" in
  *";__nixshell_hook;"*) ;;
  *) PROMPT_COMMAND="__nixshell_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}" ;;
esac
`

	case "zsh":
		// Prepend so prompt plugins (e.g. oh-my-posh) see the new env.
		code = `__nixshell_hook() { eval "$(nixshell hook-env zsh "${NIXSHELL_PWD-}" "${NIXSHELL_ALLOW-}")"; }
autoload -Uz add-zsh-hook
add-zsh-hook -d precmd __nixshell_hook 2>/dev/null || true
precmd_functions=(__nixshell_hook ${precmd_functions[@]})
`

	case "fish":
		// PWD fires before fish_prompt handlers, so prompt plugins see the new env.
		code = `function __nixshell_hook --on-variable PWD --on-event fish_prompt
    nixshell hook-env fish $NIXSHELL_PWD $NIXSHELL_ALLOW | source
end
`

	default:
		return fmt.Errorf("hook shell must be bash, zsh or fish")
	}
	if err := writeStdout(code); err != nil {
		return fmt.Errorf("write hook: %w", err)
	}
	return nil
}

type envState struct {
	Project string             `json:"project"`
	Root    string             `json:"root,omitempty"`
	Prev    map[string]*string `json:"diff"`
}

type hookCache struct {
	Signature string  `json:"signature"`
	Diff      envDiff `json:"diff"`
}

type hookProject struct {
	Root      string
	CacheID   string
	Args      []string
	Signature string
}

func (p hookProject) key() string { return p.Root + "#" + p.CacheID }

// hookEnv prints shell code that applies the dev env for the current directory
// (or restores the previous values when leaving it). lastPWD and lastAllowMod
// let us avoid walking parent directories on every prompt.
func hookEnv(shell, lastPWD string, lastAllowMod int64) error {
	if shell != "bash" && shell != "zsh" && shell != "fish" {
		return fmt.Errorf("hook shell must be bash, zsh or fish")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	state, err := decodeState(os.Getenv(stateVar))
	if err != nil {
		return err
	}
	allowMod, err := allowModTime()
	if err != nil {
		return err
	}

	var project hookProject
	var found bool
	if state.Root != "" && lastPWD == cwd && lastAllowMod == allowMod {
		// Same directory and same trust list: reuse the active root. File
		// changes are still caught by the signature check below.
		project, found, err = projectFromAllowedRoot(state.Root)
	} else {
		// A new .nix file can appear without a directory or allow-list change.
		project, found, err = findAllowedProject(cwd)
	}
	if err != nil {
		return err
	}
	allowMod, err = allowModTime()
	if err != nil {
		return err
	}

	if !found {
		code := ""
		if len(state.Prev) != 0 {
			code += applyCode(shell, state.Prev)
		}
		code += rememberCode(shell, cwd, allowMod)
		code += unsetCode(shell, stateVar)
		return writeHookCode(code)
	}

	cachePath, err := hookCachePath(project.key())
	if err != nil {
		return err
	}
	cached, err := loadCache(cachePath)
	if err != nil {
		return err
	}
	if state.Project == project.key() && cached != nil &&
		cached.Signature == project.Signature && cached.Diff.Next != nil {
		state.Root = project.Root
		encoded, err := encodeState(state)
		if err != nil {
			return err
		}
		// Re-assert cached values and state in case the rc file changed them.
		code := applyCode(shell, cached.Diff.Next) +
			exportCode(shell, stateVar, encoded) + "\n" +
			rememberCode(shell, cwd, allowMod)
		return writeHookCode(code)
	}

	base := os.Environ()
	restored := patchEnv(base, state.Prev)
	diff, err := devEnv(project.CacheID, project.Args, restored)
	if err != nil {
		return err
	}
	if err := saveCache(cachePath, hookCache{Signature: project.Signature, Diff: diff}); err != nil {
		return err
	}

	newState := envState{Project: project.key(), Root: project.Root, Prev: diff.Prev}
	encoded, err := encodeState(newState)
	if err != nil {
		return err
	}
	code := applyCode(shell, state.Prev) +
		applyCode(shell, diff.Next) +
		exportCode(shell, stateVar, encoded) + "\n" +
		rememberCode(shell, cwd, allowMod)
	return writeHookCode(code)
}

func writeHookCode(code string) error {
	if err := writeStdout(code + "\n"); err != nil {
		return fmt.Errorf("write hook code: %w", err)
	}
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
		entry := filepath.Join(root, filepath.Base(files[0]))
		args = []string{"--file", entry}
		cacheID = root + "#" + filepath.Base(files[0])
	}
	sig, err := signature(root, files)
	if err != nil {
		return hookProject{}, err
	}
	return hookProject{
		Root:      root,
		CacheID:   cacheID,
		Args:      args,
		Signature: sig,
	}, nil
}

// projectFromAllowedRoot reuses a known root while the shell stays in place.
// It also drops the trust entry if that root no longer contains a nix file.
func projectFromAllowedRoot(root string) (hookProject, bool, error) {
	allowed, err := readAllowList()
	if err != nil {
		return hookProject{}, false, err
	}
	root = filepath.Clean(root)
	if !allowed[root] {
		return hookProject{}, false, nil
	}
	files, ok, err := nixFiles(root)
	if err != nil {
		return hookProject{}, false, err
	}
	if !ok {
		delete(allowed, root)
		if err := saveAllowList(allowed); err != nil {
			return hookProject{}, false, err
		}
		return hookProject{}, false, nil
	}
	project, err := projectFrom(root, files)
	if err != nil {
		return hookProject{}, false, err
	}
	return project, true, nil
}

// findAllowedProject walks upward only when the shell changed directory or
// the allow list changed. Stale approvals are removed as they are encountered.
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

func allowModTime() (int64, error) {
	path, err := allowFile()
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", path, err)
	}
	return info.ModTime().UnixNano(), nil
}

// rememberCode updates shell-local markers. They are deliberately not exported:
// a child shell will simply redo the cheap lookup once on its first prompt.
func rememberCode(shell, pwd string, allowMod int64) string {
	mod := strconv.FormatInt(allowMod, 10)
	if shell == "fish" {
		return "set -g " + pwdVar + " " + quoteFish(pwd) + "\n" +
			"set -g " + allowModVar + " " + quoteFish(mod) + "\n"
	}
	return pwdVar + "=" + quoteBash(pwd) + "\n" +
		allowModVar + "=" + quoteBash(mod) + "\n"
}

// signature hashes top-level .nix files and flake.lock. This keeps each
// prompt cheap; imported files in subdirectories are intentionally not watched.
func signature(root string, files []string) (string, error) {
	h := sha256.New()
	if _, err := fmt.Fprintf(h, "%s\x00", root); err != nil {
		return "", fmt.Errorf("hash root: %w", err)
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
			return "", fmt.Errorf("relative path %s: %w", path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		if _, err := fmt.Fprintf(h, "%s\x00", rel); err != nil {
			return "", fmt.Errorf("hash path: %w", err)
		}
		if _, err := h.Write(data); err != nil {
			return "", fmt.Errorf("hash %s: %w", path, err)
		}
		if _, err := h.Write([]byte{0}); err != nil {
			return "", fmt.Errorf("hash %s: %w", path, err)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hookCachePath(project string) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	hooks := filepath.Join(dir, "hook")
	if err := os.MkdirAll(hooks, 0o700); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(project))
	return filepath.Join(hooks, hex.EncodeToString(sum[:])[:16]+".json"), nil
}

func loadCache(path string) (*hookCache, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cache %s: %w", path, err)
	}
	var cached hookCache
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("decode cache %s: %w", path, err)
	}
	return &cached, nil
}

func saveCache(path string, cached hookCache) error {
	data, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("encode cache %s: %w", path, err)
	}
	return writeFileAtomic(path, data, 0o600)
}

func decodeState(encoded string) (envState, error) {
	if encoded == "" {
		return envState{}, nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return envState{}, fmt.Errorf("decode %s: %w", stateVar, err)
	}
	var state envState
	if err := json.Unmarshal(data, &state); err != nil {
		return envState{}, fmt.Errorf("decode %s: %w", stateVar, err)
	}
	return state, nil
}

func encodeState(state envState) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode %s: %w", stateVar, err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func applyCode(shell string, changes map[string]*string) string {
	parts := make([]string, 0, len(changes))
	for _, key := range sortedKeys(changes) {
		if !validEnvKey(key) {
			continue
		}
		if changes[key] == nil {
			parts = append(parts, unsetCode(shell, key))
		} else {
			parts = append(parts, exportCode(shell, key, *changes[key]))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n") + "\n"
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

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const stateVar = "NIXSHELL_DIFF"

// printHook emits the few lines needed in a shell rc file. The hook itself
// calls back into nixshell on every prompt; nix is only run when files change.
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
		// Prepend so prompt plugins (e.g. oh-my-posh) see the new env.
		fmt.Print(`__nixshell_hook() { eval "$(nixshell hook-env zsh)"; }
autoload -Uz add-zsh-hook
add-zsh-hook -d precmd __nixshell_hook 2>/dev/null || true
precmd_functions=(__nixshell_hook ${precmd_functions[@]})
`)

	case "fish":
		// PWD fires before fish_prompt handlers, so prompt plugins see the new env.
		fmt.Print(`function __nixshell_hook --on-variable PWD --on-event fish_prompt
    nixshell hook-env fish | source
end
`)

	default:
		return fmt.Errorf("hook shell must be bash, zsh or fish")
	}

	return nil
}

type envState struct {
	Project string             `json:"project"`
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
// (or restores the previous values when leaving it).
func hookEnv(shell string) error {
	if shell != "bash" && shell != "zsh" && shell != "fish" {
		return fmt.Errorf("hook shell must be bash, zsh or fish")
	}
	state := decodeState(os.Getenv(stateVar))
	project, found := currentProject()
	if !found {
		if len(state.Prev) == 0 {
			return nil
		}
		fmt.Print(restoreCode(shell, state.Prev) + unsetCode(shell, stateVar) + "\n")
		return nil
	}

	sig := project.Signature
	cachePath, err := hookCachePath(project.key())
	if err != nil {
		return err
	}
	if state.Project == project.key() {
		// A nil Next means an old-format cache; ignore it.
		if cached := loadCache(cachePath); cached != nil && cached.Signature == sig && cached.Diff.Next != nil {
			// Re-assert cached values and state in case the rc file changed them.
			code := applyCode(shell, cached.Diff.Next) +
				exportCode(shell, stateVar, encodeState(state))
			fmt.Print(code + "\n")
			return nil
		}
	}

	base := os.Environ()
	restored := patchEnv(base, state.Prev)
	diff, err := devEnv(project.CacheID, project.Args, restored)
	if err != nil {
		return err
	}
	if err := saveCache(cachePath, hookCache{Signature: sig, Diff: diff}); err != nil {
		return err
	}

	newState := envState{Project: project.key(), Prev: diff.Prev}
	code := restoreCode(shell, state.Prev) +
		applyCode(shell, diff.Next) +
		exportCode(shell, stateVar, encodeState(newState))
	fmt.Print(code + "\n")
	return nil
}

// currentProject looks only at the current directory. Trust is checked via the
// allow database, so nothing in the repo is needed to opt in.
func currentProject() (hookProject, bool) {
	cwd, err := os.Getwd()
	if err != nil || !isAllowed(cwd) {
		return hookProject{}, false
	}
	files, err := filepath.Glob(filepath.Join(cwd, "*.nix"))
	if err != nil || len(files) == 0 {
		return hookProject{}, false
	}
	sort.Strings(files)
	names := make(map[string]bool, len(files))
	for _, path := range files {
		names[filepath.Base(path)] = true
	}

	var cacheID string
	var args []string
	switch {
	case names["flake.nix"]:
		args = []string{"path:" + cwd}
		cacheID = args[0]
	case names["shell.nix"]:
		args = []string{"--file", "shell.nix"}
		cacheID = cwd + "#shell.nix"
	case names["default.nix"]:
		args = []string{"--file", "default.nix"}
		cacheID = cwd + "#default.nix"
	default:
		entry := filepath.Base(files[0])
		args = []string{"--file", entry}
		cacheID = cwd + "#" + entry
	}
	return hookProject{
		Root:      cwd,
		CacheID:   cacheID,
		Args:      args,
		Signature: signature(cwd, files),
	}, true
}

// signature hashes only top-level .nix files and flake.lock. This keeps each
// prompt cheap; imported files in subdirectories are intentionally not watched.
func signature(root string, files []string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00", root)
	paths := append([]string{}, files...)
	lock := filepath.Join(root, "flake.lock")
	if _, err := os.Stat(lock); err == nil {
		paths = append(paths, lock)
	}
	sort.Strings(paths)
	for _, path := range paths {
		rel, _ := filepath.Rel(root, path)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s\x00", rel)
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
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

func loadCache(path string) *hookCache {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cached hookCache
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil
	}
	return &cached
}

func saveCache(path string, cached hookCache) error {
	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

func decodeState(encoded string) envState {
	var state envState
	if encoded == "" {
		return state
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || json.Unmarshal(data, &state) != nil {
		return envState{}
	}
	return state
}

func encodeState(state envState) string {
	data, _ := json.Marshal(state)
	return base64.StdEncoding.EncodeToString(data)
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
	var b strings.Builder
	for _, key := range sortedKeys(changes) {
		if !validEnvKey(key) {
			continue
		}
		if changes[key] == nil {
			b.WriteString(unsetCode(shell, key))
		} else {
			b.WriteString(exportCode(shell, key, *changes[key]))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func restoreCode(shell string, old map[string]*string) string {
	return applyCode(shell, old)
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

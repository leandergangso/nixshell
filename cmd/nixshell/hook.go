package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
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
	Diff    map[string]*string `json:"diff"`
}

type hookCache struct {
	Signature string            `json:"signature"`
	Changes   map[string]string `json:"changes"`
}

// hookEnv prints shell code that applies the dev env for the current directory
// (or restores the previous values when leaving it).
func hookEnv(shell string) error {
	if shell != "bash" && shell != "zsh" && shell != "fish" {
		return fmt.Errorf("hook shell must be bash, zsh or fish")
	}
	state := decodeState(os.Getenv(stateVar))
	root, installable, found := findProject()
	if !found {
		if len(state.Diff) == 0 {
			return nil
		}
		fmt.Print(restoreCode(shell, state.Diff) + unsetCode(shell, stateVar) + "\n")
		return nil
	}

	project := root + "#" + installable
	sig := signature(root)
	cachePath, err := hookCachePath(project)
	if err != nil {
		return err
	}
	if state.Project == project {
		if cached := loadCache(cachePath); cached != nil && cached.Signature == sig {
			// Re-assert cached values in case the rc file changed them.
			fmt.Print(applyCode(shell, cached.Changes) + "\n")
			return nil
		}
	}

	base := os.Environ()
	restored := restoreEnv(base, state.Diff)
	changes, err := devEnv(installable, restored)
	if err != nil {
		return err
	}
	if err := saveCache(cachePath, hookCache{Signature: sig, Changes: changes}); err != nil {
		return err
	}

	old := oldValues(restored, changes)
	code := restoreCode(shell, state.Diff) +
		applyCode(shell, changes) +
		exportCode(shell, stateVar, encodeState(envState{Project: project, Diff: old})) + "\n"
	fmt.Print(code)
	return nil
}

// A directory opts in by containing both flake.nix and an empty .nixshell
// file. The file's contents name the devShell, defaulting to "default".
func findProject() (string, string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "flake.nix")); err == nil {
			if data, err := os.ReadFile(filepath.Join(dir, ".nixshell")); err == nil {
				name := strings.TrimSpace(string(data))
				if name == "" {
					name = "default"
				}
				return dir, "path:" + dir + "#" + name, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
}

// Changes to any .nix file, flake.lock or .nixshell invalidate the cache.
func signature(root string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00", root)
	filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "result")) {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if name != "flake.nix" && name != "flake.lock" && name != ".nixshell" && !strings.HasSuffix(name, ".nix") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		fmt.Fprintf(h, "%s\x00", rel)
		h.Write(data)
		h.Write([]byte{0})
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

func hookCachePath(project string) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	hooks := filepath.Join(dir, "hook")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
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
	return os.WriteFile(path, data, 0o644)
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

func applyCode(shell string, changes map[string]string) string {
	var b strings.Builder
	for _, key := range sortedKeys(changes) {
		b.WriteString(exportCode(shell, key, changes[key]))
		b.WriteByte('\n')
	}
	return b.String()
}

func restoreCode(shell string, old map[string]*string) string {
	var b strings.Builder
	for _, key := range sortedKeys(old) {
		if old[key] == nil {
			b.WriteString(unsetCode(shell, key))
		} else {
			b.WriteString(exportCode(shell, key, *old[key]))
		}
		b.WriteByte('\n')
	}
	return b.String()
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

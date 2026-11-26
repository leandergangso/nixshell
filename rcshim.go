package main

import (
	"os"
	"path/filepath"
)

func bashShim() (string, error) {
	path, err := shimPath("bash", "bashrc")
	if err != nil {
		return "", err
	}
	content := `[ -e "$HOME/.bashrc" ] && source "$HOME/.bashrc"
export PATH="$NIXSHELL_PATH:$PATH"
unset NIXSHELL_PATH
`
	return path, writeShim(path, content)
}

func zshShimDir() (string, error) {
	dir, err := shimDir("zsh")
	if err != nil {
		return "", err
	}

	envShim := `if [ -n "$NIXSHELL_REAL_ZDOTDIR" ] && [ -f "$NIXSHELL_REAL_ZDOTDIR/.zshenv" ]; then
    source "$NIXSHELL_REAL_ZDOTDIR/.zshenv"
elif [ -f "$HOME/.zshenv" ]; then
    source "$HOME/.zshenv"
fi
`
	rcShim := `if [ -n "$NIXSHELL_REAL_ZDOTDIR" ] && [ -f "$NIXSHELL_REAL_ZDOTDIR/.zshrc" ]; then
    source "$NIXSHELL_REAL_ZDOTDIR/.zshrc"
elif [ -f "$HOME/.zshrc" ]; then
    source "$HOME/.zshrc"
fi
export PATH="$NIXSHELL_PATH:$PATH"
unset NIXSHELL_PATH
unset NIXSHELL_REAL_ZDOTDIR
`

	if err := writeShim(filepath.Join(dir, ".zshenv"), envShim); err != nil {
		return "", err
	}
	if err := writeShim(filepath.Join(dir, ".zshrc"), rcShim); err != nil {
		return "", err
	}
	return dir, nil
}

func fishInitCommand() string {
	return "set -gx PATH $NIXSHELL_PATH $PATH; set -e NIXSHELL_PATH"
}

func realZdotDir(env []string) string {
	values := envMap(env)
	if dir := values["ZDOTDIR"]; dir != "" {
		return dir
	}
	return values["HOME"]
}

func shimPath(kind, name string) (string, error) {
	dir, err := shimDir(kind)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func shimDir(kind string) (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(userCacheDir, "nixshell", "rc", kind)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func writeShim(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

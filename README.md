# nixshell

Enter a Nix devShell in the shell that launched it.

## Behavior

- Runs `nix develop`, runs your rc files, then execs your shell in place.
- After rc files, the Nix PATH is prepended; rc PATH edits survive.
- zsh: `ZDOTDIR` points at a tool-owned shim, then the real dir is restored. XDG and Home Manager layouts work.
- Caller `HOME` and `TMPDIR` win over Nix's values; zsh keeps caller `ZDOTDIR`.

Caches: `~/.cache/nixshell/rc`. Delete manually to reset.

## Prompt icon

```bash
NIXSHELL_ICON=${NIXSHELL_ICON-$'\uf313'}
nixshell_prompt_icon() { [[ -z ${IN_NIX_SHELL-} ]] || printf '%s ' "$NIXSHELL_ICON"; }
shopt -s promptvars
PS1='$(nixshell_prompt_icon) '"$PS1"
```

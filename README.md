# nixshell

Enter a Nix devShell in the shell that launched it.

## Behavior

- After rc files, PATH is set to the captured Nix PATH; rc PATH edits are dropped.
- zsh: `ZDOTDIR` points at a tool-owned shim, then the real dir is restored. XDG and Home Manager layouts work.
- Caller `HOME`, `ZDOTDIR`, and `TMPDIR` win over Nix's values.

Caches: `~/.cache/nixshell/profiles` and `~/.cache/nixshell/rc`. Delete manually to reset.

## Prompt icon

```bash
NIXSHELL_ICON=${NIXSHELL_ICON-$'\uf313'}
nixshell_prompt_icon() { [[ -z ${IN_NIX_SHELL-} ]] || printf '%s ' "$NIXSHELL_ICON"; }
shopt -s promptvars
PS1='$(nixshell_prompt_icon) '"$PS1"
```

# Prompt icon in nix shell

Add to `~/.bashrc`:

```bash
NIXSHELL_ICON=${NIXSHELL_ICON-$'\uf313'}
nixshell_prompt_icon() {
    [[ -z ${IN_NIX_SHELL-} ]] || printf '%s ' "$NIXSHELL_ICON"
}
shopt -s promptvars
PS1='$(nixshell_prompt_icon) '"$PS1"
```

`IN_NIX_SHELL` is set by Nix dev shells and passed through by nixshell.
Use a Nerd Font for the default `U+F313` glyph, or set `NIXSHELL_ICON='nix'`.

# nixshell

`nix develop` replacement, that drops you back into your own shell.

```sh
nixshell [devShell]   # -h: help, -v: version
```

Use it when you want a Nix dev environment without losing your aliases, prompt, and customizations.

Only bash, zsh, and fish are fully supported. Other shells get plain `nix develop --command <shell>`, without the rc and PATH handling.

## Prompt icon

Bash example, adapt to your own needs.

```bash
NIXSHELL_ICON=${NIXSHELL_ICON-$'\uf313'}
nixshell_prompt_icon() { [[ -z ${IN_NIX_SHELL-} ]] || printf '%s ' "$NIXSHELL_ICON"; }
shopt -s promptvars
PS1='$(nixshell_prompt_icon) '"$PS1"
```

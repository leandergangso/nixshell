# nixshell

A super simpler CLI, purpose built to enter into NIX devShells.

# Features

- direct mode, enter the devShell manually and explicitly
- hook mode, auto enter devShell on `cd`
- allow & deny directories for hook mode
- remove cache only and such ...
- remove all system files created by the tool (cache, allow, ...)
- extra: prompt example for when inside a devShell

## Prompt icon

See [PROMPT.md](PROMPT.md) for a bash snippet that shows a NIX icon when `IN_NIX_SHELL` is set.

## Direct mode

...

## Hook mode

```sh
bash: eval "$(nixshell --hook bash)"
zsh:  eval "$(nixshell --hook zsh)"
fish: nixshell --hook fish | source
```

How it works:

- add one line to your shell rc
- reload or open new shell
- `cd` into a project that defines NIX devShells
- nixshell will automatically enter the `.#default` shell
  - override shell to use with ...

This mode keeps your current shell and instead modify the shell state directly, based on devShell and current working directly.

NOTICE: i wonder if it should rather enter subshell instead of modify current shell?

## Notice

_Direct and hook mode should not be used at the same time._

# Development

`nix/` is a minimal flake for experimenting with nixshell, not for packaging the tool.

## Notes

`print-dev-env` runs in a throwaway shell taken from the dev environment:
`shellHook` is sourced with `SHELL` from the generated script (falling back to
`BASH`/`CONFIG_SHELL`), so a devShell-provided shell is used instead of the
caller's. `nix develop` may put `bashInteractive` first on PATH for its prompt;
that is not the shell that runs `shellHook`. Only changed exported variables are
copied; `HOME`, temp dirs, `shellHook` itself and bash-only state are dropped.
Only trusted directories are auto-entered, but their `shellHook` still runs
with your environment.

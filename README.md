# nixshell

Enter a Nix dev shell with your own shell. Linux only; static Go binary.
Needs `nix` and `bash`. The hook needs no extra tools.

## Install

```sh
task build
sudo task install              # PREFIX=/path to override
```

## Direct mode

```sh
nixshell                       # .#default
nixshell ci                    # .#ci
nixshell .#ci
nixshell github:org/repo#dev
nixshell -clean                # drop profiles + hook cache
```

The installable goes straight to `nix print-dev-env`. Shell: parent process,
falling back to `$SHELL`. `-clean` does not remove trusted directories.

## Test flake

`nix/` is a minimal flake for experimenting with nixshell, not for packaging
the tool:

```sh
nixshell path:./nix             # enter with nixshell
nix develop ./nix               # compare with Nix's own prompt
```

`nix/.gitignore` ignores `result` links from these experiments.

## Hook mode

Add one line to your shell rc:

```sh
bash: eval "$(nixshell -hook bash)"
zsh:  eval "$(nixshell -hook zsh)"
fish: nixshell -hook fish | source
```

Put the bash and fish lines after and before oh-my-posh init respectively. Zsh
prepends its hook automatically.

Trust a directory once with `nixshell -allow`; use `nixshell -revoke` to undo.
No project file is needed. The hook looks only in the current directory for
top-level `.nix` files: `flake.nix` (default devShell), else `shell.nix`, else
`default.nix`, else the first `.nix` file. It does not walk parents.

Entering applies the env; leaving restores it. Changes to top-level `.nix`
files or `flake.lock` refresh on the next prompt. Cache lives in
`$XDG_CACHE_HOME/nixshell`; trust lives in `$XDG_CONFIG_HOME/nixshell/allow`.

## Prompt icon

See [PROMPT.md](PROMPT.md) for a bash snippet that shows a Nix icon when
`IN_NIX_SHELL` is set, without oh-my-posh or another prompt framework.

## Notes

`print-dev-env` runs in a throwaway shell taken from the dev environment:
`shellHook` is sourced with `SHELL` from the generated script (falling back to
`BASH`/`CONFIG_SHELL`), so a devShell-provided shell is used instead of the
caller's. `nix develop` may put `bashInteractive` first on PATH for its prompt;
that is not the shell that runs `shellHook`. Only changed exported variables are
copied; `HOME`, temp dirs, `shellHook` itself and bash-only state are dropped.
Only trusted directories are auto-entered, but their `shellHook` still runs
with your environment.

```sh
task                           # build
```

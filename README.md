# nixshell

Enter a Nix dev shell with your own shell. Linux only; static Go binary.
Needs `nix` and `bash`. The hook needs no extra tools.

## Install

```sh
task build
sudo task install              # PREFIX=/path to override
# or: nix build && ./result/bin/nixshell --version
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

## Notes

`print-dev-env` runs in a throwaway bash; only changed exported variables are
copied. `HOME`, temp dirs, `shellHook` and bash-only state are dropped. Only
trusted directories are auto-entered, but their `shellHook` still runs with
your environment.

```sh
task                           # build + test
```

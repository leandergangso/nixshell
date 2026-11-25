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
nixshell --clean               # drop profiles + hook cache
```

The installable goes straight to `nix print-dev-env`. Shell: parent process,
falling back to `$SHELL`. `--clean` does not remove trusted directories; use
`--purge` for that. If hook mode is active, use `--deny` before `--clean`
because the cache holds the restore snippet.

Running `nixshell` from inside a dev shell exits with a hint to use
`exec nixshell ...` instead of creating another nested shell.

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
bash: eval "$(nixshell --hook bash)"
zsh:  eval "$(nixshell --hook zsh)"
fish: nixshell --hook fish | source
```

Put the bash and fish lines after and before oh-my-posh init respectively. Zsh
prepends its hook automatically.

Trust a project once with `nixshell --allow`; use `nixshell --deny` to undo.
Both resolve the nearest parent directory containing a `.nix` file, so they can
be run from any subdirectory. No project file is needed. The hook uses the first
allowed root containing a `.nix` file, preferring `flake.nix`, then `shell.nix`,
`default.nix`, or the first `.nix` file. Parent lookup only runs when `$PWD` or
the allow list changes.

Entering applies the env; leaving restores it. Changes to top-level `.nix`
files or `flake.lock` refresh on the next prompt. Stale approvals are removed
when their directory no longer contains a Nix file. The hook tracks the active
project with one `NIXSHELL_ACTIVE` variable; restore and re-assert snippets live
in the cache. Cache lives in `$XDG_CACHE_HOME/nixshell`; trust lives in
`$XDG_CONFIG_HOME/nixshell/allow`.

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

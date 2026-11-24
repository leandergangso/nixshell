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
falling back to `$SHELL`. `-clean` only removes nixshell's cache; it does not
run Nix GC.

## Hook mode

Add an empty `.nixshell` file (or a devShell name) to the flake root, then one
line in your shell rc:

```sh
bash: eval "$(nixshell -hook bash)"
zsh:  eval "$(nixshell -hook zsh)"
fish: nixshell -hook fish | source
```

For prompt plugins, put the bash line last in your rc file; zsh and fish handle
ordering themselves.

Entering applies the env; leaving restores it. Changes to `.nix` files,
`flake.lock` or `.nixshell` rebuild on the next prompt; no `cd` needed. The
cache lives in `$XDG_CACHE_HOME/nixshell`.

## Notes

`print-dev-env` runs in a throwaway bash; only changed exported variables are
copied. `HOME`, temp dirs, `shellHook` and bash-only state are dropped. GC roots
live in `$XDG_CACHE_HOME/nixshell/profiles`.

```sh
task                           # build + test
```

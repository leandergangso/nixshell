# nixshell

`nixshell` enters a Nix development shell **using your own shell** instead of
nixpkgs' `bashInteractive`. It is a single Linux-only Go binary with no runtime
dependencies other than `nix`, `bash` (used internally) and your chosen shell.

## Install

```sh
# From a checkout (static binary, no runtime Go dependency)
task build
./nixshell --version
sudo task install              # PREFIX=/path to change the destination

# Or with Nix
nix build
./result/bin/nixshell --version
nix profile install .          # or: nix-env -f default.nix -i
```

## Usage

```sh
nixshell                       # devShells.<system>.default in the current flake
nixshell ci                    # shorthand for .#ci
nixshell .#ci
nixshell github:org/repo#dev   # any Nix flake installable
nixshell --shell /bin/bash .#dev
nixshell -v .#dev              # show each step on stderr
```

The installable is passed directly to `nix print-dev-env`, so regular Nix flake
reference syntax works. Options must come before the installable.

The shell is chosen in this order:

1. `--shell PATH`
2. the parent process, if it is a known shell (`/proc/$PPID/exe`)
3. `$SHELL`
4. the login shell from `/etc/passwd`

## How it works

1. Run `nix print-dev-env --profile <temporary profile> <installable>` and keep
   an out-link under `$XDG_CACHE_HOME/nixshell/profiles/<hash>` as a GC root, so
   store paths are not collected while the environment is in use. The hash
   includes the absolute directory for local flake refs.
2. Source the script printed by `nix print-dev-env` in a throwaway bash. That
   script applies Nix's build environment, preserves the caller's `PATH` and
   `XDG_DATA_DIRS`, and evaluates `shellHook` — so variables exported by the
   hook are captured too.
3. Dump the bash process' exported environment and copy only variables whose
   values changed into the environment of the new shell. `HOME`, build temp
   dirs (`TMPDIR`, `NIX_BUILD_TOP`, ...), `shellHook` itself, bash bookkeeping
   and exported bash functions are deliberately not copied.
4. `exec` the chosen shell with that environment.

### Why not `--json`?

`nix print-dev-env --json` does not include Nix's ignore list, `PATH`
preservation or `shellHook` execution. Using the regular script output avoids
reimplementing all of that (and Nix's shell escaping) in Go. The resulting
environment is the same one `nix-direnv` sources, minus state that only makes
sense inside bash.

## Limitations

- Bash-only state — functions, aliases, arrays, shell options — is not carried
  into zsh/fish/etc. A `shellHook` can still set exported variables.
- `shellHook` side effects run in the throwaway bash. Use your shell's rc file
  for anything that must affect the interactive session itself.
- Only one installable per invocation. The direnv-style hook mode is not
  implemented yet.
- Linux only.

## Development

```sh
task                           # build + test
nix build
```

The tests use a fake `nix` executable and therefore do not need Nix installed.

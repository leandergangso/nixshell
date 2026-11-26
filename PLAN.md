# PLAN: direct-mode rewrite

**Status:** design settled, no code changed yet. Written so work can resume from this
file alone.

## Goal

Make `nixshell` a simple, minimal, production-ready alternative to `nix develop` for
**direct mode only**. The user's own shell should be the real top-level process, with
the dev-shell state injected into it, and the user's rc files should still run.

Hook mode is **dropped** entirely (it is still described in `README.md` and needs
removal from the docs).

## Locked decisions

- Direct mode only; no hook mode.
- The user shell is detected by `currentShell()` (`/proc/<ppid>/exe` with `$SHELL`
  fallback). Only `SHELL` is ever used — no `BASH`, `CONFIG_SHELL`, or lowercase
  `shell` fallbacks.
- **Do not start the user shell from inside `nix develop`'s bash.** Capture the
  state Nix produces, then `exec` the user shell cleanly with that state injected.
- User rc files **always** run (zsh/fish/bash config is not optional).
- Nix's PATH must win over whatever the user rc does to PATH.
- No `IN_NIX_SHELL` nesting guard (Nix allows nesting; the guard was brittle).
  Current `shell.go` already has none.
- No throwaway temp files: no `os.MkdirTemp`, no `source "$1" ... env -0 > "$2"`
  indirection. Persistent cache/config files **are** acceptable.
- Do not reimplement Nix's activation script in Go (`doc.script()` is considered
  ugly/messy). Delegate env semantics to Nix.
- Keep it simple for both users and maintainers; reuse existing helpers where
  applicable.

## Environment / tooling

- Go 1.27.3 available.
- `bash` at `/usr/sbin/bash`, `zsh` at `/usr/sbin/zsh`; **fish is not installed**, so
  its shim can only be code-reviewed, not tested locally.
- Nix 2.35.2 (Determinate Nix 3.22.3). `nixFeatures` is
  `[]string{"--extra-experimental-features", "nix-command flakes"}`.
- `/repos/nixshell/nix/` is the local experiment flake; its devShell has
  `shellHook = ''echo nix shell''`.

## Research findings (verified)

### `nix develop`

- Both `develop` and `print-dev-env` build an env derivation via `getBuildEnvironment`
  (which calls `updateProfile`, so `--profile` works for both) and read JSON dumped by
  `get-env.sh` after sourcing stdenv `setup.sh`.
- `makeRcScript` adds: ignore-list filtering, caller `PATH`/`XDG_DATA_DIRS` appended
  (not overwritten), `NIX_BUILD_TOP=$(mktemp -d -t nix-shell.XXXXXX)` plus
  `TMP`/`TMPDIR`/`TEMP`/`TEMPDIR` pointing at it, and `eval "${shellHook:-}"`.
- `nix develop` with **no** `--command` writes an rc file and `exec`s
  `bash --rcfile <file>` — it never hands control back, so a later "capture from the
  nix develop shell" step is impossible.
- With `--command`, Nix appends `exec <command>` to the same rc script and runs bash
  non-interactively with the rc file. Caller env IS preserved (verified: `HOME`,
  `USER`, arbitrary custom vars all came through). `SHELL` is overwritten with
  `bashInteractive`; `bashInteractive/bin` is prepended to PATH; hook output goes to
  stdout; `NIX_BUILD_TOP` is created and leaked. `execProgramInStore` uses `execvp`.
- Shellhook side effects are real: `mkShell { shellHook = "export FOO=fromHook"; }`
  gives no `FOO` in `print-dev-env --json` but `FOO=fromHook` under `nix develop`.
  So the hook must be executed; JSON alone is not enough.

### `nix print-dev-env`

- `--json` output is post-stdenv, pre-shellHook. `SHELL`, `shell`, `BASH` and
  `CONFIG_SHELL` are present for stdenv/mkShell shells. Lowercase `shell` can differ
  from `SHELL` (a `mkShell { shell = "${zsh}/bin/zsh"; }` produced `shell=zsh`,
  `SHELL=bash`), which is why the old code's `shell` lookup was wrong.
- Non-JSON output is the activation script; direnv and devenv use this form.
- `print-dev-env` itself does not select `bashInteractive`, override `SHELL`, or set
  `NIX_BUILD_TOP` in the caller.

### direnv

- `use_flake` is basically `eval "$(nix print-dev-env --profile <layout>/flake-profile "$@")"`.
- nix-direnv caches that script as `profile.rc`, restores `TMP*`, deletes
  `NIX_BUILD_TOP`.
- direnv's own env dump uses the fd trick `exec 3>&1; exec 1>&2` and dumps env to
  fd 3. This is the origin of the fd-3 capture idea.

### devenv (current `main` sources)

- It does **not** call `nix develop`. It evaluates the devenv deriver through an
  embedded Nix library (`cnix`), reads the build environment JSON from the store
  (`BuildEnvironment::get_dev_environment`), and rebuilds a bash activation script in
  Rust via `to_activation_script()` (save PATH/XDG_DATA_DIRS → export vars/functions →
  append saved values → `eval "${shellHook:-}"`). No `bashInteractive`, no `SHELL`
  override, no `NIX_BUILD_TOP`. Gcroot is `.devenv/gc/shell`; eval results are cached.
- Launch sequence in `prepare_shell_with_script_dir`: strip trailing
  `exec bash` / `exec $SHELL`, write `.devenv/shell-env.sh`, then start **bash for
  every target shell**:
  `bash --noprofile --rcfile <generated-rc> -i`. `--noprofile` avoids profile PATH
  overrides; `--rcfile` controls ordering.
- The exact fix for the PATH problem (in `devenv-shell/src/dialect/*.rs`):

  ```bash
  source ".devenv/shell-env.sh"      # Nix env + shellHook
  export _DEVENV_PATH="$PATH"        # save Nix PATH
  source "$HOME/.bashrc"             # user rc runs
  export PATH="$_DEVENV_PATH"        # Nix PATH wins
  exec zsh -i
  ```

  - zsh: sets `ZDOTDIR` to `.devenv/zsh`; generated `.zshrc` restores the real
    `ZDOTDIR`, sources real `.zshenv`/`.zshrc`, then restores PATH.
  - fish: `exec fish -i -C "source .devenv/devenv.fish"`, where the file does
    `set -gx PATH (string split ":" -- $_DEVENV_PATH)`.
  - Prompt hooks (`PROMPT_COMMAND`, `precmd`, wrapped `fish_prompt`) re-restore PATH
    and apply hot-reload env diffs; `_DEVENV_DIFF` is gzip+base64, direnv-inspired.
- devenv's restore is a blunt overwrite — rc PATH additions are discarded. That is
  deliberate strictness; the caller PATH is still present because the activation
  script appended it.
- Cost: generated rc files, per-shell dialect code, env-diff machinery, prompt hooks,
  `mktemp` in the rcfile. We do **not** need that whole framework.

## Rejected approaches

1. `nix develop --command <user-shell>` — simplest, but starts the user shell from
   inside nix's bash (explicitly not wanted), and has no post-rc PATH restore; also
   brings `bashInteractive` PATH prepend and leaked `NIX_BUILD_TOP`.
2. `nix develop` with no `--command` — interactive bash REPL, cannot capture.
3. `print-dev-env --json` alone — misses `shellHook` side effects entirely.
4. JSON + rebuilding the activation script in Go (`doc.script()`) — ugly, duplicates
   Nix, and still needs a shell to run the hook.
5. rc-disabled shells (`zsh -f`, `bash --noprofile --norc`, `fish --no-config`) —
   guarantees PATH, but the user always wants their rc.
6. Full devenv-style dialect framework with env diffing and reload hooks — too much
   machinery for this tool.

## Chosen design

### Layer 1 — capture from Nix, then exec the user shell

Rewrite `devEnv` in `/repos/nixshell/nix.go`:

```go
r, w, err := os.Pipe()
if err != nil { return nil, err }
defer r.Close()

args := append([]string{}, nixFeatures...)
args = append(args, "develop", "--profile", profilePath, devShell,
    "--command", "bash", "-c", "env -0 >&3")

cmd := exec.CommandContext(ctx, "nix", args...)
cmd.Stdout = os.Stderr          // shellHook output stays visible
cmd.Stderr = os.Stderr
cmd.ExtraFiles = []*os.File{w}  // child fd 3 = clean capture pipe
if err := cmd.Start(); err != nil { w.Close(); return nil, err }
w.Close()
data, err := io.ReadAll(r)
if err != nil { return nil, err }
if err := cmd.Wait(); err != nil { return nil, fmt.Errorf("nix develop: %w", err) }

captured := parseEnv0(data)
env := envMap(base)
for key, value := range captured {
    if ignoredEnv[key] || strings.HasPrefix(key, "BASH_FUNC_") { continue }
    env[key] = value
}
env["NIXSHELL_PATH"] = env["PATH"]      // for Layer 2
return envSlice(env), nil

// shell.go then does: env = setEnv(env, "SHELL", shell)
```

Then in `shell.go`: `syscall.Exec(shell, []string{shell}, env)`.

Notes:
- Keep the profile setup currently at `nix.go:30-39`: `os.UserCacheDir()`,
  `os.MkdirAll(<cache>/nixshell/profiles, 0o700)`, and
  `profilePath = filepath.Join(cacheDir, cacheKey(devShell))`. This is persistent
  cache state, not a throwaway temp file.
- `cmd.Stdin` stays nil, matching today's behaviour (hook `read` sees EOF).
- `exec 3>&1 1>&2` is not needed here because `cmd.Stdout` is already `os.Stderr`;
  only `env -0 >&3` reaches the pipe. Read the pipe fully before `cmd.Wait()`.
- Reinstate `ignoredEnv` from `nix.go:21-27` verbatim. Its exact contents are
  `HOME`, `shellHook`, `NIX_BUILD_TOP`, `TMP`, `TMPDIR`, `TEMP`, `TEMPDIR`,
  `terminfo`, `SHLVL`, `_`, `PWD`, `OLDPWD`, `ZDOTDIR`, `BASH_ENV`, `ENV`.
  Consequences to preserve deliberately: caller `HOME` wins over the dev env's;
  caller `ZDOTDIR` is dropped (Layer 2 must save it before overwriting — see below);
  `NIX_BUILD_TOP`/temp vars never reach the user shell.
- `NIX_GCROOT` and `IN_NIX_SHELL=impure` are left as Nix sets them.
- Keep `cacheKey`/`isLocalRef` only to name the profile gcroot; there is no Go-side
  eval cache to preserve (Nix caches evaluation itself).

**Delete from `nix.go`:** `printDevEnv` usage, `jsonString`, `shellQuote`,
`shellQuotes`, `shellAssoc`, `sortedKeys`, `os.MkdirTemp`, `defer os.RemoveAll`,
`WriteFile`/`ReadFile`, JSON imports (`encoding/json/v2`, `encoding/json/jsontext`).
**Delete entirely:** `/repos/nixshell/print_dev_env.go` (contains `printDevEnv`,
`stringVar`, `script()`).
**Keep:** `parseEnv0`, `envMap`, `envSlice`, `cacheKey`, `isLocalRef`, `nixFeatures`.
**Keep in `shell.go`:** arg validation, `normalizeDevShell`, `currentShell`,
`setEnv`, final `syscall.Exec`.

### Layer 2 — small persistent rc shim for PATH priority

Generated once under `~/.cache/nixshell/rc/` (persistent config, not throwaway). The
shell is still launched cleanly by `nixshell`; the shim only wraps rc ordering.

Select the dialect with `filepath.Base(shell)` — `bash`, `zsh`, `fish` — and fall back
to a plain `syscall.Exec(shell, []string{shell}, env)` for anything else. Pass the
shell's real path as `argv[0]`.

- **bash**: `<shell> --noprofile --rcfile <shim> -i`; shim:
  ```bash
  [ -e "$HOME/.bashrc" ] && source "$HOME/.bashrc"
  export PATH="$NIXSHELL_PATH"
  unset NIXSHELL_PATH
  ```
- **zsh**: save the caller's `ZDOTDIR` as `NIXSHELL_REAL_ZDOTDIR` before setting
  `ZDOTDIR=<cache>/zsh` (the capture's `ignoredEnv` drops `ZDOTDIR`, so this must be
  read from the caller env, not from the merged env). Exec `zsh -i` directly — no
  nested bash. Generated `.zshrc` restores the real `ZDOTDIR` and sources the real
  `.zshenv`/`.zshrc`, then:
  ```bash
  export PATH="$NIXSHELL_PATH"
  unset NIXSHELL_PATH
  ```
  (Generated `.zshenv` should likewise source the real `.zshenv`.)
- **fish**: `<shell> -i --init-command 'set -gx PATH (string split ":" -- $NIXSHELL_PATH); set -e NIXSHELL_PATH'`
- **other shells**: plain exec; documented as "rc runs, PATH priority not guaranteed".

No env diffing, no reload hooks, no dialect framework, no prompt hooks — those are
the devenv pieces we intentionally skip.

## Files touched

- `/repos/nixshell/nix.go` — rewrite `devEnv`, reinstate `ignoredEnv`, remove helpers.
- `/repos/nixshell/shell.go` — set `SHELL`/`NIXSHELL_PATH`, choose shim launch args.
- `/repos/nixshell/print_dev_env.go` — delete.
- `/repos/nixshell/README.md` — remove hook-mode docs and the "should not be used at
  the same time" notice; rewrite the `print-dev-env`/`shellHook` notes to describe
  capture via `nix develop`; document rc/PATH behaviour and shell coverage.
- Optionally a new small helper file for generating the Layer 2 shims, if they are
  implemented.

## Open questions

1. Implement Layer 1 first and add Layer 2 after, or both in one pass?
2. Shim coverage: bash/zsh/fish only, with plain exec for everything else?

## Verification checklist

- `go test ./...` (if tests exist) and `go build ./...`.
- To test locally: `go build -o /tmp/opencode/nixshell .` from the repo root, then run
  from `/repos/nixshell/nix` with that binary. `currentShell()` reads
  `/proc/<ppid>/exe`, so launch it from the shell you want to test (e.g. `zsh -c
  '/tmp/opencode/nixshell'`) rather than relying on `$SHELL` alone.
- Manual: `cd nix && nixshell` prints `nix shell` (hook ran), lands in the user's
  shell, `echo $SHELL` is the user shell, PATH head is the dev-shell store path.
- Manual: hook exporting a variable (e.g. `FOO=fromHook`) is visible in the final shell.
- Manual: caller env (`HOME`, custom var) survives; `NIX_BUILD_TOP`/`TMPDIR` do not.
- Manual: put a PATH mutation in a temp `~/.zshrc`/`~/.bashrc`; confirm dev PATH still
  wins after rc.
- Manual: nesting — setting `IN_NIX_SHELL=impure` does not block entry.
- Manual: `nixshell ci`, `nixshell .#ci`, `nixshell .`, and a flake ref all work and
  reuse the same profile path.

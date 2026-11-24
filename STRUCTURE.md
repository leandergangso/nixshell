# nixshell

a tool meant primarily to enter nix devshells, a better alternative to
'nix develop'

> nixshell

Run manually to enter a dev shell. With no installable, nixshell uses `.`; Nix
itself searches upward for a flake when the current directory has none.

Primarily based on `nix print-dev-env --profile ...`. Nix writes a bash script,
which we source with the shell named in that script (`SHELL`, then `BASH` or
`CONFIG_SHELL`) so `shellHook` runs in the dev environment. The resulting env is
captured and diffed against the caller so it can be applied and restored.

Direct mode stays one-shot: `syscall.Exec` starts the chosen shell as a child of
the caller, so `exit` returns to the original shell. Calling nixshell while
already in a dev shell warns and exits instead of creating another nested shell.

> nixshell --hook <shell>

Placed in a shell rc file. Aimed to work in +90% of bash, zsh and fish rc setups.

The hook enters allowed dirs with very little overhead. On every prompt it
re-asserts the cached env, so rc files and prompt plugins cannot drift it. Nix
only runs when the signature changes: cwd plus top-level `.nix` files and
`flake.lock`. Imported files in subdirectories are intentionally not watched.

Parent lookup runs only when `$PWD` or the allow list changes, tracked by
shell-local `NIXSHELL_PWD` and `NIXSHELL_ALLOW`. An allowed root is pruned
automatically when it no longer contains a Nix file.

No pause/resume flags: use `--deny`, then `--allow` again to jump out and back.

> nixshell --clean

Remove cached profiles and hook state. Leaves approved directories alone.

> nixshell --purge

Remove all approvals, and the config directory when it is empty.

> nixshell --allow

For hook mode only. Trust the nearest ancestor containing a Nix file. Idempotent.

> nixshell --deny

For hook mode only. Remove trust for that same project root. Idempotent.

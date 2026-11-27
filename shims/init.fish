# Apply the dev-shell PATH after fish config has run.
if set -q NIXSHELL_PURE[1]; and test -n "$NIXSHELL_PURE"
    set -gx PATH $NIXSHELL_PATH
else if set -q NIXSHELL_PATH[1]
    set -gx PATH $NIXSHELL_PATH $PATH
end
set -e NIXSHELL_PATH
set -e NIXSHELL_PURE

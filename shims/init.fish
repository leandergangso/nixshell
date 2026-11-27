# Prepend the dev-shell PATH after fish config has run.
if set -q NIXSHELL_PATH[1]
    set -gx PATH $NIXSHELL_PATH $PATH
    set -e NIXSHELL_PATH
end

# nixshell: fish config has run; prepend the dev PATH.
if set -q NIXSHELL_PATH[1]
    set -gx PATH $NIXSHELL_PATH $PATH
end
set -e NIXSHELL_PATH

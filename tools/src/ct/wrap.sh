#!/bin/sh

# ct needs to be able to find yamale by an _absolute_ path, not a relative
# path, for some godforsaken reason. So start by finding an absolute path
# to our ct.d directory.

ROOT_DIR="$(cd $(dirname $0) && pwd)/ct.d"

# Given that, set $HOME for ct's benefit, so $HOME/.ct/chart_schema.yaml
# is there...
export HOME="${ROOT_DIR}/home"

# ...and set the PATH so ct can find yamale.
export PATH="${ROOT_DIR}/bin:${ROOT_DIR}/venv/bin:$PATH"

exec ct "$@"

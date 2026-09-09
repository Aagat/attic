#!/bin/sh
set -eu
Xvfb :99 -screen 0 1024x768x24 -nolisten tcp &
export DISPLAY=:99
exec /usr/local/bin/attic "$@"

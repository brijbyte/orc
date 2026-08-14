#!/bin/sh
# Force agy to use its file token store in headless OpenCode sessions.
set -eu

: "${SSH_CONNECTION:=127.0.0.1 0 127.0.0.1 0}"
export SSH_CONNECTION
exec /home/opencode/.local/bin/agy-real "$@"

#!/bin/sh
set -eu

RINGCLAW_HOME="${RINGCLAW_HOME:-${HOME:-/home/ringclaw}/.ringclaw}"
mkdir -p "${RINGCLAW_HOME}" "${RINGCLAW_HOME}/workspace" "${RINGCLAW_HOME}/memory"

# Default to Personal AVA runtime mode.
# If K8S/control-plane passes ringclaw subcommands like "runtime start",
# prefix them with the ringclaw binary. Other commands still run directly.
if [ "$#" -gt 0 ]; then
  case "$1" in
    start|runtime|config|version|help)
      exec ringclaw "$@"
      ;;
    *)
      exec "$@"
      ;;
  esac
fi

exec ringclaw runtime start

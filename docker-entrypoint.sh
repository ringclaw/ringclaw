#!/bin/sh
set -eu

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

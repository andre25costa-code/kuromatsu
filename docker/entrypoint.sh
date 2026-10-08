#!/bin/sh
set -e

# First-run: neither config nor workspace exists.
# If config.json is already mounted but workspace is missing we skip onboard to
# avoid the interactive "Overwrite? (y/n)" prompt hanging in a non-TTY container.
if [ ! -d "${HOME}/.kuromatsu/workspace" ] && [ ! -f "${HOME}/.kuromatsu/config.json" ]; then
    kuromatsu onboard
    echo ""
    echo "First-run setup complete."
    echo "Add a model, then restart the container:"
    echo "  kuromatsu model add -b <api-base> -k <api-key>   (e.g. via docker compose exec)"
    echo "or edit ${HOME}/.kuromatsu/config.json."
    exit 0
fi

# Remove stale PID file from a previous container run.
# After docker kill / OOM / crash the PID file may linger on the bind-mounted
# volume and block the next gateway start (the recorded PID could collide with
# an unrelated process inside the new container).
rm -f "${HOME}/.kuromatsu/.kuromatsu.pid"

exec kuromatsu gateway "$@"

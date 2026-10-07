#!/bin/bash
# deploy.sh — install a freshly-built kuromatsu/nativebench binary pair on a
# systemd host (ADR-013, AC-020-4). No Docker: this is the whole point of the
# systemd deploy path.
#
# Usage: DEPLOY_HOST=<ssh-alias-or-user@host> ./deploy.sh <kuromatsu-binary> <nativebench-binary>
# DEPLOY_HOST may also come from a .env file at the repository root.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if [ -z "${DEPLOY_HOST:-}" ] && [ -f "$ROOT/.env" ]; then
    DEPLOY_HOST="$(grep -E '^DEPLOY_HOST=' "$ROOT/.env" | tail -1 | cut -d= -f2- | tr -d "\"' ")"
fi
DEPLOY_HOST="${DEPLOY_HOST:?set DEPLOY_HOST (ssh alias or user@host), e.g. in .env}"

KUROMATSU_BIN="${1:?usage: deploy.sh <kuromatsu-binary> <nativebench-binary>}"
NATIVEBENCH_BIN="${2:?usage: deploy.sh <kuromatsu-binary> <nativebench-binary>}"

echo "== deploying to $DEPLOY_HOST =="
scp -o ConnectTimeout=20 "$KUROMATSU_BIN" "$DEPLOY_HOST":/tmp/kuromatsu-new
scp -o ConnectTimeout=20 "$NATIVEBENCH_BIN" "$DEPLOY_HOST":/tmp/nativebench-new

ssh -o ConnectTimeout=20 "$DEPLOY_HOST" '
set -e
sudo systemctl stop kuromatsu
sudo install -m755 -o root -g root /tmp/kuromatsu-new /opt/kuromatsu/bin/kuromatsu
sudo install -m755 -o root -g root /tmp/nativebench-new /opt/kuromatsu/bin/nativebench
rm -f /tmp/kuromatsu-new /tmp/nativebench-new
sudo systemctl start kuromatsu
sleep 3
sudo systemctl status kuromatsu --no-pager | head -8
'
echo "== done =="

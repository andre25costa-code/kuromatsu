#!/bin/bash
# deploy-demetrius.sh — install a freshly-built kuromatsu/nativebench binary
# pair on the demetrius systemd deploy (ADR-013, AC-020-4). No Docker: this
# is the whole point of the systemd/demetrius path.
#
# Usage: ./deploy-demetrius.sh <kuromatsu-binary> <nativebench-binary>
# Run from a machine with the `demetrius` SSH alias configured.
set -euo pipefail

KUROMATSU_BIN="${1:?usage: deploy-demetrius.sh <kuromatsu-binary> <nativebench-binary>}"
NATIVEBENCH_BIN="${2:?usage: deploy-demetrius.sh <kuromatsu-binary> <nativebench-binary>}"

echo "== deploying to demetrius =="
scp -o ConnectTimeout=20 "$KUROMATSU_BIN" demetrius:/tmp/kuromatsu-new
scp -o ConnectTimeout=20 "$NATIVEBENCH_BIN" demetrius:/tmp/nativebench-new

ssh -o ConnectTimeout=20 demetrius '
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

#!/bin/bash
# backup-to-kuro.sh — nightly cross-cloud backup of the Kuromatsu home
# directory, pushed from demetrius (Google Cloud) to kuro (Oracle), the
# backup target per ADR-013/S33. Invoked by kuromatsu-backup.service
# (kuromatsu-backup.timer, 04:30 daily) as the `kuromatsu` system user.
#
# Prerequisites on demetrius (one-time, manual -- not automated by this
# script, since it needs privileged/security changes outside the repo):
#   1. `apt install zstd` (userspace zstd CLI; zram's kernel-side zstd
#      support does not provide this).
#   2. An SSH keypair for the `kuromatsu` user, with its public key added
#      to kuro's ~kuromatsu-backup/.ssh/authorized_keys (a *dedicated*
#      account on kuro, not the interactive admin user) -- restrict that
#      account with `command="..."`/no-pty in authorized_keys or an rrsync
#      wrapper if kuro's rsync version supports it.
#   3. KUROBACKUP_HOST (default: kuro) and KUROBACKUP_USER (default:
#      kuromatsu-backup) below overridable via environment if that setup
#      differs.
set -euo pipefail

KUROMATSU_HOME="${KUROMATSU_HOME:-/var/lib/kuromatsu}"
KUROBACKUP_HOST="${KUROBACKUP_HOST:-kuro}"
KUROBACKUP_USER="${KUROBACKUP_USER:-kuromatsu-backup}"
KUROBACKUP_DIR="${KUROBACKUP_DIR:-/home/${KUROBACKUP_USER}/backups}"
STATE_FILE="${KUROMATSU_HOME}/run/state"

log() { logger -t backup-to-kuro "$*" 2>/dev/null || echo "[backup-to-kuro] $*"; }

# Gate by runstate == Idle (ADR-016), skipping the run rather than failing
# it when the agent is genuinely busy at 04:30 -- a missed backup with a
# clear log line beats colliding with an in-flight inference/tool call.
# Absent entirely (runstate.enabled=false, or before the agent's first
# transition writes the file) is NOT treated as "busy": compatibility with
# runstate disabled means backups must keep working exactly as before C1.
if [ -f "$STATE_FILE" ] && ! grep -q 'names=idle' "$STATE_FILE"; then
	log "skipped: runstate is not idle ($(cat "$STATE_FILE"))"
	exit 0
fi

if ! command -v zstd >/dev/null 2>&1; then
	log "error: zstd not installed (apt install zstd) -- see this script's header"
	exit 1
fi

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
archive_name="kuromatsu-${timestamp}.tar.zst"
staging_dir="$(mktemp -d)"
trap 'rm -rf "$staging_dir"' EXIT
archive_path="${staging_dir}/${archive_name}"

# config.json/workspace/telemetry are the user's actual data (memory,
# sessions, turn history); .security.yml holds credentials, backed up
# encrypted-at-rest the same way it's stored locally. models/ is excluded:
# the GGUF is a large, redistributable download, not user data (S33) --
# re-running scripts/download-model.sh restores it, not a backup.
tar --create --zstd \
	--file "$archive_path" \
	--directory "$KUROMATSU_HOME" \
	--exclude=models \
	--exclude=run \
	--warning=no-file-changed \
	config.json .security.yml workspace telemetry 2>/dev/null || true

if [ ! -s "$archive_path" ]; then
	log "error: archive is empty or was not created at $archive_path"
	exit 1
fi

if ! ssh -o ConnectTimeout=20 -o BatchMode=yes "${KUROBACKUP_USER}@${KUROBACKUP_HOST}" \
	"mkdir -p '${KUROBACKUP_DIR}'"; then
	log "error: could not reach ${KUROBACKUP_USER}@${KUROBACKUP_HOST} (see this script's header for the one-time SSH setup)"
	exit 1
fi

rsync -az --timeout=120 "$archive_path" "${KUROBACKUP_USER}@${KUROBACKUP_HOST}:${KUROBACKUP_DIR}/${archive_name}"

log "backed up ${KUROMATSU_HOME} to ${KUROBACKUP_HOST}:${KUROBACKUP_DIR}/${archive_name}"

# Remote retention (keep the last 14 nightly archives) -- local staging is
# already cleaned up by the trap above regardless of outcome.
ssh -o ConnectTimeout=20 -o BatchMode=yes "${KUROBACKUP_USER}@${KUROBACKUP_HOST}" \
	"cd '${KUROBACKUP_DIR}' && ls -1t kuromatsu-*.tar.zst 2>/dev/null | tail -n +15 | xargs -r rm -f" \
	|| log "warning: remote retention cleanup failed (backup itself already succeeded)"

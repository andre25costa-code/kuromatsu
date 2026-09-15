#!/bin/bash
# trim-os.sh — versions the demetrius OS trim from Trilho 0 step 2
# (ADR-013/S32/NFR-001), previously applied by hand and not reproducible
# from the repo (Trilho F audit finding). Disables services that compete
# for the ~250MB of RAM this e2-micro needs for the model+KV, without
# purging them -- a week of "disabled, not gone" makes rollback trivial
# (`systemctl enable --now <unit>`).
#
# Kept untouched, deliberately: google-guest-agent (SSH keys/metadata),
# unattended-upgrades (security patches), chrony (clock sync -- TLS/cron
# scheduling both need correct time).
#
# Idempotent: safe to re-run after a fresh install or an OS upgrade that
# re-enables one of these units. Units absent on a given image (this list
# was built against Debian 12 on GCP) are skipped, not treated as errors.
set -uo pipefail

log() { echo "[trim-os] $*"; }

disable_unit() {
	local unit="$1"
	if ! systemctl list-unit-files "$unit" >/dev/null 2>&1; then
		log "skip (not installed): $unit"
		return 0
	fi
	if systemctl is-enabled --quiet "$unit" 2>/dev/null || systemctl is-active --quiet "$unit" 2>/dev/null; then
		systemctl disable --now "$unit" 2>&1 | sed "s/^/[trim-os] /"
		log "disabled: $unit"
	else
		log "already disabled: $unit"
	fi
}

# google-cloud-ops-agent bundles two units (...-metrics, ...-logging) on
# some images; the glob below covers both without hardcoding version-
# specific unit names.
for unit in $(systemctl list-unit-files 'google-cloud-ops-agent*' --no-legend 2>/dev/null | awk '{print $1}'); do
	disable_unit "$unit"
done

for unit in google-osconfig-agent.service exim4.service rsyslog.service haveged.service \
	fail2ban.service docker.socket docker.service containerd.service; do
	disable_unit "$unit"
done

log "done. google-guest-agent, unattended-upgrades and chrony were left untouched."
log "fail2ban's job is taken over by deploy/nftables/ssh-ratelimit.nft -- apply that separately if not already active."

#!/bin/bash
# benchmark-demetrius.sh — quantitative baseline for the Kuromatsu native
# provider on the real deploy target (S39/S40, NFR-002/005/006/007/009).
#
# Run ON demetrius (or any systemd/nativellm deploy of this shape) as a user
# with sudo. Produces real, measured numbers — no estimates — for:
#   1. Engine-level threading effect + KV prefix-cache warm-up (nativebench).
#   2. Real per-window prompt overhead (cold CLI calls through pkg/agent).
#   3. Tool-call escalation across a focus-window boundary.
#   4. Legacy PICOCLAW_* env-compat shim (functional regression check).
#   5. Live-gateway KV-cache reuse across repeated same-window calls
#      (heartbeat ticks — the one case that legitimately keeps one process,
#      and therefore one *cgoEngine, alive across repeated calls).
#   6. Resident memory footprint, idle and under the gateway.
#   7. runstate observability surface (run/state file, /health, sysmon).
#
# Suites 2-4 stop the gateway (a one-shot `kuromatsu agent -m` is a *separate*
# process from the systemd-managed daemon, and both loading the model at once
# would double the resident set on a 1 GB box). Suites 5-7 need the gateway
# running. The script restarts it before suite 5 and leaves it running.
#
# Usage: sudo ./benchmark-demetrius.sh [output-dir]
set -uo pipefail

OUT="${1:-/tmp/kuromatsu-bench-$(date +%Y%m%dT%H%M%S)}"
BIN=/opt/kuromatsu/bin/kuromatsu
NATIVEBENCH=/opt/kuromatsu/bin/nativebench
HOME_DIR=/var/lib/kuromatsu
GGUF="$HOME_DIR/models/Bonsai-1.7B-Q1_0.gguf"
RUN_AS="sudo -u kuromatsu env KUROMATSU_HOME=$HOME_DIR"

mkdir -p "$OUT"
echo "== Kuromatsu quantitative benchmark — $(date -Iseconds) =="
echo "output: $OUT"

echo
echo "--- Suite 1: engine-level (nativebench) ---"
for T in 1 2 4; do
  echo "  n_threads=$T ..."
  $RUN_AS "$NATIVEBENCH" -model "$GGUF" -n-ctx 2048 -max-predict 48 -n-threads "$T" -repeat 1 \
    > "$OUT/s1-threads-$T.log" 2>&1
done
echo "  cache warm-up (n_threads=2, repeat=3) ..."
$RUN_AS "$NATIVEBENCH" -model "$GGUF" -n-ctx 2048 -max-predict 48 -n-threads 2 -repeat 3 \
  > "$OUT/s1-cache-warmup.log" 2>&1

echo
echo "--- stopping gateway for suites 2-4 (isolated model access) ---"
systemctl stop kuromatsu
sleep 1

echo
echo "--- Suite 2: per-window prompt overhead (cold CLI calls) ---"
declare -A WINDOW_MSGS=(
  [chat]="Oi, como você está?"
  [files]="Liste os arquivos na pasta atual."
  [shell]="Rode o comando uptime."
  [memory]="Anote que hoje é dia de testes de benchmark."
  [full]="liste os arquivos e rode uptime"
)
for W in "${!WINDOW_MSGS[@]}"; do
  MSG="${WINDOW_MSGS[$W]}"
  echo "  window=$W ..."
  START=$(date +%s.%N)
  $RUN_AS "$BIN" agent -d -s "bench:$W" -m "[foco:$W] $MSG" > "$OUT/s2-window-$W.log" 2>&1
  END=$(date +%s.%N)
  echo "  elapsed=$(echo "$END - $START" | bc)s" >> "$OUT/s2-window-$W.log"
done

echo
echo "--- Suite 3: escalation (chat window asked for an out-of-window tool) ---"
$RUN_AS "$BIN" agent -d -s bench:escalation -m "[foco:chat] rode o comando uptime pra mim" \
  > "$OUT/s3-escalation.log" 2>&1

echo
echo "--- Suite 4: PICOCLAW_* env-compat regression ---"
$RUN_AS env PICOCLAW_TEST_BENCH_VALUE=hello-from-picoclaw "$BIN" agent -d -m "oi" \
  > "$OUT/s4-envcompat.log" 2>&1
grep -c "PICOCLAW_TEST_BENCH_VALUE is deprecated" "$OUT/s4-envcompat.log" > "$OUT/s4-envcompat.result" || true

echo
echo "--- restarting gateway for suites 5-7 ---"
systemctl start kuromatsu
sleep 5
systemctl is-active kuromatsu > "$OUT/s5-gateway-active.result"

echo
echo "--- Suite 5: live-gateway KV-cache reuse (successive heartbeat ticks) ---"
echo "  NOTE: deploy with heartbeat.interval=1 (minute) before running this"
echo "  suite, and restore the production interval afterward -- the default"
echo "  60-minute interval would make this suite wait an hour for tick #2."
echo "  waiting up to 200s for ~3 ticks ..."
journalctl -u kuromatsu --since "-1 minute" -f > "$OUT/s5-heartbeat-live.log" &
JPID=$!
sleep 200
kill "$JPID" 2>/dev/null
grep -E "prompt_tokens|cached_tokens|turn.end" "$OUT/s5-heartbeat-live.log" > "$OUT/s5-heartbeat-summary.log" || true

echo
echo "--- Suite 6: resident memory ---"
systemctl status kuromatsu --no-pager | grep -E "Memory|Active" > "$OUT/s6-memory.log"
free -m >> "$OUT/s6-memory.log"

echo
echo "--- Suite 7: runstate observability ---"
cat "$HOME_DIR/run/state" > "$OUT/s7-run-state.log" 2>&1 || echo "run/state not found" > "$OUT/s7-run-state.log"
curl -sf http://localhost:18790/health > "$OUT/s7-health.json" 2>&1 || echo "health endpoint unreachable" > "$OUT/s7-health.json"

echo
echo "== done: $OUT =="

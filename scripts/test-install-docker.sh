#!/bin/sh
# Real test of scripts/install.sh and `kuromatsu uninstall` against a local
# goreleaser snapshot, in a clean debian:bookworm-slim container, as an
# ordinary user and without a terminal (no `docker run -t`).
#
#   goreleaser release --snapshot --clean --skip=docker,notarize,sign
#   DIST=dist scripts/test-install-docker.sh
#
# DIST is the directory holding the archives and checksums.txt (default:
# dist). `--inside` runs the cases directly in the current HOME; the
# container calls it that way, and DIST then names the archive directory.

set -eu

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

pass() {
	printf 'ok   %s\n' "$*"
}

# install_with BASE_URL [ENV...]: runs the installer with the given base,
# stdin and stdout detached from any terminal; output in $OUT.
install_with() {
	iw_base=$1
	shift
	iw_rc=0
	env KUROMATSU_INSTALL_BASE_URL="$iw_base" "$@" sh "$INSTALLER" </dev/null >"$OUT" 2>&1 || iw_rc=$?
	return "$iw_rc"
}

marker_count() {
	if [ -f "$1" ]; then
		grep -c '# added by kuromatsu installer$' "$1" || true
	else
		echo 0
	fi
}

run_cases() {
	DIST_DIR=${DIST:-/dist}
	INSTALLER=${INSTALLER:-$(dirname "$0")/install.sh}
	BASE="file://$DIST_DIR"
	BIN=$HOME/.local/bin/kuromatsu
	WORK=$(mktemp -d)
	OUT=$WORK/out
	trap 'rm -rf "$WORK"' EXIT
	unset KUROMATSU_HOME KUROMATSU_INSTALL_DIR KUROMATSU_INSTALL_NO_ONBOARD

	case "$(uname -m)" in
	x86_64 | amd64) arch=x86_64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "test runs on x86_64 or arm64 only" ;;
	esac
	asset="kuromatsu_Linux_${arch}.tar.gz"
	[ -f "$DIST_DIR/$asset" ] || fail "$DIST_DIR/$asset not found"
	[ -f "$DIST_DIR/checksums.txt" ] || fail "$DIST_DIR/checksums.txt not found"

	# 1. No terminal: onboard falls back to --non-interactive.
	: >"$HOME/.bash_profile"
	install_with "$BASE" || {
		cat "$OUT"
		fail "case 1: install failed"
	}
	[ -f "$HOME/.kuromatsu/config.json" ] || fail "case 1: onboard did not create ~/.kuromatsu/config.json"
	grep -q 'onboard --non-interactive' "$OUT" || fail "case 1: onboard did not run --non-interactive"
	if grep -qiE '\(y/n\)|\[y/N\]|passphrase' "$OUT"; then
		fail "case 1: output has a prompt"
	fi
	pass "1 install without a terminal runs onboard --non-interactive"

	# 2. Binary works; PATH line in ~/.profile and the existing ~/.bash_profile.
	"$BIN" version >/dev/null 2>&1 || fail "case 2: kuromatsu version failed"
	[ "$(marker_count "$HOME/.profile")" = 1 ] || fail "case 2: ~/.profile has no PATH line"
	[ "$(marker_count "$HOME/.bash_profile")" = 1 ] || fail "case 2: ~/.bash_profile has no PATH line"
	install_with "$BASE" KUROMATSU_INSTALL_NO_ONBOARD=1 || fail "case 2: reinstall failed"
	for f in .profile .bash_profile; do
		[ "$(marker_count "$HOME/$f")" = 1 ] || fail "case 2: reinstall duplicated the line in ~/$f"
	done
	pass "2 version runs; PATH line once in ~/.profile and ~/.bash_profile, also after reinstall"

	# 3. Corrupted archive: refused, nothing installed.
	rm -f "$BIN"
	mirror=$WORK/corrupt
	mkdir -p "$mirror"
	cp "$DIST_DIR/checksums.txt" "$mirror/"
	cp "$DIST_DIR/$asset" "$mirror/"
	printf 'x' >>"$mirror/$asset"
	if install_with "file://$mirror" KUROMATSU_INSTALL_NO_ONBOARD=1; then
		fail "case 3: corrupted archive was accepted"
	fi
	grep -q "checksum mismatch for $asset" "$OUT" || fail "case 3: no checksum mismatch message"
	[ ! -e "$BIN" ] || fail "case 3: binary installed from a corrupted archive"
	pass "3 corrupted archive refused with checksum mismatch, no binary"

	# 4. A line for a longer name with the same prefix does not match.
	mirror=$WORK/sbom
	mkdir -p "$mirror"
	cp "$DIST_DIR/$asset" "$mirror/"
	{
		printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" "$asset.sbom.json"
		cat "$DIST_DIR/checksums.txt"
	} >"$mirror/checksums.txt"
	install_with "file://$mirror" KUROMATSU_INSTALL_NO_ONBOARD=1 || {
		cat "$OUT"
		fail "case 4: install with an extra .sbom.json line failed"
	}
	[ -x "$BIN" ] || fail "case 4: no binary"
	pass "4 checksum picks the exact file name, not $asset.sbom.json"

	# 5. Truncated script: nothing runs.
	rm -f "$BIN"
	cp "$HOME/.profile" "$WORK/profile.before"
	half=$(($(wc -c <"$INSTALLER") / 2))
	head -c "$half" "$INSTALLER" | env KUROMATSU_INSTALL_BASE_URL="$BASE" sh >"$OUT" 2>&1 || true
	[ ! -e "$BIN" ] || fail "case 5: truncated script installed the binary"
	cmp -s "$HOME/.profile" "$WORK/profile.before" || fail "case 5: truncated script changed ~/.profile"
	pass "5 truncated script installs nothing and leaves ~/.profile alone"

	# 6. uninstall --purge --yes: binary and data removed, backup kept.
	install_with "$BASE" KUROMATSU_INSTALL_NO_ONBOARD=1 || fail "case 6: reinstall failed"
	"$BIN" uninstall --purge --yes </dev/null >"$OUT" 2>&1 || {
		cat "$OUT"
		fail "case 6: uninstall --purge --yes failed"
	}
	[ ! -e "$BIN" ] || fail "case 6: binary still there"
	[ ! -e "$HOME/.kuromatsu" ] || fail "case 6: data directory still there"
	ls "$HOME"/kuromatsu-backup-*.zip >/dev/null 2>&1 || fail "case 6: no backup zip"
	[ "$(marker_count "$HOME/.profile")" = 0 ] || fail "case 6: PATH line left in ~/.profile"
	pass "6 uninstall --purge --yes removed binary, data and PATH lines; backup zip kept"

	# 7. A home that is not Kuromatsu's is refused.
	install_with "$BASE" KUROMATSU_INSTALL_NO_ONBOARD=1 || fail "case 7: reinstall failed"
	mkdir -p "$HOME/Documents"
	printf 'mine\n' >"$HOME/Documents/notes.txt"
	ls -la "$HOME/Documents" >"$WORK/docs.before"
	if KUROMATSU_HOME=$HOME/Documents "$BIN" uninstall --purge --yes </dev/null >"$OUT" 2>&1; then
		fail "case 7: purge of ~/Documents was accepted"
	fi
	grep -q 'not a Kuromatsu data directory' "$OUT" || {
		cat "$OUT"
		fail "case 7: wrong refusal"
	}
	ls -la "$HOME/Documents" >"$WORK/docs.after"
	cmp -s "$WORK/docs.before" "$WORK/docs.after" || fail "case 7: ~/Documents changed"
	[ -x "$BIN" ] || fail "case 7: binary removed although the purge was refused"
	pass "7 KUROMATSU_HOME=~/Documents refused, directory intact"

	# 8 and 9. onboard's exit code: 2 (setup left without saving) is a
	# warning and the install succeeds; any other failure is fatal. A stub
	# binary stands in for kuromatsu, in its own release and install dir.
	for want in 2 1; do
		stub=$WORK/stub-$want
		mkdir -p "$stub/rel" "$stub/bin"
		printf '#!/bin/sh\n[ "$1" = onboard ] && exit %s\nexit 0\n' "$want" >"$stub/kuromatsu"
		chmod 755 "$stub/kuromatsu"
		tar -czf "$stub/rel/$asset" -C "$stub" kuromatsu
		(cd "$stub/rel" && sha256sum "$asset" >checksums.txt)
		rc=0
		install_with "file://$stub/rel" KUROMATSU_INSTALL_DIR="$stub/bin" || rc=$?
		if [ "$want" = 2 ]; then
			[ "$rc" = 0 ] || {
				cat "$OUT"
				fail "case 8: onboard exit 2 failed the install (rc=$rc)"
			}
			grep -q "setup skipped; run 'kuromatsu onboard' later" "$OUT" || fail "case 8: no setup-skipped warning"
			[ -x "$stub/bin/kuromatsu" ] || fail "case 8: binary not installed"
			pass "8 onboard exit 2 (setup not completed): warning, install succeeds"
		else
			[ "$rc" != 0 ] || fail "case 9: onboard exit 1 did not fail the install"
			grep -q 'onboard failed' "$OUT" || fail "case 9: no onboard failure message"
			pass "9 onboard exit 1: install fails"
		fi
	done

	printf 'all install cases passed\n'
}

main() {
	if [ "${1:-}" = "--inside" ]; then
		run_cases
		return 0
	fi

	repo=$(cd "$(dirname "$0")/.." && pwd)
	dist=${DIST:-$repo/dist}
	dist=$(cd "$dist" && pwd) || fail "DIST directory not found: $dist"
	command -v docker >/dev/null 2>&1 || fail "docker not found"

	docker run --rm \
		-v "$repo/scripts:/src:ro" \
		-v "$dist:/dist:ro" \
		debian:bookworm-slim \
		sh -c 'useradd -m u && runuser -l u -c "DIST=/dist INSTALLER=/src/install.sh sh /src/test-install-docker.sh --inside"'
}

main "$@"

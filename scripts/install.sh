#!/bin/sh
# Kuromatsu installer for Linux, macOS and Termux (no root, no Go toolchain).
#
#   curl -fsSL https://raw.githubusercontent.com/andre25costa-code/kuromatsu/main/scripts/install.sh | sh
#
# It downloads the release archive for this OS/arch, checks its sha256
# against checksums.txt, installs the binary in a user directory, adds that
# directory to PATH in your shell profiles and runs `kuromatsu onboard`.
#
# Environment:
#   KUROMATSU_INSTALL_BASE_URL    where the release files are; http(s):// or
#                                 file:// (default: the latest GitHub release)
#   KUROMATSU_INSTALL_DIR         install directory (default: ~/.local/bin;
#                                 Termux: $PREFIX/bin)
#   KUROMATSU_INSTALL_NO_ONBOARD  set to 1 to skip `kuromatsu onboard`
#
# Uninstall with `kuromatsu uninstall` (add --purge to delete the data too).
#
# Everything below is a function and the last line calls main: if the
# download is cut short, the shell never reaches a partial command.

set -eu

say() {
	printf '%s\n' "$*"
}

die() {
	printf 'kuromatsu install: %s\n' "$*" >&2
	exit 1
}

# detect_platform sets OS, ARCH and TERMUX.
detect_platform() {
	TERMUX=0
	case "${PREFIX:-}" in
	*com.termux*) TERMUX=1 ;;
	esac
	machine=$(uname -m)

	if [ "$TERMUX" = 1 ]; then
		OS=Android
		case "$machine" in
		aarch64 | arm64) ARCH=arm64 ;;
		*) die "unsupported OS/arch: Termux on $machine (only aarch64 is built)" ;;
		esac
		return 0
	fi

	kernel=$(uname -s)
	case "$kernel" in
	Linux) OS=Linux ;;
	Darwin) OS=Darwin ;;
	*) die "unsupported OS/arch: $kernel/$machine" ;;
	esac
	case "$machine" in
	x86_64 | amd64) ARCH=x86_64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	armv7l) ARCH=armv7 ;;
	armv6l) ARCH=armv6 ;;
	riscv64) ARCH=riscv64 ;;
	*) die "unsupported OS/arch: $OS/$machine" ;;
	esac
}

# download URL DEST: returns 0 on success, 2 when the server answered 404,
# 1 on any other failure.
download() {
	dl_url=$1
	dl_dest=$2
	case "$dl_url" in
	file://*)
		if command -v curl >/dev/null 2>&1; then
			curl -fsSL -o "$dl_dest" "$dl_url" || return 1
		else
			cp "${dl_url#file://}" "$dl_dest" || return 1
		fi
		;;
	http://* | https://*)
		if command -v curl >/dev/null 2>&1; then
			dl_code=$(curl -sSL -o "$dl_dest" -w '%{http_code}' "$dl_url") || return 1
			case "$dl_code" in
			2??) ;;
			404) return 2 ;;
			*) return 1 ;;
			esac
		elif command -v wget >/dev/null 2>&1; then
			wget -q -O "$dl_dest" "$dl_url" || return 1
		else
			die "need curl or wget to download $dl_url"
		fi
		;;
	*)
		die "KUROMATSU_INSTALL_BASE_URL must start with http://, https:// or file:// (got $dl_url)"
		;;
	esac
}

# fetch NAME: downloads $BASE_URL/NAME into $TMP_DIR or exits.
fetch() {
	fetch_rc=0
	download "$BASE_URL/$1" "$TMP_DIR/$1" || fetch_rc=$?
	case "$fetch_rc" in
	0) ;;
	2)
		if [ "$BASE_URL" = "$DEFAULT_BASE_URL" ]; then
			die "no release published yet; build from source (README) or set KUROMATSU_INSTALL_BASE_URL"
		fi
		die "not found: $BASE_URL/$1"
		;;
	*) die "download failed: $BASE_URL/$1" ;;
	esac
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

# verify ASSET: the archive's sha256 must equal the single checksums.txt
# line whose file field is exactly ASSET.
verify() {
	asset=$1
	expected=$(awk -v f="$asset" '$2==f || $2==("*" f) {print $1}' "$TMP_DIR/checksums.txt")
	case "$expected" in
	"" | *"
"*)
		# no line, or more than one
		die "checksum mismatch for $asset"
		;;
	*[!0-9a-f]*) die "checksum mismatch for $asset" ;;
	esac
	[ "${#expected}" -eq 64 ] || die "checksum mismatch for $asset"

	if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
		die "need sha256sum or shasum to check the download"
	fi
	actual=$(sha256_of "$TMP_DIR/$asset")
	[ "$actual" = "$expected" ] || die "checksum mismatch for $asset"
}

# check_space DIR FILE: DIR needs 3x the size of FILE free.
check_space() {
	size_bytes=$(wc -c <"$2" | tr -d ' ')
	need_kb=$((size_bytes * 3 / 1024 + 1))
	avail_kb=$(df -Pk "$1" 2>/dev/null | awk 'NR==2 {print $4}')
	case "$avail_kb" in
	"" | *[!0-9]*)
		say "warning: could not read the free space of $1; continuing"
		return 0
		;;
	esac
	if [ "$avail_kb" -lt "$need_kb" ]; then
		die "not enough free space in $1: need ${need_kb} KiB, available ${avail_kb} KiB"
	fi
}

# install_bin ASSET DIR: extracts the binary and moves it into DIR
# atomically.
install_bin() {
	tar -xzf "$TMP_DIR/$1" -C "$TMP_DIR" kuromatsu || die "could not extract kuromatsu from $1"
	# Stage next to the target and rename, so a failed copy never leaves a
	# truncated binary; a failed stage is removed.
	if ! { cp "$TMP_DIR/kuromatsu" "$2/.kuromatsu.new" &&
		chmod 755 "$2/.kuromatsu.new" &&
		mv -f "$2/.kuromatsu.new" "$2/kuromatsu"; }; then
		rm -f "$2/.kuromatsu.new"
		die "could not install $2/kuromatsu"
	fi
}

# add_line FILE: appends PATH_LINE to FILE unless it is already there.
add_line() {
	if [ -f "$1" ] && grep -qxF "$PATH_LINE" "$1"; then
		return 0
	fi
	# Keep the last line of the file intact.
	if [ -s "$1" ] && [ -n "$(tail -c 1 "$1")" ]; then
		printf '\n' >>"$1"
	fi
	printf '%s\n' "$PATH_LINE" >>"$1"
	say "Added $DEST_DIR to PATH in $1"
}

# update_path DIR: sets PATH_CHANGED=1 when DIR was added to the profiles.
update_path() {
	PATH_CHANGED=0
	case ":$PATH:" in
	*":$1:"*) return 0 ;;
	esac

	PATH_LINE="export PATH=\"$1:\$PATH\" # added by kuromatsu installer"
	add_line "$HOME/.profile"
	# A login bash reads the first of these instead of ~/.profile.
	for f in .bash_profile .bash_login .bashrc; do
		if [ -f "$HOME/$f" ]; then
			add_line "$HOME/$f"
		fi
	done
	# zsh (the macOS default) never reads ~/.profile.
	if [ -f "$HOME/.zshrc" ]; then
		add_line "$HOME/.zshrc"
	else
		case "${SHELL:-}" in
		*zsh) add_line "$HOME/.zshrc" ;;
		esac
	fi
	PATH_CHANGED=1
}

run_onboard() {
	if [ "${KUROMATSU_INSTALL_NO_ONBOARD:-}" = 1 ]; then
		say "Skipping onboard (KUROMATSU_INSTALL_NO_ONBOARD=1); run 'kuromatsu onboard' later."
		return 0
	fi
	# Under `curl | sh` stdin is the script, so a prompt must read the
	# terminal. Test that /dev/tty really opens: in a container without a
	# terminal it exists but opening it fails.
	onboard_rc=0
	if [ -t 1 ] && (exec </dev/tty) 2>/dev/null; then
		say "Running kuromatsu onboard"
		"$1" onboard </dev/tty || onboard_rc=$?
	else
		say "Running kuromatsu onboard --non-interactive (no terminal)"
		"$1" onboard --non-interactive || onboard_rc=$?
	fi
	# onboard exits 2 when the user leaves setup without saving (nothing
	# was written): the install itself still succeeded.
	case "$onboard_rc" in
	0) ;;
	2) say "warning: setup skipped; run 'kuromatsu onboard' later" ;;
	*) die "onboard failed; run 'kuromatsu onboard' again" ;;
	esac
}

main() {
	DEFAULT_BASE_URL=https://github.com/andre25costa-code/kuromatsu/releases/latest/download
	BASE_URL=${KUROMATSU_INSTALL_BASE_URL:-$DEFAULT_BASE_URL}
	BASE_URL=${BASE_URL%/}

	detect_platform

	if [ -n "${KUROMATSU_INSTALL_DIR:-}" ]; then
		DEST_DIR=$KUROMATSU_INSTALL_DIR
	elif [ "$TERMUX" = 1 ]; then
		DEST_DIR=$PREFIX/bin
	else
		DEST_DIR=$HOME/.local/bin
	fi
	case "$DEST_DIR" in
	/*) ;;
	*) DEST_DIR=$(pwd)/$DEST_DIR ;;
	esac

	asset="kuromatsu_${OS}_${ARCH}.tar.gz"
	TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t kuromatsu)
	trap 'rm -rf "$TMP_DIR"' EXIT
	trap 'rm -rf "$TMP_DIR"; exit 1' HUP INT TERM

	say "Downloading $asset from $BASE_URL"
	fetch "$asset"
	fetch checksums.txt
	verify "$asset"

	mkdir -p "$DEST_DIR"
	check_space "$DEST_DIR" "$TMP_DIR/$asset"
	install_bin "$asset" "$DEST_DIR"
	bin=$DEST_DIR/kuromatsu
	say "Installed $bin"

	update_path "$DEST_DIR"
	run_onboard "$bin"

	say ""
	say "kuromatsu is installed at $bin"
	"$bin" version || true
	if [ "$PATH_CHANGED" = 1 ]; then
		say ""
		say "Open a new terminal, or run: . ~/.profile"
		say "fish users: fish_add_path $DEST_DIR"
	fi
}

main "$@"

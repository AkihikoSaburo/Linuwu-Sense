#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
ACTION=install
SCOPE=all
PREFIX=/usr/local
NO_ENABLE=0
SKIP_TESTS=0
YES=0
ACTION_SET=0
SCOPE_SET=0

if [[ -t 1 ]]; then
	COLOR_GREEN=$'\033[32m'
	COLOR_YELLOW=$'\033[33m'
	COLOR_RED=$'\033[31m'
	COLOR_RESET=$'\033[0m'
else
	COLOR_GREEN=''
	COLOR_YELLOW=''
	COLOR_RED=''
	COLOR_RESET=''
fi

info() { printf '%s\n' "${COLOR_GREEN}info:${COLOR_RESET} $*"; }
warn() { printf '%s\n' "${COLOR_YELLOW}warning:${COLOR_RESET} $*" >&2; }
error() { printf '%s\n' "${COLOR_RED}error:${COLOR_RESET} $*" >&2; }
fail() { error "$*"; exit 1; }

usage() {
	cat <<'EOF'
Usage: ./setup.sh [OPTIONS]

Install or remove the Linuwu Sense kernel module and nitrod daemon.
Run as a regular user; privileged system operations use sudo.

Options:
  --module-only    Install/uninstall only the kernel module
  --daemon-only    Install/uninstall only nitrod
  --uninstall      Uninstall daemon first, then kernel module
  --purge          Uninstall and purge daemon config/state/group; asks first
  --prefix DIR     nitrod installation prefix (default: /usr/local)
  --no-enable      Do not enable nitrod; restart only if already active
  --skip-tests     Skip nitrod Go tests
  --yes            Skip purge confirmation (only applies to --purge)
  -h, --help       Show this help
EOF
}

while (($# > 0)); do
	case "$1" in
		--module-only)
			((SCOPE_SET == 0)) || fail '--module-only and --daemon-only cannot be combined'
			SCOPE=module
			SCOPE_SET=1
			shift
			;;
		--daemon-only)
			((SCOPE_SET == 0)) || fail '--module-only and --daemon-only cannot be combined'
			SCOPE=daemon
			SCOPE_SET=1
			shift
			;;
		--uninstall)
			((ACTION_SET == 0)) || fail '--uninstall and --purge cannot be combined'
			ACTION=uninstall
			ACTION_SET=1
			shift
			;;
		--purge)
			((ACTION_SET == 0)) || fail '--uninstall and --purge cannot be combined'
			ACTION=purge
			ACTION_SET=1
			shift
			;;
		--prefix)
			(($# >= 2)) || fail '--prefix requires a directory argument'
			PREFIX=$2
			[[ -n "$PREFIX" ]] || fail '--prefix must not be empty'
			shift 2
			;;
		--no-enable)
			NO_ENABLE=1
			shift
			;;
		--skip-tests)
			SKIP_TESTS=1
			shift
			;;
		--yes)
			YES=1
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*) fail "unknown option: $1" ;;
	esac
done

if [[ "$SCOPE" == module && "$ACTION" == purge ]]; then
	fail '--purge includes daemon config/state/group; --module-only is not valid with --purge'
fi
if ((YES != 0)) && [[ "$ACTION" != purge ]]; then
	fail '--yes is only valid with --purge'
fi
if ((NO_ENABLE != 0)) && [[ "$ACTION" != install ]]; then
	fail '--no-enable is only valid during installation'
fi

retry_invocation() {
	local args=()
	if [[ "$SCOPE" == module ]]; then args+=(--module-only); fi
	if [[ "$SCOPE" == daemon ]]; then args+=(--daemon-only); fi
	if [[ "$ACTION" == uninstall ]]; then args+=(--uninstall); fi
	if [[ "$ACTION" == purge ]]; then args+=(--purge); fi
	if [[ "$PREFIX" != /usr/local ]]; then args+=(--prefix "$PREFIX"); fi
	if ((NO_ENABLE == 1)); then args+=(--no-enable); fi
	if ((SKIP_TESTS == 1)); then args+=(--skip-tests); fi
	if ((YES == 1)); then args+=(--yes); fi
	local argument
	printf 'bash %q' "$ROOT_DIR/setup.sh"
	for argument in "${args[@]}"; do printf ' %q' "$argument"; done
}
RETRY_COMMAND=$(retry_invocation)

if ((EUID == 0)); then
	if [[ -n "${SUDO_USER:-}" ]]; then
		fail 'Run as a regular user; do not invoke setup.sh through sudo because building and signing require the user HOME directory'
	fi
	fail 'Run as a regular user, not root'
fi

if ! command -v sudo >/dev/null 2>&1; then
	fail "Pre-check failed: sudo not found. Installation examples: apt install sudo; pacman -S sudo; dnf install sudo. Once available, retry: $RETRY_COMMAND"
fi
sudo -v || fail "Pre-check failed: sudo authentication failed. Check sudo access, then retry: $RETRY_COMMAND"

check_command() {
	local command_name=$1
	local description=$2
	local apt_command=$3
	local pacman_command=$4
	local dnf_command=$5
	if ! command -v "$command_name" >/dev/null 2>&1; then
		fail "Pre-check failed: $description not found. Installation examples: $apt_command (apt), $pacman_command (pacman), or $dnf_command (dnf). After installing it, retry: $RETRY_COMMAND"
	fi
}

check_make() {
	check_command make 'make' 'sudo apt install build-essential' 'sudo pacman -S make' 'sudo dnf install make'
}

check_module_prerequisites() {
	check_command dkms 'DKMS' 'sudo apt install dkms' 'sudo pacman -S dkms' 'sudo dnf install dkms'
	local kernel_release
	kernel_release=$(uname -r)
	if [[ ! -d "/lib/modules/$kernel_release/build" ]]; then
		fail "Pre-check failed: kernel headers/build directory for $kernel_release not found at /lib/modules/$kernel_release/build. Examples: sudo apt install linux-headers-$kernel_release; sudo pacman -S linux-headers; sudo dnf install kernel-devel-$kernel_release. After installing them, retry: $RETRY_COMMAND"
	fi
}

check_systemd() {
	check_command systemctl 'systemd systemctl' 'sudo apt install systemd' 'sudo pacman -S systemd' 'sudo dnf install systemd'
	check_command systemd-sysusers 'systemd-sysusers' 'sudo apt install systemd' 'sudo pacman -S systemd' 'sudo dnf install systemd'
}

check_go_version() {
	check_command go 'Go' 'sudo apt install golang-go' 'sudo pacman -S go' 'sudo dnf install golang'
	local required current required_major required_minor current_major current_minor
	required=$(awk '$1 == "go" { print $2; exit }' "$ROOT_DIR/nitrod/go.mod")
	[[ -n "$required" ]] || fail "Pre-check failed: Go version not found in nitrod/go.mod; retry: $RETRY_COMMAND"
	current=$(go env GOVERSION) || fail "Pre-check failed: could not read Go version; retry: $RETRY_COMMAND"
	[[ "$required" =~ ^([0-9]+)\.([0-9]+) ]] || fail "Pre-check failed: unrecognized go.mod version format: $required; retry: $RETRY_COMMAND"
	required_major=${BASH_REMATCH[1]}
	required_minor=${BASH_REMATCH[2]}
	[[ "$current" =~ ^go([0-9]+)\.([0-9]+) ]] || fail "Pre-check failed: unrecognized installed Go version format: $current; retry: $RETRY_COMMAND"
	current_major=${BASH_REMATCH[1]}
	current_minor=${BASH_REMATCH[2]}
	if ((current_major < required_major || (current_major == required_major && current_minor < required_minor))); then
		fail "Pre-check failed: Go >= $required is required; installed version is $current. Examples: sudo apt install golang-go; sudo pacman -S go; sudo dnf install golang. After upgrading Go, retry: $RETRY_COMMAND"
	fi
}

is_module_scope() { [[ "$SCOPE" != daemon ]]; }
is_daemon_scope() { [[ "$SCOPE" != module ]]; }

if [[ "$ACTION" == install ]]; then
	check_command bash 'bash' 'sudo apt install bash' 'sudo pacman -S bash' 'sudo dnf install bash'
	check_make
	if is_module_scope; then check_module_prerequisites; fi
	if is_daemon_scope; then
		check_go_version
		check_systemd
	fi
else
	check_make
	if is_daemon_scope; then check_systemd; fi
fi

if [[ "$ACTION" == purge && "$YES" -eq 0 ]]; then
	if [[ ! -t 0 ]]; then
		fail '--purge requires a terminal for confirmation; use --yes to confirm the removal explicitly'
	fi
	printf 'Purge will remove nitrod configuration/state and the nitrod group. Continue? [y/N] '
	if ! read -r answer; then
		info 'Purge cancelled (no confirmation input).'
		exit 0
	fi
	case "$answer" in
		y|Y|yes|YES) ;;
		*) info 'Purge cancelled'; exit 0 ;;
	esac
fi

run_step() {
	local label=$1
	local retry_command=$2
	shift 2
	info "$label"
	if "$@"; then
		return 0
	else
		local result=$?
	fi
	error "$label failed (exit $result). Fix the issue, then retry this step: $retry_command"
	return "$result"
}

secure_boot_warning() {
	if ! command -v mokutil >/dev/null 2>&1; then return 0; fi
	local result
	result=$(mokutil --sb-state 2>/dev/null || true)
	if grep -Eiq 'secureboot enabled|secure boot enabled' <<<"$result"; then
		warn 'Secure Boot is enabled. DKMS must sign the linuwu_sense module, and the signing key must be enrolled.'
	fi
}

install_module() {
	secure_boot_warning
	run_step 'Installing kernel module through the root Makefile' 'make install' make -C "$ROOT_DIR" install || return $?
	local sysfs_base=/sys/module/linuwu_sense/drivers/platform:acer-wmi/acer-wmi
	local attempt
	info 'Waiting for nitro_sense/predator_sense sysfs groups (10-second timeout)'
	for ((attempt = 0; attempt < 10; attempt += 1)); do
		if [[ -d "$sysfs_base/nitro_sense" || -d "$sysfs_base/predator_sense" ]]; then
			info 'linuwu_sense driver is ready'
			return 0
		fi
		sleep 1
	done
	if [[ -d "$sysfs_base/nitro_sense" || -d "$sysfs_base/predator_sense" ]]; then return 0; fi
	error 'The module is installed, but the nitro_sense/predator_sense sysfs groups did not appear within 10 seconds. The daemon will not be installed.'
	error 'Check dmesg, `dkms status`, acer_wmi conflicts, Secure Boot, and key enrollment.'
	error "After fixing the issue, retry the module step: bash \"$ROOT_DIR/setup.sh\" --module-only"
	return 1
}

user_has_nitrod_group() {
	id -nG "$(id -un)" | tr ' ' '\n' | grep -Fxq nitrod
}

warn_after_install() {
	if command -v lsmod >/dev/null 2>&1 && lsmod | awk 'NR > 1 { print $1 }' | grep -Fxq acer_wmi; then
		warn 'acer_wmi is still loaded; reboot or regenerate the initramfs for the blacklist to take effect.'
	fi
	if systemctl is-active --quiet power-profiles-daemon.service; then
		warn 'power-profiles-daemon is active and may conflict with platform_profile; consider `sudo systemctl mask power-profiles-daemon` (not run automatically).'
	fi
}

install_daemon() {
	local had_group=0
	if user_has_nitrod_group; then had_group=1; fi
	local retry_prefix="sudo make -C $ROOT_DIR/nitrod install PREFIX=$PREFIX"
	local make_args=("PREFIX=$PREFIX")
	if ((NO_ENABLE == 1)); then make_args+=(ENABLE=0); retry_prefix+=" ENABLE=0"; fi

	run_step 'Build daemon (regular user)' "make -C $ROOT_DIR/nitrod build" make -C "$ROOT_DIR/nitrod" build || return $?
	if ((SKIP_TESTS == 0)); then
		run_step 'Test daemon (go vet and race tests)' "make -C $ROOT_DIR/nitrod test" make -C "$ROOT_DIR/nitrod" test || return $?
	else
		warn 'Go tests skipped as requested.'
	fi
	run_step 'Create the nitrod system group from sysusers configuration' "sudo systemd-sysusers --inline 'g nitrod -'" sudo systemd-sysusers --inline 'g nitrod -' || return $?
	if ! getent group nitrod >/dev/null 2>&1; then
		error "The nitrod group is still unavailable after systemd-sysusers. Retry this step: sudo systemd-sysusers --inline 'g nitrod -'"
		return 1
	fi
	run_step 'Install daemon' "$retry_prefix" sudo make -C "$ROOT_DIR/nitrod" install "${make_args[@]}" || return $?

	if ! user_has_nitrod_group; then
		run_step 'Add the user to the nitrod group' "sudo usermod -aG nitrod $(id -un)" sudo usermod -aG nitrod "$(id -un)" || return $?
		warn 'The user was added to the nitrod group; log out and back in or run `newgrp nitrod`.'
	fi
	warn_after_install
}

uninstall_components() {
	local failed=0
	if is_daemon_scope; then
		local daemon_target=uninstall
		local daemon_retry="sudo make -C $ROOT_DIR/nitrod uninstall PREFIX=$PREFIX"
		if [[ "$ACTION" == purge ]]; then
			daemon_target=purge
			daemon_retry="sudo make -C $ROOT_DIR/nitrod purge PREFIX=$PREFIX"
		fi
		if ! sudo make -C "$ROOT_DIR/nitrod" "$daemon_target" "PREFIX=$PREFIX"; then
			error "Daemon uninstall failed; continuing with the kernel module. Retry only the daemon with: $daemon_retry"
			failed=1
		fi
	fi
	if is_module_scope; then
		if ! make -C "$ROOT_DIR" uninstall; then
			error "Kernel module uninstall failed. Retry only the module with: bash \"$ROOT_DIR/setup.sh\" --module-only --uninstall"
			failed=1
		fi
	fi
	return "$failed"
}

case "$ACTION" in
	install)
		if is_module_scope; then install_module || fail "Module step failed. Retry: bash \"$ROOT_DIR/setup.sh\" --module-only"; fi
		if is_daemon_scope; then install_daemon || fail 'Daemon step failed. Retry daemon build/test/install using the commands above.'; fi
		info 'Installation complete.'
		if is_module_scope; then
			printf '\nVerifikasi modul:\n  dkms status\n  lsmod | grep -E "linuwu|acer_wmi"\n'
		fi
		if is_daemon_scope; then
			cat <<'EOF'
Daemon verification:
  sudo systemctl status nitrod
  sudo journalctl -u nitrod -n 20

Socket test (requires socat):
  printf '%s\n' '{"id":1,"cmd":"get_state"}' | socat - UNIX-CONNECT:/run/nitrod.sock
EOF
		fi
		;;
	uninstall|purge)
		if ! uninstall_components; then
			fail 'One or more uninstall steps failed; see the specific retry commands above.'
		fi
		info 'Uninstall complete.'
		;;
esac

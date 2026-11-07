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
		fail 'jalankan sebagai user biasa; jangan panggil setup.sh melalui sudo karena build dan signing memerlukan HOME user'
	fi
	fail 'jalankan sebagai user biasa, bukan root'
fi

if ! command -v sudo >/dev/null 2>&1; then
	fail "Pre-check gagal: sudo tidak ditemukan. Contoh pemasangan: apt install sudo; pacman -S sudo; dnf install sudo. Setelah tersedia, ulangi: $RETRY_COMMAND"
fi
sudo -v || fail "Pre-check gagal: autentikasi sudo gagal. Periksa akses sudo, lalu ulangi: $RETRY_COMMAND"

check_command() {
	local command_name=$1
	local description=$2
	local apt_command=$3
	local pacman_command=$4
	local dnf_command=$5
	if ! command -v "$command_name" >/dev/null 2>&1; then
		fail "Pre-check gagal: $description tidak ditemukan. Contoh pemasangan: $apt_command (apt), $pacman_command (pacman), atau $dnf_command (dnf). Setelah memasangnya, ulangi: $RETRY_COMMAND"
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
		fail "Pre-check gagal: header/build directory untuk kernel $kernel_release tidak ditemukan di /lib/modules/$kernel_release/build. Contoh: sudo apt install linux-headers-$kernel_release; sudo pacman -S linux-headers; sudo dnf install kernel-devel-$kernel_release. Setelah memasangnya, ulangi: $RETRY_COMMAND"
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
	[[ -n "$required" ]] || fail "Pre-check gagal: versi Go tidak ditemukan di nitrod/go.mod; ulangi: $RETRY_COMMAND"
	current=$(go env GOVERSION) || fail "Pre-check gagal membaca versi Go; ulangi: $RETRY_COMMAND"
	[[ "$required" =~ ^([0-9]+)\.([0-9]+) ]] || fail "Pre-check gagal: format versi go.mod tidak dikenali: $required; ulangi: $RETRY_COMMAND"
	required_major=${BASH_REMATCH[1]}
	required_minor=${BASH_REMATCH[2]}
	[[ "$current" =~ ^go([0-9]+)\.([0-9]+) ]] || fail "Pre-check gagal: format Go terpasang tidak dikenali: $current; ulangi: $RETRY_COMMAND"
	current_major=${BASH_REMATCH[1]}
	current_minor=${BASH_REMATCH[2]}
	if ((current_major < required_major || (current_major == required_major && current_minor < required_minor))); then
		fail "Pre-check gagal: Go >= $required diperlukan, versi terpasang $current. Contoh: sudo apt install golang-go; sudo pacman -S go; sudo dnf install golang. Setelah upgrade Go, ulangi: $RETRY_COMMAND"
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
		fail '--purge memerlukan terminal untuk konfirmasi; gunakan --yes jika penghapusan memang dikehendaki'
	fi
	printf 'Purge akan menghapus konfigurasi/state nitrod dan grup nitrod. Lanjutkan? [y/N] '
	if ! read -r answer; then
		info 'Purge dibatalkan (tidak ada input konfirmasi).'
		exit 0
	fi
	case "$answer" in
		y|Y|yes|YES) ;;
		*) info 'Purge dibatalkan'; exit 0 ;;
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
	error "$label gagal (exit $result). Perbaiki masalah lalu ulangi langkah ini: $retry_command"
	return "$result"
}

secure_boot_warning() {
	if ! command -v mokutil >/dev/null 2>&1; then return 0; fi
	local result
	result=$(mokutil --sb-state 2>/dev/null || true)
	if grep -Eiq 'secureboot enabled|secure boot enabled' <<<"$result"; then
		warn 'Secure Boot aktif. DKMS harus menandatangani modul linuwu_sense dan key penandatangan harus sudah di-enroll.'
	fi
}

install_module() {
	secure_boot_warning
	run_step 'Memasang modul kernel melalui Makefile root' 'make install' make -C "$ROOT_DIR" install || return $?
	local sysfs_base=/sys/module/linuwu_sense/drivers/platform:acer-wmi/acer-wmi
	local attempt
	info 'Menunggu sysfs nitro_sense/predator_sense, timeout 10 detik'
	for ((attempt = 0; attempt < 10; attempt += 1)); do
		if [[ -d "$sysfs_base/nitro_sense" || -d "$sysfs_base/predator_sense" ]]; then
			info 'Driver linuwu_sense siap'
			return 0
		fi
		sleep 1
	done
	if [[ -d "$sysfs_base/nitro_sense" || -d "$sysfs_base/predator_sense" ]]; then return 0; fi
	error 'Modul terpasang tetapi grup sysfs nitro_sense/predator_sense tidak tersedia setelah 10 detik. Daemon tidak dipasang.'
	error 'Periksa dmesg, `dkms status`, konflik acer_wmi, Secure Boot, dan enrollment key.'
	error "Ulangi langkah modul setelah memperbaiki penyebabnya: bash \"$ROOT_DIR/setup.sh\" --module-only"
	return 1
}

user_has_nitrod_group() {
	id -nG "$(id -un)" | tr ' ' '\n' | grep -Fxq nitrod
}

warn_after_install() {
	if command -v lsmod >/dev/null 2>&1 && lsmod | awk 'NR > 1 { print $1 }' | grep -Fxq acer_wmi; then
		warn 'acer_wmi masih termuat; reboot atau regenerate initramfs agar blacklist berlaku.'
	fi
	if systemctl is-active --quiet power-profiles-daemon.service; then
		warn 'power-profiles-daemon aktif dan dapat konflik dengan platform_profile; pertimbangkan `sudo systemctl mask power-profiles-daemon` (tidak dijalankan otomatis).'
	fi
}

install_daemon() {
	local had_group=0
	if user_has_nitrod_group; then had_group=1; fi
	local retry_prefix="sudo make -C $ROOT_DIR/nitrod install PREFIX=$PREFIX"
	local make_args=("PREFIX=$PREFIX")
	if ((NO_ENABLE == 1)); then make_args+=(ENABLE=0); retry_prefix+=" ENABLE=0"; fi

	run_step 'Build daemon (user biasa)' "make -C $ROOT_DIR/nitrod build" make -C "$ROOT_DIR/nitrod" build || return $?
	if ((SKIP_TESTS == 0)); then
		run_step 'Tes daemon (go vet dan race)' "make -C $ROOT_DIR/nitrod test" make -C "$ROOT_DIR/nitrod" test || return $?
	else
		warn 'Tes Go dilewati atas permintaan.'
	fi
	run_step 'Pasang daemon' "$retry_prefix" sudo make -C "$ROOT_DIR/nitrod" install "${make_args[@]}" || return $?

	if ! user_has_nitrod_group; then
		run_step 'Tambahkan user ke grup nitrod' "sudo usermod -aG nitrod $(id -un)" sudo usermod -aG nitrod "$(id -un)" || return $?
		warn 'User baru ditambahkan ke grup nitrod; logout/login kembali atau jalankan `newgrp nitrod`.'
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
			error "Uninstall daemon gagal; tetap melanjutkan ke modul kernel. Ulangi daemon saja dengan: $daemon_retry"
			failed=1
		fi
	fi
	if is_module_scope; then
		if ! make -C "$ROOT_DIR" uninstall; then
			error "Uninstall modul kernel gagal. Ulangi modul saja dengan: bash \"$ROOT_DIR/setup.sh\" --module-only --uninstall"
			failed=1
		fi
	fi
	return "$failed"
}

case "$ACTION" in
	install)
		if is_module_scope; then install_module || fail "Langkah modul gagal. Ulangi: bash \"$ROOT_DIR/setup.sh\" --module-only"; fi
		if is_daemon_scope; then install_daemon || fail "Langkah daemon gagal. Ulangi daemon build/test/install menggunakan perintah di atas."; fi
		info 'Instalasi selesai.'
		if is_module_scope; then
			printf '\nVerifikasi modul:\n  dkms status\n  lsmod | grep -E "linuwu|acer_wmi"\n'
		fi
		if is_daemon_scope; then
			cat <<'EOF'
Verifikasi daemon:
  sudo systemctl status nitrod
  sudo journalctl -u nitrod -n 20

Tes socket (memerlukan socat):
  printf '%s\n' '{"id":1,"cmd":"get_state"}' | socat - UNIX-CONNECT:/run/nitrod.sock
EOF
		fi
		;;
	uninstall|purge)
		if ! uninstall_components; then
			fail 'Satu atau lebih langkah uninstall gagal; lihat perintah pengulangan spesifik di atas.'
		fi
		info 'Uninstall selesai.'
		;;
esac

#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../../../.." && pwd)
CACHE_ROOT=${E2E_CACHE_ROOT:-$REPO_ROOT/.cache/luci-e2e}
IMAGE_NAME=openwrt-23.05.5-x86-64-generic-ext4-combined.img.gz
IMAGE_URL=https://downloads.openwrt.org/releases/23.05.5/targets/x86/64/$IMAGE_NAME
IMAGE_SHA256=f5c77659a33bd43cba105cf2f75e56d054fa9e0b9a73dc9f2a5bcb0e126ff7d5
RAW_SHA256=5ba1a78dbca0cf94b2e5682f95a5890ee2dd9e4616cb3fab7c37ff21d2f7838e
PROVISION_SCHEMA=3
PACKAGE_MANIFEST='luci-base=git-24.264.56413-c7a3562
luci-mod-admin-full=git-19.253.48496-3f93650
luci-theme-bootstrap=git-24.086.46634-1ffe078
rpcd-mod-file=2023-07-01-c07ab2f9-1
uhttpd=2023-06-25-34a8a74d-2
uhttpd-mod-ubus=2023-06-25-34a8a74d-2'
FINGERPRINT=$(printf '%s\n%s\n%s\n' "$IMAGE_SHA256" "$PROVISION_SCHEMA" "$PACKAGE_MANIFEST" | sha256sum | awk '{print $1}')
BASE_DIR=$CACHE_ROOT/base/$FINGERPRINT
BASE_IMAGE=$BASE_DIR/openwrt-luci-base.qcow2
READY_FILE=$BASE_DIR/ready
DOWNLOAD_DIR=$CACHE_ROOT/downloads
RAW_IMAGE=$DOWNLOAD_DIR/openwrt-23.05.5-x86-64.raw
KEY_DIR=$CACHE_ROOT/ssh
SSH_KEY=$KEY_DIR/id_ed25519
WORK_DIR=$CACHE_ROOT/p/$$
SSH_PORT=${E2E_PROVISION_SSH_PORT:-22233}
HTTP_PORT=${E2E_PROVISION_HTTP_PORT:-28083}
QEMU_PID=

log() { printf 'prepare-base: %s\n' "$*" >&2; }
die() { log "$*"; exit 1; }

require() {
	command -v "$1" >/dev/null 2>&1 || die "missing host prerequisite: $1"
}

ssh_base() {
	ssh -i "$SSH_KEY" -p "$SSH_PORT" -o BatchMode=yes -o StrictHostKeyChecking=no \
		-o UserKnownHostsFile="$WORK_DIR/known_hosts" -o ConnectTimeout=2 root@127.0.0.1 "$@"
}

owned_qemu_running() {
	[ -n "$QEMU_PID" ] && kill -0 "$QEMU_PID" 2>/dev/null
}

stop_owned_qemu() {
	owned_qemu_running || return 0
	case "$QEMU_PID" in ''|*[!0-9]*) die "invalid owned QEMU PID" ;; esac
	cmdline=$(tr '\0' ' ' < "/proc/$QEMU_PID/cmdline" 2>/dev/null || true)
	case "$cmdline" in *"$WORK_DIR/router.qcow2"*) ;; *) die "refusing to stop non-owned PID $QEMU_PID" ;; esac
	kill -TERM "$QEMU_PID"
	for _attempt in $(seq 1 20); do
		kill -0 "$QEMU_PID" 2>/dev/null || return 0
		sleep 1
	done
	die "owned QEMU did not terminate"
}

cleanup() {
	stop_owned_qemu || true
}
trap cleanup EXIT HUP INT TERM

for tool in curl gzip sha256sum qemu-img qemu-system-x86_64 ssh ssh-keygen nc awk; do require "$tool"; done

# The base image contains the public half of the disposable guest-login key.
# If CI restores the public-key-bearing base without restoring the private key,
# rebuild the explicit base target with a fresh key instead of persisting that
# private key in a cross-run cache.
if [ -r "$READY_FILE" ] && [ -r "$BASE_IMAGE" ] && [ ! -r "$SSH_KEY" ]; then
	log "base cache has no disposable SSH key; rebuilding the fingerprinted base"
	rm -f "$READY_FILE" "$BASE_IMAGE"
fi

if [ -r "$READY_FILE" ] && [ -r "$BASE_IMAGE" ]; then
	[ "$(cat "$READY_FILE")" = "$FINGERPRINT" ] || die "base marker mismatch"
	if [ "${1:-}" = "--print-path" ]; then printf '%s\n' "$BASE_IMAGE"; fi
	exit 0
fi

mkdir -p "$DOWNLOAD_DIR" "$BASE_DIR" "$KEY_DIR" "$WORK_DIR"
chmod 700 "$CACHE_ROOT" "$KEY_DIR" "$WORK_DIR"

if [ ! -r "$SSH_KEY" ]; then
	ssh-keygen -q -t ed25519 -N '' -C 'uamask-luci-e2e' -f "$SSH_KEY"
fi

if [ -n "${OPENWRT_IMAGE_GZ:-}" ]; then
	ARCHIVE=$OPENWRT_IMAGE_GZ
elif [ -r "$REPO_ROOT/dev/qemu/images/$IMAGE_NAME" ]; then
	ARCHIVE=$REPO_ROOT/dev/qemu/images/$IMAGE_NAME
else
	ARCHIVE=$DOWNLOAD_DIR/$IMAGE_NAME
	if [ ! -r "$ARCHIVE" ]; then
		log "downloading verified OpenWrt image"
		curl --fail --location --retry 3 --output "$ARCHIVE.part" "$IMAGE_URL"
		mv "$ARCHIVE.part" "$ARCHIVE"
	fi
fi

[ "$(sha256sum "$ARCHIVE" | awk '{print $1}')" = "$IMAGE_SHA256" ] || die "OpenWrt image SHA256 mismatch"

if [ ! -r "$RAW_IMAGE" ] || [ "$(sha256sum "$RAW_IMAGE" | awk '{print $1}')" != "$RAW_SHA256" ]; then
	set +e
	gzip -dc "$ARCHIVE" > "$RAW_IMAGE.part"
	gzip_rc=$?
	set -e
	case "$gzip_rc" in 0|2) ;; *) die "OpenWrt image decompression failed" ;; esac
	[ "$(sha256sum "$RAW_IMAGE.part" | awk '{print $1}')" = "$RAW_SHA256" ] || die "decompressed OpenWrt image SHA256 mismatch"
	mv "$RAW_IMAGE.part" "$RAW_IMAGE"
fi

qemu-img create -q -f qcow2 -F raw -b "$RAW_IMAGE" "$WORK_DIR/router.qcow2"

if ss -H -ltn "sport = :$SSH_PORT" | grep -q . || ss -H -ltn "sport = :$HTTP_PORT" | grep -q .; then
	die "provisioning port is already in use"
fi

QEMU_ACCEL=${QEMU_ACCEL:-tcg}
qemu-system-x86_64 -m 512 -smp 2 -accel "$QEMU_ACCEL" \
	-drive "file=$WORK_DIR/router.qcow2,format=qcow2,if=virtio" \
	-netdev "user,id=lan,restrict=on,hostfwd=tcp:127.0.0.1:$SSH_PORT-:22,hostfwd=tcp:127.0.0.1:$HTTP_PORT-:80" \
	-device virtio-net-pci,netdev=lan -display none -monitor none -no-reboot \
	-chardev "socket,id=serial0,path=$WORK_DIR/serial.sock,server=on,wait=off,logfile=$WORK_DIR/serial.log" \
	-serial chardev:serial0 -pidfile "$WORK_DIR/qemu.pid" -daemonize
QEMU_PID=$(cat "$WORK_DIR/qemu.pid")

console_ready=0
for attempt in $(seq 1 60); do
	if grep -q 'Please press Enter' "$WORK_DIR/serial.log" 2>/dev/null &&
		grep -q 'br-lan: port .* entered forwarding state' "$WORK_DIR/serial.log" 2>/dev/null; then
		console_ready=1
		break
	fi
	sleep 1
done
[ "$console_ready" -eq 1 ] || die "guest console did not become ready"

{
	printf '\n'
	while ! grep -Eq 'root@[^:]*:/#' "$WORK_DIR/serial.log" 2>/dev/null; do sleep 1; done
	printf "uci set network.lan.proto='dhcp'\n"
	printf "uci -q delete network.lan.ipaddr\n"
	printf "uci -q delete network.lan.netmask\n"
	printf "uci commit network\n"
	printf "/etc/init.d/network restart\n"
	printf "echo E2E_NETWORK_CONFIGURED\n"
	while ! grep -q 'E2E_NETWORK_CONFIGURED' "$WORK_DIR/serial.log" 2>/dev/null; do sleep 1; done
} | timeout 20 nc -U "$WORK_DIR/serial.sock" > "$WORK_DIR/console.out" 2>&1 || true

ssh_ready=0
for attempt in $(seq 1 60); do
	if ssh -p "$SSH_PORT" -o BatchMode=yes -o PreferredAuthentications=none \
		-o StrictHostKeyChecking=no -o UserKnownHostsFile="$WORK_DIR/known_hosts-bootstrap" \
		-o ConnectTimeout=2 root@127.0.0.1 true >/dev/null 2>&1; then ssh_ready=1; break; fi
	sleep 1
done
[ "$ssh_ready" -eq 1 ] || die "guest SSH did not become ready after DHCP configuration"

cat "$SSH_KEY.pub" | ssh -p "$SSH_PORT" -o BatchMode=yes -o PreferredAuthentications=none \
	-o StrictHostKeyChecking=no -o UserKnownHostsFile="$WORK_DIR/known_hosts-bootstrap" \
	root@127.0.0.1 'umask 077; mkdir -p /etc/dropbear; cat > /etc/dropbear/authorized_keys'

ssh_base 'touch /etc/config/wireless; /etc/init.d/rpcd enable; /etc/init.d/rpcd restart; /etc/init.d/uhttpd enable; /etc/init.d/uhttpd restart'
ssh_base 'opkg list-installed' > "$WORK_DIR/packages.txt"
printf '%s\n' "$PACKAGE_MANIFEST" | while IFS='=' read -r package version; do
	grep -Fx "$package - $version" "$WORK_DIR/packages.txt" >/dev/null || die "pinned base package missing: $package=$version"
done

http_ready=0
for attempt in $(seq 1 30); do
	if curl --silent --show-error --max-time 2 "http://127.0.0.1:$HTTP_PORT/cgi-bin/luci/" 2>/dev/null | grep -q 'luci_username'; then http_ready=1; break; fi
	sleep 1
done
[ "$http_ready" -eq 1 ] || die "LuCI readiness probe failed"

ssh_base 'sync; poweroff' >/dev/null 2>&1 || true
for attempt in $(seq 1 30); do
	if ! kill -0 "$QEMU_PID" 2>/dev/null; then QEMU_PID=; break; fi
	sleep 1
done
[ -z "$QEMU_PID" ] || die "provisioning guest did not power off"

mv "$WORK_DIR/router.qcow2" "$BASE_IMAGE"
printf '%s\n' "$PACKAGE_MANIFEST" > "$BASE_DIR/packages.txt"
printf '%s\n' "$FINGERPRINT" > "$READY_FILE"
log "prepared fingerprinted base $FINGERPRINT"
if [ "${1:-}" = "--print-path" ]; then printf '%s\n' "$BASE_IMAGE"; fi

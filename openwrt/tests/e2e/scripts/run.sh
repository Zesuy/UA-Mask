#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
E2E_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
REPO_ROOT=$(CDPATH= cd -- "$E2E_DIR/../../.." && pwd)
MODE=${1:-fast}
SUITE=${2:-full}
IPK=${IPK:-}
RUN_ID=${E2E_RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)-$$-$MODE-$SUITE}
RUN_DIR=$REPO_ROOT/dev/luci-e2e/$RUN_ID
CACHE_ROOT=${E2E_CACHE_ROOT:-$REPO_ROOT/.cache/luci-e2e}
SSH_KEY=$CACHE_ROOT/ssh/id_ed25519
PASSWORD_FILE=$RUN_DIR/root-password
SSH_PORT=${E2E_SSH_PORT:-22222}
HTTP_PORT=${E2E_HTTP_PORT:-28080}
QEMU_PID=
OVERLAY=$RUN_DIR/router.qcow2
PLAYWRIGHT_RC=0
CLEANUP_OK=0

log() { printf 'luci-e2e: %s\n' "$*"; }
die() { log "$*" >&2; exit 1; }
require() { command -v "$1" >/dev/null 2>&1 || die "missing host prerequisite: $1"; }

ssh_guest() {
	ssh -i "$SSH_KEY" -p "$SSH_PORT" -o BatchMode=yes -o StrictHostKeyChecking=no \
		-o UserKnownHostsFile="$RUN_DIR/known_hosts" -o ConnectTimeout=3 root@127.0.0.1 "$@"
}

owned_qemu_running() {
	[ -n "$QEMU_PID" ] && kill -0 "$QEMU_PID" 2>/dev/null
}

stop_owned_qemu() {
	owned_qemu_running || return 0
	case "$QEMU_PID" in ''|*[!0-9]*) return 1 ;; esac
	cmdline=$(tr '\0' ' ' < "/proc/$QEMU_PID/cmdline" 2>/dev/null || true)
	case "$cmdline" in *"$OVERLAY"*) ;; *) log "refusing to stop non-owned PID $QEMU_PID"; return 1 ;; esac
	kill -TERM "$QEMU_PID"
	for _attempt in $(seq 1 20); do
		kill -0 "$QEMU_PID" 2>/dev/null || { QEMU_PID=; return 0; }
		sleep 1
	done
	return 1
}

stop_owned_browsers() {
	owned=""
	for proc_dir in /proc/[0-9]*; do
		[ -r "$proc_dir/cmdline" ] || continue
		cmdline=$(tr '\0' ' ' < "$proc_dir/cmdline" 2>/dev/null || true)
		case "$cmdline" in *"$RUN_DIR/tmp"*) owned="$owned ${proc_dir##*/}" ;; esac
	done
	for pid in $owned; do
		case "$pid" in ''|*[!0-9]*) return 1 ;; esac
		cmdline=$(tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)
		case "$cmdline" in *"$RUN_DIR/tmp"*chrom*) kill -TERM "$pid" ;; *) return 1 ;; esac
	done
	for _attempt in $(seq 1 10); do
		remaining=0
		for pid in $owned; do kill -0 "$pid" 2>/dev/null && remaining=1 || true; done
		[ "$remaining" -eq 0 ] && return 0
		sleep 1
	done
	return 1
}

cleanup() {
	cleanup_rc=0
	stop_owned_browsers || cleanup_rc=1
	stop_owned_qemu || cleanup_rc=1
	if [ "$cleanup_rc" -eq 0 ]; then
		CLEANUP_OK=1
		printf 'owned_qemu=none\nowned_chromium=none\nresult=pass\n' > "$RUN_DIR/cleanup.txt"
	else
		printf 'result=failed\n' > "$RUN_DIR/cleanup.txt"
	fi
	[ -e "$PASSWORD_FILE" ] && rm -f "$PASSWORD_FILE"
	[ -e "$OVERLAY" ] && rm -f "$OVERLAY"
}
trap cleanup EXIT HUP INT TERM

case "$MODE" in fast|package) ;; *) die "mode must be fast or package" ;; esac
case "$SUITE" in smoke|full|failure-probe) ;; *) die "suite must be smoke, full, or failure-probe" ;; esac
[ -x "$E2E_DIR/node_modules/.bin/playwright" ] || die "run 'make luci-e2e-install' first"

for tool in qemu-img qemu-system-x86_64 ssh scp curl sha256sum go node npm ss; do require "$tool"; done
mkdir -p "$RUN_DIR/tmp" "$RUN_DIR/telemetry"
chmod 700 "$RUN_DIR" "$RUN_DIR/tmp"

BASE_IMAGE=$($SCRIPT_DIR/prepare-base.sh --print-path)
[ -r "$BASE_IMAGE" ] || die "prepared base is unavailable"
[ -r "$SSH_KEY" ] || die "prepared SSH key is unavailable"

if ss -H -ltn "sport = :$SSH_PORT" | grep -q . || ss -H -ltn "sport = :$HTTP_PORT" | grep -q .; then
	die "requested SSH or HTTP port is already in use"
fi

qemu-img create -q -f qcow2 -F qcow2 -b "$BASE_IMAGE" "$OVERLAY"
QEMU_ACCEL=${QEMU_ACCEL:-$(if [ -w /dev/kvm ]; then printf kvm; else printf tcg; fi)}
qemu-system-x86_64 -m 512 -smp 2 -accel "$QEMU_ACCEL" \
	-drive "file=$OVERLAY,format=qcow2,if=virtio" \
	-netdev "user,id=lan,restrict=on,hostfwd=tcp:127.0.0.1:$SSH_PORT-:22,hostfwd=tcp:127.0.0.1:$HTTP_PORT-:80" \
	-device virtio-net-pci,netdev=lan -display none -monitor none -no-reboot \
	-serial "file:$RUN_DIR/qemu-serial.log" -pidfile "$RUN_DIR/qemu.pid" -daemonize
QEMU_PID=$(cat "$RUN_DIR/qemu.pid")

ssh_ready=0
for attempt in $(seq 1 60); do
	if ssh_guest true >/dev/null 2>&1; then ssh_ready=1; break; fi
	sleep 1
done
[ "$ssh_ready" -eq 1 ] || die "guest SSH readiness probe failed"

od -An -N16 -tx1 /dev/urandom | tr -d ' \n' > "$PASSWORD_FILE"
chmod 600 "$PASSWORD_FILE"
{ cat "$PASSWORD_FILE"; printf '\n'; cat "$PASSWORD_FILE"; printf '\n'; } | ssh_guest 'passwd root' >/dev/null

printf '{"mode":"%s","suite":"%s","git_head":"%s","qemu_accel":"%s"}\n' \
	"$MODE" "$SUITE" "$(git -C "$REPO_ROOT" rev-parse HEAD)" "$QEMU_ACCEL" > "$RUN_DIR/source-identity.json"
git -C "$REPO_ROOT" status --porcelain | awk '{print $2}' > "$RUN_DIR/worktree-paths.txt"

if [ "$MODE" = fast ]; then
	ssh_guest "if opkg status UAmask 2>/dev/null | grep -q '^Status: .* installed'; then exit 42; fi"
	(cd "$REPO_ROOT/core" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
		-ldflags "-s -w -X main.version=$(cat "$REPO_ROOT/VERSION")-e2e" \
		-o "$RUN_DIR/UAmask" ./cmd/UAmask)
	scp -O -q -i "$SSH_KEY" -P "$SSH_PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile="$RUN_DIR/known_hosts" \
		"$RUN_DIR/UAmask" root@127.0.0.1:/usr/bin/UAmask
	ssh_guest 'mkdir -p /usr/libexec /usr/share/luci/menu.d /usr/share/rpcd/acl.d /www/luci-static/resources/view/uamask'
	copy_to_guest() {
		scp -O -q -i "$SSH_KEY" -P "$SSH_PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile="$RUN_DIR/known_hosts" "$1" "root@127.0.0.1:$2"
	}
	copy_to_guest "$REPO_ROOT/openwrt/root/etc/config/UAmask" /etc/config/UAmask
	copy_to_guest "$REPO_ROOT/openwrt/root/etc/init.d/UAmask" /etc/init.d/UAmask
	copy_to_guest "$REPO_ROOT/openwrt/root/usr/libexec/uamask-luci" /usr/libexec/uamask-luci
	copy_to_guest "$REPO_ROOT/openwrt/luci/root/usr/share/luci/menu.d/luci-app-uamask.json" /usr/share/luci/menu.d/luci-app-uamask.json
	copy_to_guest "$REPO_ROOT/openwrt/luci/root/usr/share/rpcd/acl.d/luci-app-uamask.json" /usr/share/rpcd/acl.d/luci-app-uamask.json
	for view in overview settings diagnostics; do
		copy_to_guest "$REPO_ROOT/openwrt/luci/htdocs/luci-static/resources/view/uamask/$view.js" "/www/luci-static/resources/view/uamask/$view.js"
	done
	printf '%s\n' "$(cat "$REPO_ROOT/VERSION")-e2e" | ssh_guest 'mkdir -p /usr/share/uamask; cat > /usr/share/uamask/VERSION'
	ssh_guest 'chmod 755 /usr/bin/UAmask /etc/init.d/UAmask /usr/libexec/uamask-luci; chmod 644 /etc/config/UAmask /usr/share/luci/menu.d/luci-app-uamask.json /usr/share/rpcd/acl.d/luci-app-uamask.json /www/luci-static/resources/view/uamask/*.js /usr/share/uamask/VERSION; rm -f /tmp/luci-indexcache /tmp/luci-modulecache/*; /etc/init.d/rpcd restart; /etc/init.d/uhttpd restart'
else
	[ -n "$IPK" ] || die "package mode requires IPK=/absolute/path/to/UAmask.ipk"
	case "$IPK" in /*) ;; *) IPK=$REPO_ROOT/$IPK ;; esac
	$SCRIPT_DIR/verify-ipk.sh "$IPK" "$RUN_DIR/package-identity.json" > "$RUN_DIR/package-verification.txt"
	scp -O -q -i "$SSH_KEY" -P "$SSH_PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile="$RUN_DIR/known_hosts" \
		"$IPK" root@127.0.0.1:/tmp/UAmask-e2e.ipk
	ssh_guest 'opkg install --force-reinstall /tmp/UAmask-e2e.ipk; opkg status UAmask' > "$RUN_DIR/installed-package.txt"
	{
		for view in overview settings diagnostics; do
			hash=$(sha256sum "$REPO_ROOT/openwrt/luci/htdocs/luci-static/resources/view/uamask/$view.js" | awk '{print $1}')
			printf '%s  /www/luci-static/resources/view/uamask/%s.js\n' "$hash" "$view"
		done
	} > "$RUN_DIR/expected-views.sha256"
	cat "$RUN_DIR/expected-views.sha256" | ssh_guest 'sha256sum -c -' > "$RUN_DIR/installed-view-verification.txt"
	ssh_guest 'rm -f /tmp/luci-indexcache /tmp/luci-modulecache/*; /etc/init.d/rpcd restart; /etc/init.d/uhttpd restart'
fi

ssh_guest 'cat /etc/openwrt_release; /usr/bin/UAmask -v 2>&1 || true; /usr/libexec/uamask-luci version' > "$RUN_DIR/guest-identity.txt"

http_ready=0
for attempt in $(seq 1 30); do
	if curl --silent --show-error --max-time 2 "http://127.0.0.1:$HTTP_PORT/cgi-bin/luci/" 2>/dev/null | grep -q 'luci_username'; then http_ready=1; break; fi
	sleep 1
done
if [ "$http_ready" -ne 1 ]; then
	ssh_guest 'printf "rpcd="; /etc/init.d/rpcd status || true; printf "uhttpd="; /etc/init.d/uhttpd status || true; ubus list 2>&1 || true; netstat -lntp 2>&1 || true; logread | tail -n 80' > "$RUN_DIR/readiness-debug.txt" 2>&1 || true
	curl --silent --show-error --max-time 3 "http://127.0.0.1:$HTTP_PORT/cgi-bin/luci/" > "$RUN_DIR/luci-readiness.html" 2> "$RUN_DIR/luci-readiness-curl.txt" || true
	die "LuCI readiness probe failed"
fi

export E2E_RUN_DIR="$RUN_DIR"
export E2E_SSH_PORT="$SSH_PORT"
export E2E_SSH_KEY="$SSH_KEY"
export LUCI_BASE_URL="http://127.0.0.1:$HTTP_PORT"
export TMPDIR="$RUN_DIR/tmp"
export E2E_MODE="$MODE"
export LUCI_PASSWORD="$(cat "$PASSWORD_FILE")"

set +e
case "$SUITE" in
	smoke) (cd "$E2E_DIR" && npm run test:smoke) ; PLAYWRIGHT_RC=$? ;;
	full) (cd "$E2E_DIR" && npm run test) ; PLAYWRIGHT_RC=$? ;;
	failure-probe) (cd "$E2E_DIR" && npm run test:failure-probe) ; PLAYWRIGHT_RC=$? ;;
esac
set -e

printf '{"playwright_exit":%s,"expected_failure":%s}\n' "$PLAYWRIGHT_RC" "$([ "$SUITE" = failure-probe ] && printf true || printf false)" > "$RUN_DIR/result.json"

if [ "$SUITE" = failure-probe ]; then
	[ "$PLAYWRIGHT_RC" -ne 0 ] || die "intentional failure probe unexpectedly passed"
	find "$RUN_DIR/test-results" -type f -name 'trace.zip' -print | grep -q . || die "failure probe did not retain a trace"
	find "$RUN_DIR/test-results" -type f -name 'test-failed-*.png' -print | grep -q . || die "failure probe did not retain a screenshot"
	PLAYWRIGHT_RC=0
fi

[ "$PLAYWRIGHT_RC" -eq 0 ] || exit "$PLAYWRIGHT_RC"
log "PASS mode=$MODE suite=$SUITE artifacts=$RUN_DIR"

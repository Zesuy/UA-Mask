#!/bin/sh

set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

export JSON_OPS="$TMP_DIR/json.ops"
export JSON_RENDERER="$ROOT/openwrt/tests/render-json-ops.py"
export JSHN_LIB="$ROOT/openwrt/tests/fake-jshn.sh"
export CORE_CONFIG_DIR="$TMP_DIR/run"
export PROG="$TMP_DIR/UAmask"
export TEST_LOG_FILE="$TMP_DIR/log/UAmask.log"
export NAME="UAmask"

TEST_LOG_OUTPUT="$TMP_DIR/init.log"
TEST_SCENARIO="valid"
TEST_ENABLED=1

(cd "$ROOT/core" && go build -o "$PROG" ./cmd/UAmask)

logger() {
    printf '%s\n' "$*" >> "$TEST_LOG_OUTPUT"
}

preserve_failure_artifacts() {
    [ -n "${TEST_ARTIFACT_DIR:-}" ] || return 0
    mkdir -p "$TEST_ARTIFACT_DIR"
    [ -f "$TEST_LOG_OUTPUT" ] && cp "$TEST_LOG_OUTPUT" "$TEST_ARTIFACT_DIR/procd-contract.log"
    [ -f "$CORE_CONFIG_PATH" ] && cp "$CORE_CONFIG_PATH" "$TEST_ARTIFACT_DIR/procd-config.json"
}

fail() {
    echo "FAIL $1" >&2
    preserve_failure_artifacts
    exit 1
}

config_load() {
    [ "$1" = "UAmask" ]
}

config_get() {
    local variable="$1"
    local option="$3"
    local default_value="$4"
    local value="$default_value"
    [ "$option" = "log_file" ] && value="$TEST_LOG_FILE"
    [ "$TEST_SCENARIO:$option" = "invalid:match_mode" ] && value="invalid-mode"
    eval "$variable=\$value"
}

config_get_bool() {
    local variable="$1"
    local section="$2"
    local option="$3"
    local default_value="$4"
    local value="$default_value"
    if [ "$section:$option" = "enabled:enabled" ]; then
        value="$TEST_ENABLED"
    fi
    eval "$variable=\$value"
}

. "$ROOT/openwrt/root/etc/init.d/UAmask"

OPEN_INSTANCE=""
CLOSED_INSTANCE=0
COMMAND_PROGRAM=""
COMMAND_FLAG=""
COMMAND_CONFIG=""
FILE_PARAM=""
LIMITS_PARAM=""
RESPAWN_PARAM=0
STDOUT_PARAM=""
STDERR_PARAM=""
FIREWALL_SET=0
EVENTS=""

procd_open_instance() {
    OPEN_INSTANCE="$1"
    EVENTS="$EVENTS open"
}

procd_set_param() {
    case "$1" in
        command)
            [ "$#" -eq 4 ] || return 1
            COMMAND_PROGRAM="$2"
            COMMAND_FLAG="$3"
            COMMAND_CONFIG="$4"
            ;;
        file)
            [ "$#" -eq 2 ] || return 1
            FILE_PARAM="$2"
            ;;
        limits)
            [ "$#" -eq 2 ] || return 1
            LIMITS_PARAM="$2"
            ;;
        respawn)
            [ "$#" -eq 1 ] || return 1
            RESPAWN_PARAM=1
            ;;
        stdout)
            [ "$#" -eq 2 ] || return 1
            STDOUT_PARAM="$2"
            ;;
        stderr)
            [ "$#" -eq 2 ] || return 1
            STDERR_PARAM="$2"
            ;;
        *)
            return 1
            ;;
    esac
}

procd_close_instance() {
    CLOSED_INSTANCE=1
    EVENTS="$EVENTS close"
}

set_firewall() {
    FIREWALL_SET=1
    EVENTS="$EVENTS firewall"
}

unset_firewall() {
    EVENTS="$EVENTS unset-firewall"
}

FW_TYPE="nft"

start_service || fail "OWRT-PROCD-001 enabled start unexpectedly failed"

[ "$OPEN_INSTANCE" = "UAmask" ] || fail "OWRT-PROCD-001 instance name"
[ "$COMMAND_PROGRAM" = "$PROG" ] || fail "OWRT-PROCD-001 program"
[ "$COMMAND_FLAG" = "-config" ] || fail "OWRT-PROCD-001 config flag"
[ "$COMMAND_CONFIG" = "$CORE_CONFIG_PATH" ] || fail "OWRT-PROCD-001 config path"
[ "$FILE_PARAM" = "$CORE_CONFIG_PATH" ] || fail "OWRT-PROCD-001 file param"
[ "$LIMITS_PARAM" = "nofile=65536 65536" ] || fail "OWRT-PROCD-001 limits"
[ "$RESPAWN_PARAM" -eq 1 ] || fail "OWRT-PROCD-001 respawn"
[ "$STDOUT_PARAM" = "1" ] || fail "OWRT-PROCD-001 stdout"
[ "$STDERR_PARAM" = "1" ] || fail "OWRT-PROCD-001 stderr"
[ "$CLOSED_INSTANCE" -eq 1 ] || fail "OWRT-PROCD-001 close"
[ "$FIREWALL_SET" -eq 1 ] || fail "OWRT-PROCD-001 firewall"
[ -f "$CORE_CONFIG_PATH" ] || fail "OWRT-PROCD-001 generated config"
[ "$EVENTS" = " open close firewall" ] || fail "OWRT-PROCD-001 procd/firewall ordering"

"$PROG" -check-config -config "$CORE_CONFIG_PATH" >/dev/null || fail "OWRT-PROCD-001 generated config validation"
echo "PASS OWRT-PROCD-001 start_service contract"

# A disabled service must stop before generating JSON or opening a procd
# instance. This preserves the current enable flag gate and avoids stale
# firewall setup on a disabled install.
rm -f "$CORE_CONFIG_PATH"
OPEN_INSTANCE=""
CLOSED_INSTANCE=0
COMMAND_PROGRAM=""
COMMAND_FLAG=""
COMMAND_CONFIG=""
FILE_PARAM=""
LIMITS_PARAM=""
RESPAWN_PARAM=0
STDOUT_PARAM=""
STDERR_PARAM=""
FIREWALL_SET=0
EVENTS=""
TEST_ENABLED=0
TEST_SCENARIO="valid"
if start_service; then
    fail "OWRT-PROCD-002 disabled service unexpectedly started"
fi
[ -z "$OPEN_INSTANCE" ] || fail "OWRT-PROCD-002 disabled service opened procd instance"
[ "$FIREWALL_SET" -eq 0 ] || fail "OWRT-PROCD-002 disabled service configured firewall"
[ ! -e "$CORE_CONFIG_PATH" ] || fail "OWRT-PROCD-002 disabled service generated config"
[ -z "$EVENTS" ] || fail "OWRT-PROCD-002 disabled service emitted lifecycle events"
echo "PASS OWRT-PROCD-002 disabled start gate"

# A validation failure must preserve the previous generated JSON and must not
# open procd or install firewall rules.
mkdir -p "$CORE_CONFIG_DIR"
cp "$ROOT/core/internal/config/testdata/valid/schema-v1-minimal.json" "$CORE_CONFIG_PATH"
cp "$CORE_CONFIG_PATH" "$TMP_DIR/previous-config.json"
OPEN_INSTANCE=""
CLOSED_INSTANCE=0
COMMAND_PROGRAM=""
COMMAND_FLAG=""
COMMAND_CONFIG=""
FILE_PARAM=""
LIMITS_PARAM=""
RESPAWN_PARAM=0
STDOUT_PARAM=""
STDERR_PARAM=""
FIREWALL_SET=0
EVENTS=""
TEST_ENABLED=1
TEST_SCENARIO="invalid"
if start_service; then
    fail "OWRT-PROCD-003 invalid config unexpectedly started"
fi
cmp "$TMP_DIR/previous-config.json" "$CORE_CONFIG_PATH" >/dev/null || fail "OWRT-PROCD-003 invalid config replaced previous JSON"
[ -z "$OPEN_INSTANCE" ] || fail "OWRT-PROCD-003 invalid config opened procd instance"
[ "$FIREWALL_SET" -eq 0 ] || fail "OWRT-PROCD-003 invalid config configured firewall"
[ -z "$EVENTS" ] || fail "OWRT-PROCD-003 invalid config emitted lifecycle events"
if find "$CORE_CONFIG_DIR" -name 'config.json.tmp.*' -type f | grep -q .; then
    fail "OWRT-PROCD-003 invalid config left temporary JSON"
fi
[ -f "$LAST_ERROR_PATH" ] || fail "OWRT-PROCD-003 validation error was not persisted"
grep -q '^stage=validation$' "$LAST_ERROR_PATH" || fail "OWRT-PROCD-003 validation error stage"
grep -q '^message=Core configuration validation failed:' "$LAST_ERROR_PATH" || fail "OWRT-PROCD-003 validation error message"
[ "$(stat -c %a "$LAST_ERROR_PATH")" = "600" ] || fail "OWRT-PROCD-003 validation error permissions"
echo "PASS OWRT-PROCD-003 invalid start preserves config and exposes validation error"

TEST_ENABLED=0
stop_service
[ "$EVENTS" = " unset-firewall" ] || fail "OWRT-PROCD-004 stop firewall cleanup"
[ ! -e "$LAST_ERROR_PATH" ] || fail "OWRT-PROCD-004 successful stop did not clear error"
echo "PASS OWRT-PROCD-004 explicit stop cleanup and recovery"

# Lock the package/UCI assumptions that remain after the reviewed native LuCI
# migration switch.
python3 - "$ROOT/openwrt/package/Makefile" "$ROOT/openwrt/root/etc/config/UAmask" <<'PY' || fail "OWRT-PKG-001 package and UCI asset assumptions"
from pathlib import Path
import sys

package = Path(sys.argv[1]).read_text(encoding="utf-8")
uci = Path(sys.argv[2]).read_text(encoding="utf-8")

assert "DEPENDS:=$(UAMASK_ARCH_DEPENDS) +libubox +luci-base" in package
for source, destination in (
    ("openwrt/root/etc/init.d/UAmask", "/etc/init.d/UAmask"),
    ("openwrt/root/etc/config/UAmask", "/etc/config/UAmask"),
):
    assert source in package and destination in package
assert "openwrt/luci/model/cbi/UAmask.lua" not in package
assert "openwrt/luci/controller/UAmask.lua" not in package
assert "config 'UAmask' 'enabled'" in uci
assert "option bypass_ports '22 443'" in uci
assert "option bypass_ips '172.16.0.0/12 192.168.0.0/16 127.0.0.0/8 169.254.0.0/16'" in uci
PY
echo "PASS OWRT-PKG-001 native package dependency and shipped UCI assumptions"

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

(cd "$ROOT/core" && go build -o "$PROG" ./cmd/UAmask)

logger() {
    :
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
    eval "$variable=\$value"
}

config_get_bool() {
    local variable="$1"
    local section="$2"
    local option="$3"
    local default_value="$4"
    local value="$default_value"
    if [ "$section:$option" = "enabled:enabled" ]; then
        value="1"
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

procd_open_instance() {
    OPEN_INSTANCE="$1"
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
}

set_firewall() {
    FIREWALL_SET=1
}

FW_TYPE="nft"
start_service

[ "$OPEN_INSTANCE" = "UAmask" ] || { echo "FAIL OWRT-PROCD-001 instance name" >&2; exit 1; }
[ "$COMMAND_PROGRAM" = "$PROG" ] || { echo "FAIL OWRT-PROCD-001 program" >&2; exit 1; }
[ "$COMMAND_FLAG" = "-config" ] || { echo "FAIL OWRT-PROCD-001 config flag" >&2; exit 1; }
[ "$COMMAND_CONFIG" = "$CORE_CONFIG_PATH" ] || { echo "FAIL OWRT-PROCD-001 config path" >&2; exit 1; }
[ "$FILE_PARAM" = "$CORE_CONFIG_PATH" ] || { echo "FAIL OWRT-PROCD-001 file param" >&2; exit 1; }
[ "$LIMITS_PARAM" = "nofile=65536 65536" ] || { echo "FAIL OWRT-PROCD-001 limits" >&2; exit 1; }
[ "$RESPAWN_PARAM" -eq 1 ] || { echo "FAIL OWRT-PROCD-001 respawn" >&2; exit 1; }
[ "$STDOUT_PARAM" = "1" ] || { echo "FAIL OWRT-PROCD-001 stdout" >&2; exit 1; }
[ "$STDERR_PARAM" = "1" ] || { echo "FAIL OWRT-PROCD-001 stderr" >&2; exit 1; }
[ "$CLOSED_INSTANCE" -eq 1 ] || { echo "FAIL OWRT-PROCD-001 close" >&2; exit 1; }
[ "$FIREWALL_SET" -eq 1 ] || { echo "FAIL OWRT-PROCD-001 firewall" >&2; exit 1; }
[ -f "$CORE_CONFIG_PATH" ] || { echo "FAIL OWRT-PROCD-001 generated config" >&2; exit 1; }

"$PROG" -check-config -config "$CORE_CONFIG_PATH" >/dev/null
echo "PASS OWRT-PROCD-001 start_service contract"

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

"$PROG" -check-config -config "$CORE_CONFIG_PATH" >/dev/null || fail "OWRT-PROCD-001 generated config validation"
echo "PASS OWRT-PROCD-001 start_service contract"

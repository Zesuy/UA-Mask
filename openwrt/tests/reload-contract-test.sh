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

fail() {
    echo "FAIL $1" >&2
    exit 1
}

pass() {
    echo "PASS $1"
}

logger() {
    printf '%s\n' "$*" >> "$TEST_LOG_OUTPUT"
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

    [ "$section:$option" = "enabled:enabled" ] && value="$TEST_ENABLED"
    eval "$variable=\$value"
}

config_list_foreach() {
    return 0
}

. "$ROOT/openwrt/root/etc/init.d/UAmask"

FW_TYPE="nft"
RC_PROCD_CALLS=0
REGISTER_CALLS=0
FIREWALL_SET_CALLS=0
FIREWALL_UNSET_CALLS=0
KILL_CALLS=0
FAIL_REGISTER=0

rc_procd() {
    local callback="$1"

    shift
    RC_PROCD_CALLS=$((RC_PROCD_CALLS + 1))
    "$callback" "$@"
}

procd_open_instance() {
    REGISTER_CALLS=$((REGISTER_CALLS + 1))
    [ "$FAIL_REGISTER" -eq 0 ]
}

procd_set_param() {
    return 0
}

procd_close_instance() {
    return 0
}

set_firewall() {
    FIREWALL_SET_CALLS=$((FIREWALL_SET_CALLS + 1))
}

unset_firewall() {
    FIREWALL_UNSET_CALLS=$((FIREWALL_UNSET_CALLS + 1))
}

procd_kill() {
    [ "$1" = "UAmask" ] || return 1
    KILL_CALLS=$((KILL_CALLS + 1))
}

reset_actions() {
    RC_PROCD_CALLS=0
    REGISTER_CALLS=0
    FIREWALL_SET_CALLS=0
    FIREWALL_UNSET_CALLS=0
    KILL_CALLS=0
}

if grep -q 'procd_add_reload_trigger' "$ROOT/openwrt/root/etc/init.d/UAmask"; then
    fail "OWRT-RELOAD-000 implicit UCI trigger would duplicate explicit Save & Apply"
fi
pass "OWRT-RELOAD-000 lifecycle uses explicit actions only"

# Establish a known healthy runtime artifact before exercising reload.
generate_core_config || fail "OWRT-RELOAD-SETUP generate initial config"
cp "$CORE_CONFIG_PATH" "$TMP_DIR/last-valid.json"

# An invalid candidate must not update procd, firewall, or the running config.
TEST_SCENARIO="invalid"
reset_actions
if reload_service; then
    fail "OWRT-RELOAD-001 invalid reload unexpectedly succeeded"
fi
cmp "$TMP_DIR/last-valid.json" "$CORE_CONFIG_PATH" >/dev/null || fail "OWRT-RELOAD-001 invalid reload replaced valid config"
[ "$RC_PROCD_CALLS" -eq 0 ] || fail "OWRT-RELOAD-001 invalid reload updated procd"
[ "$REGISTER_CALLS" -eq 0 ] || fail "OWRT-RELOAD-001 invalid reload registered instance"
[ "$FIREWALL_SET_CALLS" -eq 0 ] || fail "OWRT-RELOAD-001 invalid reload changed firewall"
[ "$FIREWALL_UNSET_CALLS" -eq 0 ] || fail "OWRT-RELOAD-001 invalid reload removed firewall"
[ "$KILL_CALLS" -eq 0 ] || fail "OWRT-RELOAD-001 invalid reload stopped healthy process"
[ -f "$LAST_ERROR_PATH" ] || fail "OWRT-RELOAD-001 validation error missing"
grep -q '^stage=validation$' "$LAST_ERROR_PATH" || fail "OWRT-RELOAD-001 validation stage"
grep -q 'invalid rewrite mode' "$LAST_ERROR_PATH" || fail "OWRT-RELOAD-001 authoritative validation message"
pass "OWRT-RELOAD-001 invalid candidate preserves healthy runtime"

# Recovery performs exactly one procd update and one firewall apply, then
# clears the persisted error.
TEST_SCENARIO="valid"
reset_actions
reload_service || fail "OWRT-RELOAD-002 valid recovery reload"
[ "$RC_PROCD_CALLS" -eq 1 ] || fail "OWRT-RELOAD-002 duplicate or missing procd update"
[ "$REGISTER_CALLS" -eq 1 ] || fail "OWRT-RELOAD-002 duplicate or missing instance registration"
[ "$FIREWALL_SET_CALLS" -eq 1 ] || fail "OWRT-RELOAD-002 duplicate or missing firewall apply"
[ "$FIREWALL_UNSET_CALLS" -eq 0 ] || fail "OWRT-RELOAD-002 healthy process stopped before update"
[ "$KILL_CALLS" -eq 0 ] || fail "OWRT-RELOAD-002 healthy process killed before update"
[ ! -e "$LAST_ERROR_PATH" ] || fail "OWRT-RELOAD-002 recovery did not clear error"
pass "OWRT-RELOAD-002 valid reload is one explicit recovery action"

# A synchronous procd registration failure is persisted as a start failure;
# a later successful start clears it.
FAIL_REGISTER=1
reset_actions
if start_service; then
    fail "OWRT-RELOAD-003 failed registration unexpectedly started"
fi
[ "$REGISTER_CALLS" -eq 1 ] || fail "OWRT-RELOAD-003 registration was not attempted once"
[ "$FIREWALL_SET_CALLS" -eq 0 ] || fail "OWRT-RELOAD-003 firewall applied after start failure"
grep -q '^stage=start$' "$LAST_ERROR_PATH" || fail "OWRT-RELOAD-003 start error stage"
FAIL_REGISTER=0
reset_actions
start_service || fail "OWRT-RELOAD-003 start recovery"
[ "$REGISTER_CALLS" -eq 1 ] || fail "OWRT-RELOAD-003 recovery registration count"
[ "$FIREWALL_SET_CALLS" -eq 1 ] || fail "OWRT-RELOAD-003 recovery firewall count"
[ ! -e "$LAST_ERROR_PATH" ] || fail "OWRT-RELOAD-003 start recovery did not clear error"
pass "OWRT-RELOAD-003 start failure is observable and recoverable"

# Disabling through the explicit reload action removes firewall state and
# asks procd to stop exactly once without generating or registering a service.
persist_last_error "validation" "stale error" || fail "OWRT-RELOAD-004 seed stale error"
TEST_ENABLED=0
reset_actions
reload_service || fail "OWRT-RELOAD-004 disable transition"
[ "$RC_PROCD_CALLS" -eq 0 ] || fail "OWRT-RELOAD-004 disabled service updated procd"
[ "$REGISTER_CALLS" -eq 0 ] || fail "OWRT-RELOAD-004 disabled service registered instance"
[ "$FIREWALL_UNSET_CALLS" -eq 1 ] || fail "OWRT-RELOAD-004 firewall cleanup count"
[ "$KILL_CALLS" -eq 1 ] || fail "OWRT-RELOAD-004 process stop count"
[ ! -e "$LAST_ERROR_PATH" ] || fail "OWRT-RELOAD-004 disable did not clear error"
pass "OWRT-RELOAD-004 disable is one explicit stop action"

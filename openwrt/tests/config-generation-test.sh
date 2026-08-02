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
TEST_SCENARIO="complete"
TEST_PROFILE=""

(cd "$ROOT/core" && go build -o "$PROG" ./cmd/UAmask)

preserve_failure_artifacts() {
    [ -n "${TEST_ARTIFACT_DIR:-}" ] || return 0
    mkdir -p "$TEST_ARTIFACT_DIR"
    [ -f "$TEST_LOG_OUTPUT" ] && cp "$TEST_LOG_OUTPUT" "$TEST_ARTIFACT_DIR/config-generation.log"
    [ -f "$CORE_CONFIG_PATH" ] && cp "$CORE_CONFIG_PATH" "$TEST_ARTIFACT_DIR/config.json"
    for file in "$CORE_CONFIG_PATH".tmp.*; do
        [ -f "$file" ] && cp "$file" "$TEST_ARTIFACT_DIR/$(basename "$file")"
    done
}

fail() {
    echo "FAIL $1" >&2
    preserve_failure_artifacts
    exit 1
}

pass() {
    echo "PASS $1"
}

logger() {
    printf '%s\n' "$*" >> "$TEST_LOG_OUTPUT"
}

config_get() {
    local variable="$1"
    local option="$3"
    local default_value="$4"
    local value="$default_value"

    case "$option" in
        log_file) value="$TEST_LOG_FILE" ;;
    esac

    case "$TEST_SCENARIO:$option" in
        complete:port) value="13000" ;;
        complete:ua) value="Mask UA" ;;
        complete:log_level) value="debug" ;;
        complete:whitelist) value="Client A, Client B" ;;
        complete:match_mode) value="regex" ;;
        complete:keywords) value="Android,Linux" ;;
        complete:ua_regex) value="Android|Linux" ;;
        complete:replace_method) value="partial" ;;
        complete:Firewall_ua_whitelist) value="Steam, Valve" ;;
        complete:firewall_nonhttp_threshold) value="7" ;;
        complete:firewall_timeout) value="3600" ;;
        complete:firewall_decision_delay) value="15" ;;
        complete:operating_profile) value="custom" ;;
        complete:cache_size) value="1234" ;;
        complete:buffer_size) value="4096" ;;
        complete:pool_size) value="9" ;;
        complete:gogc_value) value="80" ;;
        safety:firewall_nonhttp_threshold) value="0" ;;
        safety:firewall_timeout) value="1" ;;
        safety:firewall_decision_delay) value="0" ;;
        profile:operating_profile)
            [ "$TEST_PROFILE" = "__missing__" ] || value="$TEST_PROFILE"
            ;;
        profile:cache_size) value="1234" ;;
        profile:buffer_size) value="4096" ;;
        profile:pool_size) value="9" ;;
        profile:gogc_value) value="80" ;;
        escape:ua)
            value='Mask "UA" \ path
第二行'
            ;;
        escape:whitelist) value='Client "A", 路径\B' ;;
        escape:keywords) value='Android,OpenHarmony' ;;
        escape:Firewall_ua_whitelist) value='Steam "Deck", 游戏\客户端' ;;
        invalid:match_mode) value="invalid-mode" ;;
    esac

    eval "$variable=\$value"
}

config_get_bool() {
    local variable="$1"
    local option="$3"
    local default_value="$4"
    local value="$default_value"

    case "$TEST_SCENARIO:$option" in
        complete:enable_firewall_set|complete:Firewall_ua_bypass|complete:Firewall_drop_on_match|complete:firewall_advanced_settings)
            value="1"
            ;;
        safety:enable_firewall_set|safety:firewall_advanced_settings)
            value="1"
            ;;
        escape:enable_firewall_set)
            value="1"
            ;;
    esac
    eval "$variable=\$value"
}

. "$ROOT/openwrt/root/etc/init.d/UAmask"
FW_TYPE="nft"
CONFIG_PATH="$CORE_CONFIG_PATH"

TEST_SCENARIO="complete"
generate_core_config || fail "OWRT-JSON-001 generate complete config"
"$PROG" -check-config -config "$CONFIG_PATH" >/dev/null || fail "OWRT-JSON-001 validate complete config"
python3 - "$CONFIG_PATH" <<'PY' || fail "OWRT-JSON-001 verify complete mapping"
import json
import sys

with open(sys.argv[1], encoding="utf-8") as config_file:
    config = json.load(config_file)

assert config == {
    "schema_version": 1,
    "listen": {
        "port": 13000,
        "dial_timeout": "30s",
        "client_keep_alive": "3m",
        "upstream_keep_alive": "3m",
    },
    "rewrite": {
        "user_agent": "Mask UA",
        "mode": "regex",
        "keywords": ["Android", "Linux"],
        "whitelist": ["Client A", "Client B"],
        "pattern": "Android|Linux",
        "partial_replace": True,
    },
    "firewall": {
        "backend": "nft",
        "set_name": "UAmask_bypass_set",
        "ua_whitelist": ["Steam", "Valve"],
        "drop_on_match": True,
        "whitelist_timeout": "24h",
        "non_http": {
            "enabled": True,
            "threshold": 7,
            "decision_delay": "15s",
            "http_cooldown": "1h",
            "timeout": "3600s",
            "cleanup_interval": "10m",
        },
    },
    "observe": {
        "log_level": "debug",
        "log_file": config["observe"]["log_file"],
        "stats_file": "/tmp/UAmask.stats",
        "stats_interval": "5s",
    },
    "performance": {
        "profile": "custom",
        "cache_size": 1234,
        "buffer_size": 4096,
        "pool_size": 9,
        "gc_percent": 80,
    },
}
assert config["observe"]["log_file"].endswith("/log/UAmask.log")
PY
pass "OWRT-JSON-001 complete UCI mapping"

TEST_SCENARIO="safety"
generate_core_config || fail "OWRT-SAFE-001 generate safety config"
python3 - "$CONFIG_PATH" <<'PY' || fail "OWRT-SAFE-001 verify safety floors"
import json
import sys

with open(sys.argv[1], encoding="utf-8") as config_file:
    non_http = json.load(config_file)["firewall"]["non_http"]

assert non_http["threshold"] == 5
assert non_http["decision_delay"] == "60s"
assert non_http["timeout"] == "28800s"
PY
pass "OWRT-SAFE-001 legacy safety floors"

TEST_SCENARIO="profile"
for profile_case in \
    "Low low 2000 8192 200 100" \
    "Medium medium 3000 8192 500 100" \
    "High high 5000 8192 1000 100" \
    "custom custom 1234 4096 9 80" \
    "__missing__ medium 3000 8192 500 100"
do
    set -- $profile_case
    TEST_PROFILE="$1"
    expected_profile="$2"
    expected_cache="$3"
    expected_buffer="$4"
    expected_pool="$5"
    expected_gc="$6"
    generate_core_config || fail "OWRT-PROFILE generate $TEST_PROFILE"
    "$PROG" -dump-effective-config -config "$CONFIG_PATH" > "$TMP_DIR/effective.json" || fail "OWRT-PROFILE dump $TEST_PROFILE"
    python3 - "$TMP_DIR/effective.json" "$expected_profile" "$expected_cache" "$expected_buffer" "$expected_pool" "$expected_gc" <<'PY' || fail "OWRT-PROFILE verify $TEST_PROFILE"
import json
import sys

with open(sys.argv[1], encoding="utf-8") as config_file:
    config = json.load(config_file)

assert config["performance"]["profile"] == sys.argv[2]
assert config["performance"]["cache_size"] == int(sys.argv[3])
assert config["performance"]["buffer_size"] == int(sys.argv[4])
assert config["performance"]["pool_size"] == int(sys.argv[5])
assert config["performance"]["gc_percent"] == int(sys.argv[6])
PY
done
pass "OWRT-PROFILE-001 preset and default profiles"

TEST_SCENARIO="escape"
generate_core_config || fail "OWRT-ESCAPE-001 generate escaped config"
python3 - "$CONFIG_PATH" <<'PY' || fail "OWRT-ESCAPE-001 verify escaped values"
import json
import sys

with open(sys.argv[1], encoding="utf-8") as config_file:
    config = json.load(config_file)

assert config["rewrite"]["user_agent"] == 'Mask "UA" \\ path\n第二行'
assert config["rewrite"]["whitelist"] == ['Client "A"', '路径\\B']
assert config["firewall"]["ua_whitelist"] == ['Steam "Deck"', '游戏\\客户端']
PY
pass "OWRT-ESCAPE-001 JSON escaping and Unicode"

mkdir -p "$CORE_CONFIG_DIR"
cp "$ROOT/core/internal/config/testdata/valid/schema-v1-minimal.json" "$CONFIG_PATH"
cp "$CONFIG_PATH" "$TMP_DIR/previous-config.json"
TEST_SCENARIO="invalid"
if generate_core_config; then
    fail "OWRT-ATOMIC-001 invalid config was accepted"
fi
cmp "$TMP_DIR/previous-config.json" "$CONFIG_PATH" >/dev/null || fail "OWRT-ATOMIC-001 previous config was replaced"
if find "$CORE_CONFIG_DIR" -name 'config.json.tmp.*' -type f | grep -q .; then
    fail "OWRT-ATOMIC-001 temporary config was not removed"
fi
pass "OWRT-ATOMIC-001 invalid generation preserves previous config"

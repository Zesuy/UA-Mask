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

(cd "$ROOT/core" && go build -o "$PROG" ./cmd/UAmask)

logger() {
    :
}

config_get() {
    local variable="$1"
    local option="$3"
    local default_value="$4"
    local value="$default_value"

    case "$option" in
        port) value="13000" ;;
        ua) value="Mask UA" ;;
        log_level) value="debug" ;;
        log_file) value="$TEST_LOG_FILE" ;;
        whitelist) value="Client A, Client B" ;;
        match_mode) value="regex" ;;
        keywords) value="Android,Linux" ;;
        ua_regex) value="Android|Linux" ;;
        replace_method) value="partial" ;;
        Firewall_ua_whitelist) value="Steam, Valve" ;;
        firewall_nonhttp_threshold) value="7" ;;
        firewall_timeout) value="3600" ;;
        firewall_decision_delay) value="15" ;;
        operating_profile) value="custom" ;;
        cache_size) value="1234" ;;
        buffer_size) value="4096" ;;
        pool_size) value="9" ;;
        gogc_value) value="80" ;;
    esac
    eval "$variable=\$value"
}

config_get_bool() {
    local variable="$1"
    local option="$3"
    local default_value="$4"
    local value="$default_value"

    case "$option" in
        enable_firewall_set|Firewall_ua_bypass|Firewall_drop_on_match|firewall_advanced_settings)
            value="1"
            ;;
    esac
    eval "$variable=\$value"
}

. "$ROOT/openwrt/root/etc/init.d/UAmask"
FW_TYPE="nft"
generate_core_config

CONFIG_PATH="$CORE_CONFIG_DIR/config.json"
"$PROG" -check-config -config "$CONFIG_PATH" >/dev/null

python3 - "$CONFIG_PATH" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as config_file:
    config = json.load(config_file)

assert config["schema_version"] == 1
assert config["listen"]["port"] == 13000
assert config["rewrite"]["user_agent"] == "Mask UA"
assert config["rewrite"]["mode"] == "regex"
assert config["rewrite"]["whitelist"] == ["Client A", "Client B"]
assert config["rewrite"]["partial_replace"] is True
assert config["firewall"]["backend"] == "nft"
assert config["firewall"]["ua_whitelist"] == ["Steam", "Valve"]
assert config["firewall"]["non_http"]["enabled"] is True
assert config["firewall"]["non_http"]["threshold"] == 7
assert config["firewall"]["non_http"]["decision_delay"] == "15s"
assert config["firewall"]["non_http"]["timeout"] == "3600s"
assert config["performance"] == {
    "profile": "custom",
    "cache_size": 1234,
    "buffer_size": 4096,
    "pool_size": 9,
    "gc_percent": 80,
}
PY

config_get() {
    local variable="$1"
    local option="$3"
    local default_value="$4"
    local value="$default_value"

    case "$option" in
        firewall_nonhttp_threshold) value="0" ;;
        firewall_timeout) value="1" ;;
        firewall_decision_delay) value="0" ;;
        log_file) value="$TEST_LOG_FILE" ;;
    esac
    eval "$variable=\$value"
}

generate_core_config

python3 - "$CONFIG_PATH" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as config_file:
    config = json.load(config_file)

non_http = config["firewall"]["non_http"]
assert non_http["threshold"] == 5
assert non_http["decision_delay"] == "60s"
assert non_http["timeout"] == "28800s"
PY

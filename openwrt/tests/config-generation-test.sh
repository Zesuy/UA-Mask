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

FIXTURE_DIR="$ROOT/openwrt/tests/fixtures"
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

config_load() {
    [ "$1" = "UAmask" ]
}

fixture_value() {
    local fixture_file="$1"
    local fixture_key="$2"
    awk -F= -v key="$fixture_key" '
        $1 == key {
            sub(/^[^=]*=/, "")
            print
            exit
        }
    ' "$FIXTURE_DIR/$fixture_file"
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
        canonical-csv:keywords_LENGTH) value="5" ;;
        canonical-csv:whitelist_LENGTH) value="3" ;;
        canonical-csv:Firewall_ua_whitelist_LENGTH) value="3" ;;
        canonical-space:iface_LENGTH) value="2" ;;
        canonical-space:bypass_ports_LENGTH) value="3" ;;
        canonical-space:bypass_ips_LENGTH) value="2" ;;
        canonical-space-empty:bypass_ports_LENGTH) value="1" ;;
        canonical-space-empty:bypass_ips_LENGTH) value="1" ;;
        legacy-csv:ua|legacy-csv:match_mode|legacy-csv:keywords|legacy-csv:whitelist|legacy-csv:Firewall_ua_whitelist)
            value="$(fixture_value legacy-csv-values.conf "$option")"
            ;;
        legacy-space:port|legacy-space:iface|legacy-space:bypass_gid|legacy-space:bypass_ports|legacy-space:bypass_ips|legacy-space:proxy_host)
            value="$(fixture_value legacy-space-values.conf "$option")"
            ;;
        canonical-csv:ua) value='Mask "UA" \\ path' ;;
        canonical-csv:match_mode) value="keywords" ;;
        canonical-space:port|canonical-space:bypass_gid)
            value="$(fixture_value legacy-space-values.conf "$option")"
            ;;
        legacy-space-empty:iface) value="br-lan" ;;
        legacy-space-empty:bypass_ports|legacy-space-empty:bypass_ips)
            value=""
            ;;
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
        invalid-regex:match_mode) value="regex" ;;
        invalid-regex:ua_regex) value="(" ;;
    esac

    eval "$variable=\$value"
}

config_list_foreach() {
    local section="$1"
    local option="$2"
    local callback="$3"
    local fixture_file=""

    case "$TEST_SCENARIO:$option" in
        canonical-csv:keywords|canonical-csv:whitelist|canonical-csv:Firewall_ua_whitelist)
            fixture_file="canonical-csv-values.conf"
            ;;
        canonical-space:iface|canonical-space:bypass_ports|canonical-space:bypass_ips)
            fixture_file="canonical-space-values.conf"
            ;;
        canonical-space-empty:bypass_ports|canonical-space-empty:bypass_ips)
            fixture_file="canonical-space-empty-values.conf"
            ;;
        *) return 0 ;;
    esac

    while IFS= read -r fixture_line || [ -n "$fixture_line" ]; do
        case "$fixture_line" in
            "$option="*) "$callback" "${fixture_line#*=}" ;;
        esac
    done < "$FIXTURE_DIR/$fixture_file"
}

config_get_bool() {
    local variable="$1"
    local option="$3"
    local default_value="$4"
    local value="$default_value"

    case "$TEST_SCENARIO:$option" in
        legacy-space:proxy_host)
            value="$(fixture_value legacy-space-values.conf "$option")"
            ;;
        canonical-space:proxy_host) value="1" ;;
        complete:enable_firewall_set|complete:Firewall_ua_bypass|complete:Firewall_drop_on_match|complete:firewall_advanced_settings)
            value="1"
            ;;
        legacy-csv:enable_firewall_set)
            value="1"
            ;;
        canonical-csv:enable_firewall_set) value="1" ;;
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

python3 - "$ROOT/openwrt/root/etc/config/UAmask" <<'PY' || fail "OWRT-UCI-001 explicit new-install defaults"
import re
import sys

sections = {}
current = None
for raw_line in open(sys.argv[1], encoding="utf-8"):
    line = raw_line.strip()
    match = re.fullmatch(r"config\s+'UAmask'\s+'([^']+)'", line)
    if match:
        current = match.group(1)
        sections[current] = {}
        continue
    match = re.fullmatch(r"option\s+([A-Za-z0-9_]+)\s+'(.*)'", line)
    if match and current is not None:
        sections[current][match.group(1)] = match.group(2)

assert sections == {
    "enabled": {"enabled": "0"},
    "main": {
        "port": "12032",
        "ua": "FFF",
        "operating_profile": "Medium",
        "buffer_size": "8192",
        "pool_size": "0",
        "cache_size": "2000",
        "gogc_value": "100",
        "match_mode": "keywords",
        "keywords": "iPhone,iPad,Android,Macintosh,Windows",
        "replace_method": "full",
        "log_level": "info",
        "iface": "br-lan",
        "enable_firewall_set": "0",
        "Firewall_ua_bypass": "0",
        "Firewall_ua_whitelist": "",
        "Firewall_drop_on_match": "0",
        "firewall_advanced_settings": "0",
        "firewall_nonhttp_threshold": "5",
        "firewall_decision_delay": "60",
        "firewall_timeout": "28800",
        "bypass_gid": "65533",
        "bypass_ports": "22 443",
        "bypass_ips": "172.16.0.0/12 192.168.0.0/16 127.0.0.0/8 169.254.0.0/16",
        "ua_regex": "(iPhone|iPad|Android|Macintosh|Windows|Linux|Apple|Mac OS X|Mobile)",
        "proxy_host": "0",
        "whitelist": "",
        "log_file": "/tmp/UAmask/UAmask.log",
    },
}
PY
pass "OWRT-UCI-001 all 28 new-install UCI defaults are explicit"

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

TEST_SCENARIO="defaults"
FW_TYPE="nft"
generate_core_config || fail "OWRT-DEFAULT-001 generate missing-value defaults"
python3 - "$CONFIG_PATH" <<'PY' || fail "OWRT-DEFAULT-001 verify missing-value defaults, types, and root fields"
import json
import sys

with open(sys.argv[1], encoding="utf-8") as config_file:
    config = json.load(config_file)

assert set(config) == {"schema_version", "listen", "rewrite", "firewall", "observe", "performance"}
assert config["schema_version"] == 1
assert type(config["schema_version"]) is int
assert config["listen"] == {
    "port": 12032,
    "dial_timeout": "30s",
    "client_keep_alive": "3m",
    "upstream_keep_alive": "3m",
}
assert type(config["listen"]["port"]) is int
assert config["rewrite"] == {
    "user_agent": "FFF",
    "mode": "keywords",
    "keywords": ["iPhone", "iPad", "Android", "Macintosh", "Windows"],
    "whitelist": [],
    "pattern": "(iPhone|iPad|Android|Macintosh|Windows|Linux)",
    "partial_replace": False,
}
assert type(config["rewrite"]["partial_replace"]) is bool
assert config["firewall"] == {
    "backend": "nft",
    "set_name": "UAmask_bypass_set",
    "ua_whitelist": [],
    "drop_on_match": False,
    "whitelist_timeout": "24h",
    "non_http": {
        "enabled": False,
        "threshold": 5,
        "decision_delay": "60s",
        "http_cooldown": "1h",
        "timeout": "28800s",
        "cleanup_interval": "10m",
    },
}
assert type(config["firewall"]["non_http"]["enabled"]) is bool
assert type(config["firewall"]["non_http"]["threshold"]) is int
assert config["observe"]["log_level"] == "info"
assert config["observe"]["stats_file"] == "/tmp/UAmask.stats"
assert config["observe"]["stats_interval"] == "5s"
assert config["performance"] == {
    "profile": "Medium",
    "cache_size": 2000,
    "buffer_size": 8192,
    "pool_size": 0,
    "gc_percent": 100,
}
assert all(type(config["performance"][key]) is int for key in ("cache_size", "buffer_size", "pool_size", "gc_percent"))

# UCI option names are not schema-v1 root fields. Some are service/firewall
# only; others map to explicit nested JSON fields asserted above.
uci_option_names = {
    "enabled",
    "iface",
    "enable_firewall_set",
    "Firewall_ua_bypass",
    "Firewall_drop_on_match",
    "proxy_host",
    "bypass_gid",
    "bypass_ports",
    "bypass_ips",
    "firewall_advanced_settings",
    "firewall_nonhttp_threshold",
    "firewall_decision_delay",
    "firewall_timeout",
}
assert uci_option_names.isdisjoint(config)
PY
"$PROG" -dump-effective-config -config "$CONFIG_PATH" > "$TMP_DIR/default-effective.json" || fail "OWRT-DEFAULT-001 dump effective defaults"
python3 - "$TMP_DIR/default-effective.json" <<'PY' || fail "OWRT-DEFAULT-001 verify effective medium defaults"
import json
import sys

with open(sys.argv[1], encoding="utf-8") as config_file:
    performance = json.load(config_file)["performance"]

assert performance == {
    "profile": "medium",
    "cache_size": 3000,
    "buffer_size": 8192,
    "pool_size": 500,
    "gc_percent": 100,
}
PY
pass "OWRT-DEFAULT-001 missing-value defaults, schema types, and UCI root-field exclusions"

FW_TYPE="ipt"
generate_core_config || fail "OWRT-BACKEND-001 generate iptables backend config"
python3 - "$CONFIG_PATH" <<'PY' || fail "OWRT-BACKEND-001 verify detected backend mapping"
import json
import sys

with open(sys.argv[1], encoding="utf-8") as config_file:
    assert json.load(config_file)["firewall"]["backend"] == "ipt"
PY
FW_TYPE="nft"
pass "OWRT-BACKEND-001 detected nftables/iptables backend mapping"

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

TEST_SCENARIO="legacy-csv"
generate_core_config || fail "OWRT-LIST-001 generate legacy CSV fixture"
python3 - "$CONFIG_PATH" <<'PY' || fail "OWRT-LIST-001 verify CSV trimming and empty elements"
import json
import sys

with open(sys.argv[1], encoding="utf-8") as config_file:
    config = json.load(config_file)

assert config["rewrite"]["user_agent"] == 'Mask "UA" \\\\ path'
assert config["rewrite"]["keywords"] == ["Android", "Linux", "OpenHarmony"]
assert config["rewrite"]["whitelist"] == ['Client "A"', "真实 UA"]
assert config["firewall"]["ua_whitelist"] == ['Steam "Deck"', "游戏\\\\客户端"]
raw = open(sys.argv[1], encoding="utf-8").read()
assert "真实 UA" in raw and "游戏" in raw
assert "\\\\" in raw
PY
pass "OWRT-LIST-001 legacy CSV trimming, empty elements, Unicode, and backslashes"

# Canonical UCI list entries must produce the same JSON and effective profile
# as their legacy scalar equivalents, including empty items and values that
# contain quotes, Unicode, and backslashes.
cp "$CONFIG_PATH" "$TMP_DIR/legacy-csv.json"
"$PROG" -dump-effective-config -config "$CONFIG_PATH" > "$TMP_DIR/legacy-csv-effective.json" || fail "OWRT-LIST-003 dump legacy CSV effective config"
TEST_SCENARIO="canonical-csv"
generate_core_config || fail "OWRT-LIST-003 generate canonical UCI-list fixture"
cmp "$TMP_DIR/legacy-csv.json" "$CONFIG_PATH" >/dev/null || fail "OWRT-LIST-003 scalar/list JSON mismatch"
"$PROG" -dump-effective-config -config "$CONFIG_PATH" > "$TMP_DIR/canonical-csv-effective.json" || fail "OWRT-LIST-003 dump canonical UCI-list effective config"
cmp "$TMP_DIR/legacy-csv-effective.json" "$TMP_DIR/canonical-csv-effective.json" >/dev/null || fail "OWRT-LIST-003 scalar/list effective config mismatch"
pass "OWRT-LIST-003 canonical CSV UCI lists match legacy JSON and effective config"

# The firewall-only legacy fields are shell-whitespace-separated rather than
# comma-separated. Exercise the real nft rule formatter with command stubs so
# this stays host-safe and proves each scalar word remains an individual item.
uci() { :; }
fw4() { :; }
nft() { :; }
NFT_PATH="$TMP_DIR/legacy-space.nft"
TEST_SCENARIO="legacy-space"
FW_TYPE="nft"
set_firewall_nft || fail "OWRT-LIST-002 generate legacy whitespace firewall fixture"
python3 - "$NFT_PATH" <<'PY' || fail "OWRT-LIST-002 verify whitespace firewall fixture"
from pathlib import Path
import sys

rules = Path(sys.argv[1]).read_text(encoding="utf-8")
assert 'iifname { "br-lan", "eth0" }' in rules
assert 'ip daddr != { 172.16.0.0/12, 192.168.0.0/16 }' in rules
assert 'tcp dport != { 22, 443, 8080 }' in rules
assert "redirect to :12032" in rules
assert "chain UAmask_output_after" in rules
assert "meta skgid != { 65533, 65534 }" in rules
PY
cp "$NFT_PATH" "$TMP_DIR/legacy-space-expected.nft"
NFT_PATH="$TMP_DIR/canonical-space.nft"
TEST_SCENARIO="canonical-space"
set_firewall_nft || fail "OWRT-LIST-004 generate canonical whitespace UCI-list fixture"
cmp "$TMP_DIR/legacy-space-expected.nft" "$NFT_PATH" >/dev/null || fail "OWRT-LIST-004 scalar/list firewall rule mismatch"
pass "OWRT-LIST-004 canonical whitespace UCI lists match legacy firewall rules"
NFT_PATH="$TMP_DIR/canonical-space-empty.nft"
TEST_SCENARIO="canonical-space-empty"
set_firewall_nft || fail "OWRT-LIST-005 generate empty canonical whitespace UCI-list fixture"
python3 - "$NFT_PATH" <<'PY' || fail "OWRT-LIST-005 verify empty canonical whitespace UCI-list fixture"
from pathlib import Path
import sys

rules = Path(sys.argv[1]).read_text(encoding="utf-8")
assert "# No bypass IPs configured" in rules
assert "# No bypass ports configured" in rules
assert "ip daddr !=" not in rules
assert "tcp dport !=" not in rules
PY
pass "OWRT-LIST-005 empty canonical whitespace UCI lists"
NFT_PATH="$TMP_DIR/legacy-space-empty.nft"
TEST_SCENARIO="legacy-space-empty"
set_firewall_nft || fail "OWRT-LIST-002 generate empty whitespace firewall fixture"
python3 - "$NFT_PATH" <<'PY' || fail "OWRT-LIST-002 verify empty whitespace firewall fixture"
from pathlib import Path
import sys

rules = Path(sys.argv[1]).read_text(encoding="utf-8")
assert "# No bypass IPs configured" in rules
assert "# No bypass ports configured" in rules
assert "ip daddr !=" not in rules
assert "tcp dport !=" not in rules
PY
pass "OWRT-LIST-002 legacy whitespace devices, ports, addresses, and empty inputs"

# Invalid modes and invalid Go RE2 patterns must be rejected before the
# candidate replaces the last valid generated JSON.
cp "$CONFIG_PATH" "$TMP_DIR/pre-invalid-regex.json"
TEST_SCENARIO="invalid-regex"
if generate_core_config; then
    fail "OWRT-VALIDATE-001 invalid regex was accepted"
fi
cmp "$TMP_DIR/pre-invalid-regex.json" "$CONFIG_PATH" >/dev/null || fail "OWRT-VALIDATE-001 invalid regex replaced previous config"
pass "OWRT-VALIDATE-001 invalid regex is rejected and previous config is retained"

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

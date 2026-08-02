#!/bin/sh

set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

ADAPTER="$ROOT/openwrt/root/usr/libexec/uamask-luci"
FIXTURE_DIR="$ROOT/openwrt/tests/fixtures/adapter"

fail() {
	echo "FAIL $1" >&2
	exit 1
}

pass() {
	echo "PASS $1"
}

mkdir -p "$TMP_DIR/bin" "$TMP_DIR/run" "$TMP_DIR/log"

# The production adapter uses libubox's jshn.sh.  Reuse the repository's
# host-safe fixture renderer so these tests exercise the same action calls
# without requiring a router or touching any host service path.
export JSON_OPS="$TMP_DIR/json.ops"
export JSON_RENDERER="$ROOT/openwrt/tests/render-json-ops.py"
export JSHN_LIB="$ROOT/openwrt/tests/fake-jshn.sh"
export CORE_CONFIG_DIR="$TMP_DIR/run"
export CORE_CONFIG_PATH="$TMP_DIR/run/config.json"
export LAST_ERROR_PATH="$TMP_DIR/run/last_error"
export STATS_PATH="$TMP_DIR/stats"
export UAMASK_PROG="$TMP_DIR/UAmask"
export UCI_BIN="$TMP_DIR/uci"
export UBUS_BIN="$TMP_DIR/ubus"
export JSONFILTER_BIN="$TMP_DIR/jsonfilter"
export VERSION_FILE_PATH="$TMP_DIR/missing-version-file"
export OPKG_CONTROL_PATH="$TMP_DIR/UAmask.control"
export APK_DATABASE_PATH="$TMP_DIR/missing-apk-database"
export ADAPTER_TEST_ENABLED=0
export ADAPTER_TEST_LOG_PATH="$TMP_DIR/log/UAmask.log"
export ALLOWED_TMP_LOG_ROOT="$TMP_DIR/log"
export ALLOWED_VAR_LOG_ROOT="$TMP_DIR/missing-var-log"
export PATH="$TMP_DIR/bin:$PATH"

cp "$FIXTURE_DIR/stats.txt" "$STATS_PATH"
printf '%s\n' '{"schema_version":1,"candidate_only":true}' > "$CORE_CONFIG_PATH"
cp "$FIXTURE_DIR/log.txt" "$ADAPTER_TEST_LOG_PATH"
printf '%s\n' 'Version: 0.4.3-test' > "$OPKG_CONTROL_PATH"

cat > "$UCI_BIN" <<'EOF'
#!/bin/sh
case "$*" in
    *UAmask.main.log_file)
        printf '%s' "${ADAPTER_TEST_LOG_PATH-}"
        ;;
    *UAmask.enabled.enabled)
        printf '%s' "${ADAPTER_TEST_ENABLED-0}"
        ;;
    *)
        exit 1
        ;;
esac
EOF
chmod 0755 "$UCI_BIN"

cat > "$UAMASK_PROG" <<'EOF'
#!/bin/sh
if [ "$#" -eq 3 ] && [ "$1" = "-dump-effective-config" ] && [ "$2" = "-config" ]; then
    [ -r "$3" ] || exit 1
    cat "${ADAPTER_TEST_EFFECTIVE_CONFIG:?}"
    exit 0
fi
exit 1
EOF
chmod 0755 "$UAMASK_PROG"
export ADAPTER_TEST_EFFECTIVE_CONFIG="$FIXTURE_DIR/effective.json"

cat > "$UBUS_BIN" <<'EOF'
#!/bin/sh
printf '%s\n' "${ADAPTER_TEST_SERVICE_JSON:-{}}"
EOF
chmod 0755 "$UBUS_BIN"

cat > "$JSONFILTER_BIN" <<'EOF'
#!/bin/sh
json=""
expression=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        -s) json="$2"; shift 2 ;;
        -e) expression="$2"; shift 2 ;;
        *) exit 1 ;;
    esac
done
case "$expression" in
    *.running)
        printf '%s' "$json" | sed -n 's/.*"running"[[:space:]]*:[[:space:]]*\(true\|false\).*/\1/p'
        ;;
    *.exit_code)
        printf '%s' "$json" | sed -n 's/.*"exit_code"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p'
        ;;
    *.UAmask)
        case "$json" in *'"instances"'*'"UAmask"'*) printf '%s' '{}' ;; esac
        ;;
esac
EOF
chmod 0755 "$JSONFILTER_BIN"

cat > "$TMP_DIR/bin/fw4" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 0755 "$TMP_DIR/bin/fw4"

cat > "$TMP_DIR/bin/pidof" <<'EOF'
#!/bin/sh
if [ "${ADAPTER_TEST_PID-}" != "" ]; then
    printf '%s\n' "$ADAPTER_TEST_PID"
    exit 0
fi
exit 1
EOF
chmod 0755 "$TMP_DIR/bin/pidof"

run_json() {
    JSON_OUTPUT="$TMP_DIR/output.json"
    if "$ADAPTER" "$@" > "$JSON_OUTPUT"; then
        JSON_STATUS=0
    else
        JSON_STATUS=$?
    fi
}

expect_json() {
    jq -e "$1" "$TMP_DIR/output.json" >/dev/null || {
        cat "$TMP_DIR/output.json" >&2
        fail "$2"
    }
}

# OpenWrt 23.05's platform jshn.sh reads and then unsets JSON_UNSET during
# json_init(), before the first json_add_* expands the accumulator. Keep a
# fixture that performs this full call path so the regression cannot be masked
# by the lightweight host renderer above.
NOUNSET_JSHN="$TMP_DIR/jshn-nounset.sh"
cat > "$NOUNSET_JSHN" <<EOF
#!/bin/sh
. "$ROOT/openwrt/tests/fake-jshn.sh"
_json_get_var() {
    eval "\$1=\\\"\\\$\${JSON_PREFIX}\$2\\\""
}
json_cleanup() {
    local unset
    _json_get_var unset JSON_UNSET
    unset JSON_UNSET
}
json_init() {
    json_cleanup
    : "\$JSON_OPS"
    : > "\$JSON_OPS"
}
json_add_boolean() {
    [ "\${JSON_UNSET+x}" = x ] || return 97
    json_record boolean "\$1" "\$2"
}
EOF
chmod 0755 "$NOUNSET_JSHN"
JSHN_LIB="$NOUNSET_JSHN"
unset JSON_PREFIX JSON_UNSET
run_json status
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-000 23.05 jshn status nounset compatibility"
expect_json '.ok == true and .action == "status"' "LUI-ADAPTER-000 23.05 jshn status output"
run_json stats
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-000 23.05 jshn stats nounset compatibility"
expect_json '.ok == true and .action == "stats"' "LUI-ADAPTER-000 23.05 jshn stats output"
pass "LUI-ADAPTER-000 23.05 jshn JSON_PREFIX/JSON_UNSET initialization"
JSHN_LIB="$ROOT/openwrt/tests/fake-jshn.sh"

# Syntax is part of the adapter contract and must remain valid on ash/dash.
sh -n "$ADAPTER" || fail "LUI-ADAPTER-000 shell syntax"
pass "LUI-ADAPTER-000 shell syntax"

# Disabled is distinct from enabled-but-not-running, and does not need a PID.
unset ADAPTER_TEST_PID
export ADAPTER_TEST_SERVICE_JSON='{}'
rm -f "$LAST_ERROR_PATH"
ADAPTER_TEST_ENABLED=0
run_json status
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-001 disabled status command"
expect_json '.ok == true and .state == "disabled" and .status == "disabled" and .enabled == false' \
    "LUI-ADAPTER-001 disabled status"
pass "LUI-ADAPTER-001 disabled status"

# A registered, not-yet-running procd instance distinguishes starting from
# the enabled-but-not-running anomaly without requiring a writable marker.
ADAPTER_TEST_ENABLED=1
export ADAPTER_TEST_SERVICE_JSON='{"UAmask":{"instances":{"UAmask":{"running":false}}}}'
run_json status
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-002 starting status command"
expect_json '.state == "starting" and .anomaly == ""' "LUI-ADAPTER-002 starting status"
pass "LUI-ADAPTER-002 starting status"

# Running status includes the installed binary version, detected backend, and
# all metrics emitted by stats.FileReporter.
export ADAPTER_TEST_SERVICE_JSON='{"UAmask":{"instances":{"UAmask":{"running":true}}}}'
export ADAPTER_TEST_PID=4242
run_json status
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-003 running status command"
expect_json '.state == "running" and .pid == "4242" and .version == "0.4.3-test" and .backend == "nft" and .stats.available == true and .stats.current_connections == 7 and .stats.total_requests == 123 and .stats.rps == "4.50" and .stats.total_cache_ratio == "10.57"' \
    "LUI-ADAPTER-003 running status"
pass "LUI-ADAPTER-003 running status, version, backend, and stats"

# A rejected new configuration does not make the preserved healthy process
# disappear. Keep the runtime state running and expose the configuration
# problem separately for actionable UI messaging.
cp "$FIXTURE_DIR/last_error" "$LAST_ERROR_PATH"
run_json status
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-003 running-with-error status command"
expect_json '.state == "running" and .anomaly == "configuration-error" and .last_error.present == true' \
    "LUI-ADAPTER-003 running with rejected configuration"
pass "LUI-ADAPTER-003 healthy runtime with rejected configuration"

# A persisted validation/start artifact is authoritative for failed state and
# is returned as structured stage/message data.
unset ADAPTER_TEST_PID
export ADAPTER_TEST_SERVICE_JSON='{"UAmask":{"instances":{"UAmask":{"running":false,"exit_code":1}}}}'
cp "$FIXTURE_DIR/last_error" "$LAST_ERROR_PATH"
run_json status
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-004 failed status command"
expect_json '.state == "failed" and .last_error.present == true and .last_error.stage == "validation" and (.last_error.message | contains("invalid rewrite mode"))' \
    "LUI-ADAPTER-004 failed status"
pass "LUI-ADAPTER-004 failed status and last error"

# When enabled with no process, marker, or error, report the anomalous state
# instead of collapsing it into a generic stopped value.
rm -f "$LAST_ERROR_PATH"
export ADAPTER_TEST_SERVICE_JSON='{}'
run_json status
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-005 anomaly status command"
expect_json '.state == "enabled-not-running" and .anomaly == "enabled-but-not-running"' \
    "LUI-ADAPTER-005 enabled-not-running anomaly"
pass "LUI-ADAPTER-005 enabled-not-running anomaly"

# Standalone fixed actions expose version and stats without accepting extra
# browser-controlled arguments.
run_json version
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-006 version command"
expect_json '.ok == true and .version == "0.4.3-test"' "LUI-ADAPTER-006 version action"
run_json stats
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-006 stats command"
expect_json '.ok == true and .stats.total_requests == 123' "LUI-ADAPTER-006 stats action"
pass "LUI-ADAPTER-006 fixed version and stats actions"

# Effective JSON is returned as escaped text; no arbitrary config path can be
# supplied by the caller.
run_json effective-config
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-007 effective config command"
expect_json '.ok == true and (.content | fromjson | .listen.port) == 12032 and ((.content | fromjson) | has("candidate_only") | not) and .effective_config == .content' \
    "LUI-ADAPTER-007 effective config"
pass "LUI-ADAPTER-007 effective config action"

# Logs are capped to the latest 200 lines and use the internally resolved UCI
# path.  Generate more than the cap so the boundary is directly observable.
: > "$ADAPTER_TEST_LOG_PATH"
i=1
while [ "$i" -le 205 ]; do
    printf 'line-%03d\n' "$i" >> "$ADAPTER_TEST_LOG_PATH"
    i=$((i + 1))
done
run_json logs
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-008 logs command"
expect_json '.ok == true and .available == true and .lines == 200 and (.content | startswith("line-006")) and (.content | contains("line-205")) and (.content | contains("line-005") | not)' \
    "LUI-ADAPTER-008 bounded log tail"
pass "LUI-ADAPTER-008 bounded log tail"

# Missing and explicitly empty paths produce a structured failure and never
# fall back to a browser-provided path.
ADAPTER_TEST_LOG_PATH="$TMP_DIR/log/missing.log"
run_json logs
[ "$JSON_STATUS" -ne 0 ] || fail "LUI-ADAPTER-009 missing log unexpectedly succeeded"
expect_json '.ok == false and (.error | contains("unavailable"))' "LUI-ADAPTER-009 missing log"
ADAPTER_TEST_LOG_PATH=""
run_json logs
[ "$JSON_STATUS" -ne 0 ] || fail "LUI-ADAPTER-009 empty log unexpectedly succeeded"
expect_json '.ok == false and (.error | contains("disabled"))' "LUI-ADAPTER-009 empty configured log"
pass "LUI-ADAPTER-009 missing and disabled logs"

# A path containing shell metacharacters remains data.  The adapter quotes it
# for tail and does not create the marker named after the injected command.
INJECTED_MARKER="$TMP_DIR/injected-marker"
ADAPTER_TEST_LOG_PATH="$TMP_DIR/missing;touch $INJECTED_MARKER"
run_json logs
[ "$JSON_STATUS" -ne 0 ] || fail "LUI-ADAPTER-010 malicious path unexpectedly succeeded"
[ ! -e "$INJECTED_MARKER" ] || fail "LUI-ADAPTER-010 configured path was executed"
expect_json '.ok == false' "LUI-ADAPTER-010 malicious-looking path response"
pass "LUI-ADAPTER-010 configured path isolation"

# A path lexically inside the allowed directory cannot escape through a
# symbolic link for either reads or clear-log.
printf '%s\n' secret-data > "$TMP_DIR/secret.log"
ln -s "$TMP_DIR/secret.log" "$TMP_DIR/log/escaped.log"
ADAPTER_TEST_LOG_PATH="$TMP_DIR/log/escaped.log"
run_json logs
[ "$JSON_STATUS" -ne 0 ] || fail "LUI-ADAPTER-010 symlink escape unexpectedly read"
run_json clear-log confirm
[ "$JSON_STATUS" -ne 0 ] || fail "LUI-ADAPTER-010 symlink escape unexpectedly cleared"
grep -q '^secret-data$' "$TMP_DIR/secret.log" || fail "LUI-ADAPTER-010 symlink target was modified"
pass "LUI-ADAPTER-010 symlink escape rejection"

# Restore a real configured file and require a fixed confirmation token before
# truncation.  The action never accepts a path argument.
ADAPTER_TEST_LOG_PATH="$TMP_DIR/log/UAmask.log"
printf '%s\n' before-clear > "$ADAPTER_TEST_LOG_PATH"
run_json clear-log
[ "$JSON_STATUS" -ne 0 ] || fail "LUI-ADAPTER-011 unconfirmed clear unexpectedly succeeded"
expect_json '.ok == false and (.error | contains("confirmation"))' "LUI-ADAPTER-011 clear confirmation"
grep -q '^before-clear$' "$ADAPTER_TEST_LOG_PATH" || fail "LUI-ADAPTER-011 unconfirmed clear changed log"
run_json clear-log confirm
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-011 confirmed clear command"
expect_json '.ok == true and .cleared == true' "LUI-ADAPTER-011 confirmed clear"
[ ! -s "$ADAPTER_TEST_LOG_PATH" ] || fail "LUI-ADAPTER-011 confirmed clear left log data"
pass "LUI-ADAPTER-011 confirmed clear-log"

# Unknown actions, extra arguments, and browser-supplied paths are rejected.
run_json status "$ADAPTER_TEST_LOG_PATH"
[ "$JSON_STATUS" -eq 64 ] || fail "LUI-ADAPTER-012 status accepted a path argument"
expect_json '.ok == false and (.error | contains("arguments"))' "LUI-ADAPTER-012 status argument rejection"
run_json logs "$ADAPTER_TEST_LOG_PATH"
[ "$JSON_STATUS" -eq 64 ] || fail "LUI-ADAPTER-012 logs accepted a path argument"
run_json totally-unknown
[ "$JSON_STATUS" -eq 64 ] || fail "LUI-ADAPTER-012 unknown action accepted"
expect_json '.ok == false and .error == "unknown action"' "LUI-ADAPTER-012 unknown action rejection"
pass "LUI-ADAPTER-012 fixed action and argument rejection"

# Missing stats and effective/error artifacts remain parseable and actionable.
rm -f "$STATS_PATH" "$CORE_CONFIG_PATH" "$LAST_ERROR_PATH"
run_json stats
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-013 missing stats command"
expect_json '.ok == true and .stats.available == false and .stats.total_requests == 0' "LUI-ADAPTER-013 missing stats"
run_json last-error
[ "$JSON_STATUS" -eq 0 ] || fail "LUI-ADAPTER-013 missing error command"
expect_json '.ok == true and .last_error.present == false' "LUI-ADAPTER-013 missing error artifact"
run_json effective-config
[ "$JSON_STATUS" -ne 0 ] || fail "LUI-ADAPTER-013 missing config unexpectedly succeeded"
expect_json '.ok == false and (.error | contains("unavailable"))' "LUI-ADAPTER-013 missing effective config"
pass "LUI-ADAPTER-013 missing artifact recovery responses"

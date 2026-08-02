#!/bin/sh

set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
MENU="$ROOT/openwrt/luci/root/usr/share/luci/menu.d/luci-app-uamask.json"
ACL="$ROOT/openwrt/luci/root/usr/share/rpcd/acl.d/luci-app-uamask.json"
VIEW_DIR="$ROOT/openwrt/luci/htdocs/luci-static/resources/view/uamask"

fail() {
	echo "FAIL $1" >&2
	exit 1
}

pass() {
	echo "PASS $1"
}

jq -e 'type == "object"' "$MENU" >/dev/null || fail "LUI-JS-001 menu JSON"
jq -e '
	keys == [
		"admin/services/uamask",
		"admin/services/uamask/diagnostics",
		"admin/services/uamask/overview",
		"admin/services/uamask/settings"
	] and
	.["admin/services/uamask"].action.type == "firstchild" and
	.["admin/services/uamask"].depends.acl == ["luci-app-uamask"] and
	.["admin/services/uamask/overview"].action == {"type":"view","path":"uamask/overview"} and
	.["admin/services/uamask/settings"].action == {"type":"view","path":"uamask/settings"} and
	.["admin/services/uamask/diagnostics"].action == {"type":"view","path":"uamask/diagnostics"}
' "$MENU" >/dev/null || fail "LUI-JS-001 route contract"
pass "LUI-JS-001 lowercase menu and route contract"

jq -e 'type == "object" and has("luci-app-uamask")' "$ACL" >/dev/null || fail "LUI-JS-002 ACL JSON"
jq -e '
	.["luci-app-uamask"].read.uci == ["UAmask"] and
	.["luci-app-uamask"].write.uci == ["UAmask"] and
	(.["luci-app-uamask"].read.file | keys) == ["/usr/libexec/uamask-luci"] and
	(.["luci-app-uamask"].write.file | keys) == ["/etc/init.d/UAmask", "/usr/libexec/uamask-luci"] and
	.["luci-app-uamask"].read.file["/usr/libexec/uamask-luci"] == ["exec"] and
	.["luci-app-uamask"].write.file["/usr/libexec/uamask-luci"] == ["exec"] and
	.["luci-app-uamask"].write.file["/etc/init.d/UAmask"] == ["exec"] and
	(.["luci-app-uamask"].read | has("ubus") | not) and
	(.["luci-app-uamask"].write | has("ubus") | not)
' "$ACL" >/dev/null || fail "LUI-JS-002 least-privilege ACL"
if jq -e '.. | strings | select(contains("*"))' "$ACL" >/dev/null; then
	fail "LUI-JS-002 wildcard ACL"
fi
pass "LUI-JS-002 UAmask-only UCI, adapter, and init ACL"

for view in overview settings diagnostics; do
	path="$VIEW_DIR/$view.js"
	[ -f "$path" ] || fail "LUI-JS-003 missing required $view view"
	node --check "$path" >/dev/null || fail "LUI-JS-003 $view syntax"
	if grep -Eq '(React|Vue|Svelte|Tailwind|fonts\.googleapis|chart\.js)' "$path"; then
		fail "LUI-JS-003 $view forbidden frontend dependency"
	fi
	pass "LUI-JS-003 $view view syntax"
done

# LuCI's DOM helper converts null entries in child arrays to literal text.
# Check only the render ranges where optional nodes are assembled, rather than
# rejecting legitimate null state sentinels elsewhere in the views.
assert_no_optional_null() {
	file="$1"
	start="$2"
	end="$3"
	label="$4"

	if awk -v start="$start" -v end="$end" '
		index($0, start) { scanning = 1 }
		scanning && /:[[:space:]]*null[[:space:]]*[,)]/ { found = 1 }
		scanning && index($0, end) { scanning = 0 }
		END { exit(found ? 0 : 1) }
	' "$file"; then
		fail "LUI-JS-004 $label contains an optional null child"
	fi
}

assert_no_optional_null "$VIEW_DIR/overview.js" 'function metricsSection' 'function healthSection' 'overview metrics'
assert_no_optional_null "$VIEW_DIR/overview.js" 'function updateView' 'function currentRiskState' 'overview update'
assert_no_optional_null "$VIEW_DIR/diagnostics.js" 'function draw' 'draw();' 'diagnostics feedback'

# Recovery is optional in the diagnostics status section. It must be appended
# only when present, not inserted as a null child in the returned collection.
if awk '
	/function statusSection/ { scanning = 1 }
	scanning && /^[[:space:]]*recovery,[[:space:]]*$/ { found = 1 }
	scanning && /function logsSection/ { scanning = 0 }
	END { exit(found ? 0 : 1) }
' "$VIEW_DIR/diagnostics.js"; then
	fail "LUI-JS-004 diagnostics recovery contains an optional null child"
fi
pass "LUI-JS-004 no rendered null placeholders"

# A DynamicList always keeps a blank add-control input next to its saved
# items.  Its validator must accept that blank control while still validating
# every non-empty item.  Count the four explicit allow-blank guards instead of
# depending on generated DOM ids or LuCI version-specific markup.
blank_item_guards=$(grep -F -c 'if (allowBlankDynamicListItem(value))' "$VIEW_DIR/settings.js" || true)
[ "$blank_item_guards" -eq 4 ] || fail "LUI-JS-005 DynamicList validators must allow blank add-controls"
pass "LUI-JS-005 DynamicList blank add-controls remain valid"

# Compatibility parsing must write active, valid list arrays on every save so
# an unchanged legacy scalar is normalized to a canonical UCI list. Inactive
# retained options and invalid values still fall through to the inherited
# parser. Inspect only the helper's source range; generated field ids are not
# part of this contract.
compat_parse=$(sed -n '/option.parse = function(section_id)/,/inheritedParse.call/p' "$VIEW_DIR/settings.js")
printf '%s\n' "$compat_parse" | grep -Fq 'this.isActive(section_id) && this.isValid(section_id)' || fail "LUI-JS-006 compatibility parser must gate writes on active valid fields"
printf '%s\n' "$compat_parse" | grep -Fq 'if (Array.isArray(value))' || fail "LUI-JS-006 compatibility parser must handle list arrays"
printf '%s\n' "$compat_parse" | grep -Fq 'return Promise.resolve(this.write(section_id, value));' || fail "LUI-JS-006 compatibility parser must write unchanged arrays"
printf '%s\n' "$compat_parse" | grep -Fq 'return inheritedParse.call(this, section_id);' || fail "LUI-JS-006 compatibility parser must retain inherited fallback"
pass "LUI-JS-006 unchanged legacy list values canonicalize on save"

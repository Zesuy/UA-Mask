#!/bin/sh

set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
PACKAGE="$ROOT/openwrt/package/Makefile"

fail() {
	echo "FAIL $1" >&2
	exit 1
}

require_text() {
	grep -F "$1" "$PACKAGE" >/dev/null || fail "$2"
}

require_text '+libubox +luci-base' "LUI-PKG-001 native LuCI dependencies"
require_text 'openwrt/root/usr/libexec/uamask-luci $(1)/usr/libexec/uamask-luci' "LUI-PKG-001 adapter install"
require_text 'luci/menu.d/luci-app-uamask.json $(1)/usr/share/luci/menu.d/luci-app-uamask.json' "LUI-PKG-001 menu install"
require_text 'rpcd/acl.d/luci-app-uamask.json $(1)/usr/share/rpcd/acl.d/luci-app-uamask.json' "LUI-PKG-001 ACL install"
require_text 'view/uamask/overview.js $(1)/www/luci-static/resources/view/uamask/overview.js' "LUI-PKG-001 overview install"
require_text 'view/uamask/settings.js $(1)/www/luci-static/resources/view/uamask/settings.js' "LUI-PKG-001 settings install"
require_text 'view/uamask/diagnostics.js $(1)/www/luci-static/resources/view/uamask/diagnostics.js' "LUI-PKG-001 diagnostics install"

if grep -F 'luci-compat' "$PACKAGE" >/dev/null; then
	fail "LUI-PKG-001 luci-compat remains"
fi
if grep -F 'openwrt/luci/model/cbi/UAmask.lua' "$PACKAGE" >/dev/null; then
	fail "LUI-PKG-001 legacy CBI install remains"
fi
if grep -F 'openwrt/luci/controller/UAmask.lua' "$PACKAGE" >/dev/null; then
	fail "LUI-PKG-001 legacy controller install remains"
fi

[ -x "$ROOT/openwrt/root/usr/libexec/uamask-luci" ] || fail "LUI-PKG-001 adapter source executable"
jq -e 'type == "object"' "$ROOT/openwrt/luci/root/usr/share/luci/menu.d/luci-app-uamask.json" >/dev/null || fail "LUI-PKG-001 menu source"
jq -e 'type == "object"' "$ROOT/openwrt/luci/root/usr/share/rpcd/acl.d/luci-app-uamask.json" >/dev/null || fail "LUI-PKG-001 ACL source"
[ ! -e "$ROOT/openwrt/luci/model/cbi/UAmask.lua" ] || fail "LUI-PKG-001 legacy CBI source remains"
[ ! -e "$ROOT/openwrt/luci/controller/UAmask.lua" ] || fail "LUI-PKG-001 legacy controller source remains"

echo "PASS LUI-PKG-001 native views/adapter/menu/ACL assets without Lua compatibility"

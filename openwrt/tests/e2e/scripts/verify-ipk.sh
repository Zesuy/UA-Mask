#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../../../.." && pwd)
IPK=${1:?usage: verify-ipk.sh /absolute/path/to/UAmask.ipk [identity-output]}
IDENTITY_OUTPUT=${2:-}

[ -f "$IPK" ] || { printf 'IPK does not exist: %s\n' "$IPK" >&2; exit 1; }
case "$(basename "$IPK")" in UAmask_*.ipk) ;; *) printf 'unexpected package filename: %s\n' "$IPK" >&2; exit 1 ;; esac

contents=$(tar -xOf "$IPK" ./data.tar.gz | tar -tzf -)
for asset in \
	./usr/bin/UAmask \
	./etc/init.d/UAmask \
	./etc/config/UAmask \
	./usr/libexec/uamask-luci \
	./usr/share/luci/menu.d/luci-app-uamask.json \
	./usr/share/rpcd/acl.d/luci-app-uamask.json \
	./www/luci-static/resources/view/uamask/overview.js \
	./www/luci-static/resources/view/uamask/settings.js \
	./www/luci-static/resources/view/uamask/diagnostics.js; do
	printf '%s\n' "$contents" | grep -Fx "$asset" >/dev/null || { printf 'missing package asset: %s\n' "$asset" >&2; exit 1; }
done
printf '%s\n' "$contents" | grep -Eq '/luci/(controller|model/cbi)/|\.lua$' && { printf 'legacy Lua asset present\n' >&2; exit 1; }

for view in overview settings diagnostics; do
	package_hash=$(tar -xOf "$IPK" ./data.tar.gz | tar -xzOf - "./www/luci-static/resources/view/uamask/$view.js" | sha256sum | awk '{print $1}')
	source_hash=$(sha256sum "$REPO_ROOT/openwrt/luci/htdocs/luci-static/resources/view/uamask/$view.js" | awk '{print $1}')
	[ "$package_hash" = "$source_hash" ] || { printf '%s view differs from current source\n' "$view" >&2; exit 1; }
done

package_sha=$(sha256sum "$IPK" | awk '{print $1}')
control=$(tar -xOf "$IPK" ./control.tar.gz | tar -xzOf - ./control)
package_name=$(printf '%s\n' "$control" | sed -n 's/^Package: //p')
package_version=$(printf '%s\n' "$control" | sed -n 's/^Version: //p')
[ "$package_name" = UAmask ] || { printf 'unexpected package identity: %s\n' "$package_name" >&2; exit 1; }

if [ -n "$IDENTITY_OUTPUT" ]; then
	printf '{"path":"%s","sha256":"%s","package":"%s","version":"%s"}\n' \
		"$IPK" "$package_sha" "$package_name" "$package_version" > "$IDENTITY_OUTPUT"
fi
printf '%s %s %s\n' "$package_name" "$package_version" "$package_sha"

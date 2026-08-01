#!/bin/sh
set -eu
umask 022

usage() {
	cat <<'EOF'
Usage:
  build-release.sh --platform linux --arch ARCH [options]
  build-release.sh --platform openwrt --arch ARCH --sdk-dir DIR [options]

Options:
  --version VERSION    Version injected into the binary (default: VERSION file)
  --commit SHA         Source commit recorded in metadata (default: git HEAD)
  --output-dir DIR     Artifact directory (default: dist)
  --sdk-dir DIR        Prepared OpenWrt SDK directory (required for openwrt)
  --allow-dirty        Permit a dirty tree and record it in metadata

Supported architectures:
  amd64, 386, arm64, armv5, armv6, armv7,
  mips-softfloat, mipsle-softfloat, riscv64
EOF
}

die() {
	echo "build-release: $*" >&2
	exit 1
}

platform=
arch=
version=
commit=
output_dir=dist
sdk_dir=
allow_dirty=false

while [ "$#" -gt 0 ]; do
	case "$1" in
		--platform|--arch|--version|--commit|--output-dir|--sdk-dir)
			[ "$#" -ge 2 ] || die "missing value for $1"
			case "$1" in
				--platform) platform=$2 ;;
				--arch) arch=$2 ;;
				--version) version=$2 ;;
				--commit) commit=$2 ;;
				--output-dir) output_dir=$2 ;;
				--sdk-dir) sdk_dir=$2 ;;
			esac
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		--allow-dirty)
			allow_dirty=true
			shift
			;;
		*) die "unknown argument: $1" ;;
	esac
done

[ -n "$platform" ] || die "--platform is required"
[ -n "$arch" ] || die "--arch is required"

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)

if [ -z "$version" ]; then
	version=$(tr -d '\r\n' < "$repo_root/VERSION")
fi
if [ -z "$commit" ]; then
	commit=$(git -C "$repo_root" rev-parse HEAD)
fi
dirty=false
if [ -n "$(git -C "$repo_root" status --porcelain --untracked-files=normal)" ]; then
	dirty=true
	[ "$allow_dirty" = true ] || die "working tree is dirty; commit the source or use --allow-dirty for a non-release build"
fi

case "$version" in
	*[!0-9A-Za-z._+-]*|'') die "version contains unsupported characters: $version" ;;
esac
case "$commit" in
	*[!0-9A-Fa-f]*|'') die "commit must be a hexadecimal Git object ID" ;;
esac

goarch=
goarm=
gomips=
case "$arch" in
	amd64|386|arm64|riscv64) goarch=$arch ;;
	armv5) goarch=arm; goarm=5 ;;
	armv6) goarch=arm; goarm=6 ;;
	armv7) goarch=arm; goarm=7 ;;
	mips-softfloat) goarch=mips; gomips=softfloat ;;
	mipsle-softfloat) goarch=mipsle; gomips=softfloat ;;
	*) die "unsupported architecture: $arch" ;;
esac

case "$platform" in
	linux|openwrt) ;;
	*) die "unsupported platform: $platform" ;;
esac
if [ "$platform" = openwrt ]; then
	[ -n "$sdk_dir" ] || die "--sdk-dir is required for openwrt builds"
	[ -f "$sdk_dir/include/toplevel.mk" ] || die "not an OpenWrt SDK directory: $sdk_dir"
fi

mkdir -p "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd)
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/uamask-release.XXXXXX")
package_link=
cleanup() {
	if [ -n "$package_link" ]; then
		rm -f "$package_link"
	fi
	rm -rf "$work_dir"
}
trap cleanup EXIT HUP INT TERM

binary="$work_dir/UAmask"
(
	cd "$repo_root/core"
	export CGO_ENABLED=0 GOOS=linux GOARCH="$goarch"
	if [ -n "$goarm" ]; then
		export GOARM="$goarm"
	fi
	if [ -n "$gomips" ]; then
		export GOMIPS="$gomips"
	fi
	go build -trimpath -ldflags "-s -w -X main.version=$version" \
		-o "$binary" ./cmd/UAmask
)

write_metadata() {
	artifact=$1
	metadata=$2
	digest=$(sha256sum "$artifact" | awk '{print $1}')
	go_version=$(go version | sed 's/"/\\"/g')
	cat > "$metadata" <<EOF
{
  "version": "$version",
  "commit": "$commit",
  "dirty": $dirty,
  "platform": "$platform",
  "architecture": "$arch",
  "go_version": "$go_version",
  "artifact": "$(basename "$artifact")",
  "sha256": "$digest"
}
EOF
	printf '%s  %s\n' "$digest" "$(basename "$artifact")" > "$artifact.sha256"
}

if [ "$platform" = linux ]; then
	artifact="$output_dir/UAmask-linux-$arch"
	cp "$binary" "$artifact"
	chmod 0755 "$artifact"
	write_metadata "$artifact" "$artifact.metadata.json"
	printf '%s\n' "$artifact"
	exit 0
fi

package_dir="$sdk_dir/package/UAmask"
[ ! -e "$package_dir" ] || die "SDK package path already exists: $package_dir"
ln -s "$repo_root/openwrt/package" "$package_dir"
package_link=$package_dir

make -C "$sdk_dir" defconfig \
	UAMASK_SOURCE_DIR="$repo_root" \
	UAMASK_PREBUILT="$binary" \
	UAMASK_VERSION="$version"
make -C "$sdk_dir" package/UAmask/clean \
	UAMASK_SOURCE_DIR="$repo_root" \
	UAMASK_PREBUILT="$binary" \
	UAMASK_VERSION="$version"
make -C "$sdk_dir" package/UAmask/compile V=s \
	UAMASK_SOURCE_DIR="$repo_root" \
	UAMASK_PREBUILT="$binary" \
	UAMASK_VERSION="$version"

packages_file="$work_dir/packages"
find "$sdk_dir/bin" -type f \( -name 'UAmask_*.ipk' -o -name 'UAmask-*.apk' \) > "$packages_file"
[ -s "$packages_file" ] || die "OpenWrt SDK completed without producing a UAmask package"

while IFS= read -r package; do
	artifact="$output_dir/$(basename "$package")"
	cp "$package" "$artifact"
	write_metadata "$artifact" "$artifact.metadata.json"
	printf '%s\n' "$artifact"
done < "$packages_file"

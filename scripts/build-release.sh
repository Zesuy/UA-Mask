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
  --openwrt-version V  OpenWrt version recorded in package metadata
  --target TARGET      OpenWrt target recorded in package metadata
  --subtarget TARGET   OpenWrt subtarget recorded in package metadata
  --package-arch ARCH  OpenWrt package architecture and APK filename suffix
  --package-format FMT Expected OpenWrt package format: ipk or apk
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
openwrt_version=
target=
subtarget=
package_arch=
package_format=
allow_dirty=false

while [ "$#" -gt 0 ]; do
	case "$1" in
		--platform|--arch|--version|--commit|--output-dir|--sdk-dir|--openwrt-version|--target|--subtarget|--package-arch|--package-format)
			[ "$#" -ge 2 ] || die "missing value for $1"
			case "$1" in
				--platform) platform=$2 ;;
				--arch) arch=$2 ;;
				--version) version=$2 ;;
				--commit) commit=$2 ;;
				--output-dir) output_dir=$2 ;;
				--sdk-dir) sdk_dir=$2 ;;
				--openwrt-version) openwrt_version=$2 ;;
				--target) target=$2 ;;
				--subtarget) subtarget=$2 ;;
				--package-arch) package_arch=$2 ;;
				--package-format) package_format=$2 ;;
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
	[ -n "$openwrt_version" ] || die "--openwrt-version is required for openwrt builds"
	[ -n "$target" ] || die "--target is required for openwrt builds"
	[ -n "$subtarget" ] || die "--subtarget is required for openwrt builds"
	[ -n "$package_arch" ] || die "--package-arch is required for openwrt builds"
	case "$package_format" in
		ipk|apk) ;;
		*) die "--package-format must be ipk or apk for openwrt builds" ;;
	esac
	for value in "$openwrt_version" "$target" "$subtarget" "$package_arch"; do
		case "$value" in
			*[!0-9A-Za-z._+-]*|'') die "OpenWrt metadata contains unsupported characters: $value" ;;
		esac
	done
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

# apk-tools accepts only numeric dotted upstream versions followed by its
# generated "-rN" release suffix. Canary and CI identifiers therefore remain
# in the binary/artifact metadata, while a non-release APK uses the repository
# VERSION as its installable package version. Release tags require VERSION and
# requested version to match, so published APKs remain fully versioned.
package_version=$version
if [ "$platform" = openwrt ] && [ "$package_format" = apk ]; then
	base_version=$(tr -d '\r\n' < "$repo_root/VERSION")
	if [ "$version" != "$base_version" ]; then
		package_version=$base_version
	fi
fi

write_metadata() {
	artifact=$1
	metadata=$2
	digest=$(sha256sum "$artifact" | awk '{print $1}')
	go_version=$(go version | sed 's/"/\\"/g')
	cat > "$metadata" <<EOF
{
  "version": "$version",
  "package_version": "$package_version",
  "commit": "$commit",
  "dirty": $dirty,
  "platform": "$platform",
  "architecture": "$arch",
  "openwrt_version": "$openwrt_version",
  "target": "$target",
  "subtarget": "$subtarget",
  "package_architecture": "$package_arch",
  "package_format": "$package_format",
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
	UAMASK_VERSION="$package_version"
make -C "$sdk_dir" package/UAmask/clean \
	UAMASK_SOURCE_DIR="$repo_root" \
	UAMASK_PREBUILT="$binary" \
	UAMASK_VERSION="$package_version"
make -C "$sdk_dir" package/UAmask/compile V=s \
	UAMASK_SOURCE_DIR="$repo_root" \
	UAMASK_PREBUILT="$binary" \
	UAMASK_VERSION="$package_version"

packages_file="$work_dir/packages"
case "$package_format" in
	ipk) find "$sdk_dir/bin" -type f -name 'UAmask_*.ipk' > "$packages_file" ;;
	apk) find "$sdk_dir/bin" -type f -name 'UAmask-*.apk' > "$packages_file" ;;
esac
[ -s "$packages_file" ] || die "OpenWrt SDK completed without producing a UAmask package"

while IFS= read -r package; do
	package_name=$(basename "$package")
	if [ "$package_format" = apk ]; then
		case "$package_name" in
			*"_$package_arch.apk") ;;
			*) package_name=${package_name%.apk}_$package_arch.apk ;;
		esac
	fi
	artifact="$output_dir/$package_name"
	cp "$package" "$artifact"
	write_metadata "$artifact" "$artifact.metadata.json"
	printf '%s\n' "$artifact"
done < "$packages_file"

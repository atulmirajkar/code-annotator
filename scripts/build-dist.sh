#!/bin/sh

set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(CDPATH='' cd -- "$script_dir/.." && pwd)
staging_dir=$(mktemp -d "${TMPDIR:-/tmp}/md-viewer-dist.XXXXXX")

cleanup() {
  rm -rf -- "$staging_dir"
}
trap cleanup EXIT HUP INT TERM

cd "$project_dir"

if ! command -v go >/dev/null 2>&1; then
  echo "build-dist: Go is required" >&2
  exit 1
fi

echo "Running tests..."
go test ./...

build_target() {
  target_os=$1
  target_arch=$2
  output_name=$3
  output_dir="$staging_dir/$target_os-$target_arch"

  mkdir -p "$output_dir"
  echo "Building $target_os/$target_arch..."
  GOOS=$target_os GOARCH=$target_arch \
    go build -trimpath -buildvcs=false -ldflags="-s -w" \
    -o "$output_dir/$output_name" ./cmd/md-viewer
}

build_target darwin amd64 md-viewer
build_target darwin arm64 md-viewer
build_target linux amd64 md-viewer
build_target linux arm64 md-viewer
build_target windows amd64 md-viewer.exe
build_target windows arm64 md-viewer.exe

for platform in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64 windows-amd64 windows-arm64; do
  mkdir -p "$project_dir/dist/$platform"
done

install -m 0755 "$staging_dir/darwin-amd64/md-viewer" "$project_dir/dist/darwin-amd64/md-viewer"
install -m 0755 "$staging_dir/darwin-arm64/md-viewer" "$project_dir/dist/darwin-arm64/md-viewer"
install -m 0755 "$staging_dir/linux-amd64/md-viewer" "$project_dir/dist/linux-amd64/md-viewer"
install -m 0755 "$staging_dir/linux-arm64/md-viewer" "$project_dir/dist/linux-arm64/md-viewer"
install -m 0644 "$staging_dir/windows-amd64/md-viewer.exe" "$project_dir/dist/windows-amd64/md-viewer.exe"
install -m 0644 "$staging_dir/windows-arm64/md-viewer.exe" "$project_dir/dist/windows-arm64/md-viewer.exe"

artifacts="
dist/darwin-amd64/md-viewer
dist/darwin-arm64/md-viewer
dist/linux-amd64/md-viewer
dist/linux-arm64/md-viewer
dist/windows-amd64/md-viewer.exe
dist/windows-arm64/md-viewer.exe
"

checksum_file="$staging_dir/SHA256SUMS"
if command -v shasum >/dev/null 2>&1; then
  # Intentional word splitting: each artifact occupies one line above.
  # shellcheck disable=SC2086
  shasum -a 256 $artifacts >"$checksum_file"
elif command -v sha256sum >/dev/null 2>&1; then
  # shellcheck disable=SC2086
  sha256sum $artifacts >"$checksum_file"
else
  echo "build-dist: shasum or sha256sum is required" >&2
  exit 1
fi

install -m 0644 "$checksum_file" "$project_dir/dist/SHA256SUMS"

echo "Built six release binaries in $project_dir/dist"
echo "Updated $project_dir/dist/SHA256SUMS"

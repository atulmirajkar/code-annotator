#!/bin/sh

set -eu

mermaid_version=11.17.0
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(CDPATH='' cd -- "$script_dir/.." && pwd)
staging_dir=$(mktemp -d "${TMPDIR:-/tmp}/md-viewer-mermaid.XXXXXX")

cleanup() {
  rm -rf -- "$staging_dir"
}
trap cleanup EXIT HUP INT TERM

if ! command -v npm >/dev/null 2>&1; then
  echo "update-mermaid: npm is required" >&2
  exit 1
fi

# npm pack gives us the exact published package without creating node_modules or
# adding a JavaScript package manager to the application's normal build path.
archive_name=$(npm pack "@mermaid-js/tiny@$mermaid_version" \
  --pack-destination "$staging_dir" --silent)
archive_path="$staging_dir/$archive_name"
unpacked_dir="$staging_dir/package"

tar -xzf "$archive_path" -C "$staging_dir"

if [ ! -f "$unpacked_dir/dist/mermaid.tiny.js" ] || \
  [ ! -f "$unpacked_dir/LICENSE" ]; then
  echo "update-mermaid: published package is missing required files" >&2
  exit 1
fi

mkdir -p "$project_dir/web/vendor/mermaid"
install -m 0644 "$unpacked_dir/dist/mermaid.tiny.js" \
  "$project_dir/web/vendor/mermaid/mermaid.tiny.js"
install -m 0644 "$unpacked_dir/LICENSE" \
  "$project_dir/web/vendor/mermaid/LICENSE"

echo "Vendored @mermaid-js/tiny@$mermaid_version"

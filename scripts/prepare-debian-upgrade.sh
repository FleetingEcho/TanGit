#!/usr/bin/env bash
set -euo pipefail

# MyGo derives the Debian package name from the distribution slug. Tell dpkg
# that the renamed package takes over the existing installation.
build_dir=$1
shift
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
for deb in "$@"; do
  package="$work/package"
  dpkg-deb --raw-extract "$deb" "$package"
  control="$package/DEBIAN/control"
  test "$(dpkg-deb -f "$deb" Package)" = lazygitgo
  printf 'Conflicts: tangit\nReplaces: tangit\n' >> "$control"
  dpkg-deb --root-owner-group --build "$package" "$work/$(basename "$deb")"
  mv "$work/$(basename "$deb")" "$deb"
  rm -rf "$package"

  # Rewrite the checksum for the replacement package and keep existing assets.
  name=$(basename "$deb")
  awk -v name="$name" '$2 != name' "$build_dir/SHA256SUMS" > "$work/sums"
  (cd "$(dirname "$deb")"; sha256sum "$name") >> "$work/sums"
  sort -k2 "$work/sums" > "$build_dir/SHA256SUMS"
done

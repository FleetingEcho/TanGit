#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

platform=$1
shift
case "$platform" in
  windows/amd64|windows/arm64|windows/amd64,windows/arm64) ;;
  *) printf 'Unsupported Windows build target: %s\n' "$platform" >&2; exit 2 ;;
esac

# The old updater relaunches TanGit.exe. Ship a small compatibility launcher
# beside the canonical executable in installers and signed updates.
IFS=, read -ra targets <<<"$platform"
generated=()
cleanup() {
  for dir in "${generated[@]}"; do
    rm -f "$dir/TanGit.exe"
    rmdir "$dir"
  done
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
for target in "${targets[@]}"; do
  arch=${target#windows/}
  dir=resources/windows-$arch
  mkdir "$dir"
  generated+=("$dir")
  CGO_ENABLED=0 GOOS=windows GOARCH="$arch" go build -trimpath -ldflags '-s -w -H windowsgui' \
    -o "$dir/TanGit.exe" ./packaging/windows/legacylauncher
done
go tool mygo build -platform "$platform" "$@"

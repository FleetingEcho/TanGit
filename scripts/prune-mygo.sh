#!/usr/bin/env bash
# prune-mygo.sh — slim the vendored mygo module copy in third_party/mygo.
#
# lazygitGo vendors github.com/egoist/mygo via a `replace` directive in go.mod.
# The vendored tree only needs what the Go build (and the mygo CLI tool) use;
# upstream's website, docs, examples, benchmark fixtures, JS toolchain and the
# i18n package's multi-MB JSON fixtures only bloat the repo. Run this after
# copying a fresh mygo release/snapshot into third_party/mygo:
#
#   rsync -a --delete "$GOMODCACHE/github.com/egoist/mygo@vX.Y.Z/" third_party/mygo/
#   scripts/prune-mygo.sh
#
# Then re-apply lazygitGo's local patches (see docs or git log of third_party/mygo).
# Anything removed here can be restored from git history or the module cache.
set -euo pipefail

cd "$(dirname "$0")/../third_party/mygo"

rm -rf \
  website \
  docs \
  docs-zh \
  examples \
  benchmarks \
  packages \
  scripts \
  .github \
  .claude \
  i18n/testdata

rm -f \
  bun.lock \
  .bun-version \
  package.json \
  tsconfig.json \
  mise.toml \
  .gitattributes

echo "third_party/mygo pruned."

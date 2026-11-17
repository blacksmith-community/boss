#!/bin/sh
# Regenerates the recorded broker responses.
#
# usage: run.sh <broker-checkout> <output-dir> [<broker-checkout-at-v1.8.2>]
#
# The generators are Go test files that this script lays over the broker
# with `go test -overlay`, so nothing is written inside either checkout.
# Responses from the first checkout land in <output-dir>/head, and
# responses from the v1.8.2 checkout land in <output-dir>/v182. Copy the
# v182 files into testdata/ with a `v182_` prefix.
set -eu

if [ $# -lt 2 ]; then
  sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
fi

HERE=$(cd "$(dirname "$0")" && pwd)
HEAD=$(cd "$1" && pwd)
mkdir -p "$2"
OUT=$(cd "$2" && pwd)
OLD=""
if [ $# -ge 3 ]; then OLD=$(cd "$3" && pwd); fi

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# strip drops the `//go:build ignore` line so the file builds as a test.
strip() { sed '1,2d' "$HERE/$1" > "$WORK/$1"; }

# run <checkout> <package directory> <generator> <output dir>
run() {
  checkout=$1; pkg=$2; gen=$3; dest=$4
  strip "$gen"
  mkdir -p "$dest"
  replace="\"$checkout/$pkg/zz_fixtures_test.go\": \"$WORK/$gen\""
  # The broker only reads its log files from fixed paths, so the
  # blacksmith generator reads from a temporary directory instead.
  if [ "$gen" = blacksmith.go ]; then
    mkdir -p "$WORK/logs"
    sed "s#/var/vcap/sys/log/blacksmith/#$WORK/logs/#g" "$checkout/$pkg/handler.go" > "$WORK/handler.go"
    replace="$replace, \"$checkout/$pkg/handler.go\": \"$WORK/handler.go\""
  fi
  printf '{"Replace": {%s}}\n' "$replace" > "$WORK/overlay.json"
  echo "== $gen"
  (cd "$checkout" && FIXOUT="$dest" FIXLOGS="$WORK/logs" go test -count=1 \
    -overlay="$WORK/overlay.json" -run TestZZFixtures "./$pkg/")
}

run "$HEAD" internal/api api.go "$OUT/head"
run "$HEAD" internal/apierr apierr.go "$OUT/head"
run "$HEAD" internal/broker broker.go "$OUT/head"
run "$HEAD" internal/handlers/blacksmith blacksmith.go "$OUT/head"
run "$HEAD" internal/handlers/bosh bosh.go "$OUT/head"
run "$HEAD" internal/handlers/certificates certificates.go "$OUT/head"
run "$HEAD" internal/handlers/deployments deployments.go "$OUT/head"
run "$HEAD" internal/handlers/services services.go "$OUT/head"
run "$HEAD" internal/handlers/tasks tasks.go "$OUT/head"
run "$HEAD" internal/handlers/upgrade upgrade.go "$OUT/head"

if [ -n "$OLD" ]; then
  run "$OLD" internal/handlers/bosh bosh_v182.go "$OUT/v182"
  run "$OLD" internal/handlers/deployments deployments_v182.go "$OUT/v182"
  run "$OLD" internal/handlers/tasks tasks.go "$OUT/v182"
  run "$OLD" internal/api api.go "$OUT/v182"
fi

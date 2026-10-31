#!/usr/bin/env bash
#
# Corpus golden test: runs the analyzer over pinned commits of real modules
# that use zerolog heavily, and diffs the diagnostics against the golden files
# in testdata/corpus/. A refactor that claims no change in behavior must keep
# this diff empty.
#
# Usage:
#   scripts/corpus.sh            # check every module
#   scripts/corpus.sh -update    # rewrite the golden files
#   scripts/corpus.sh topaz reva # only the named modules (with or without -update)
#
# Each golden line is "relative/file.go:line:col: message", sorted and
# deduplicated (a test variant of a package reports the same diagnostic again).
# Only diagnostics are compared: packages that fail to load (broken deps, build
# errors) are skipped by the analyzer and their errors are dropped, so they do
# not make the output depend on the machine. The analysis runs with
# GOOS=linux GOARCH=amd64 CGO_ENABLED=0, so the set of files is the same on
# every host. Only the root module of each repository is analyzed.
#
# Clones are cached in $ZEROLOGLINTCTX_CORPUS_DIR
# (default: ${XDG_CACHE_HOME:-$HOME/.cache}/zerologlintctx-corpus).
# Requires git, go and jq.

set -euo pipefail

# name  repository URL  commit
corpus=(
  "autobrr     https://github.com/autobrr/autobrr.git         61ac7374e62b80858c461c73545787fece4ab988"
  "cloudflared https://github.com/cloudflare/cloudflared.git  872b241f94871665c1c157adefcf90c11f0ead96"
  "fleet-server https://github.com/elastic/fleet-server.git   b20d4ac528867e212dddc8ae807fbb84ece4c4fb"
  "reva        https://github.com/cs3org/reva.git             1951af40ed30eb4a4863d42ecfaed25d9fe653fe"
  "topaz       https://github.com/aserto-dev/topaz.git        b998849b31942601e4d7064e61c59a75069499a4"
)

repo_root=$(cd "$(dirname "$0")/.." && pwd)
golden_dir="$repo_root/testdata/corpus"
cache_dir="${ZEROLOGLINTCTX_CORPUS_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/zerologlintctx-corpus}"

update=false
selected=()
for arg in "$@"; do
  case "$arg" in
    -update | --update) update=true ;;
    -h | --help)
      sed -n '3,/^$/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) selected+=("$arg") ;;
  esac
done

mkdir -p "$cache_dir" "$golden_dir"
bin="$cache_dir/zerologlintctx"
echo "building analyzer"
(cd "$repo_root" && go build -o "$bin" ./cmd/zerologlintctx)

# checkout clones $url at $sha into $dir, fetching only that commit.
checkout() {
  local dir="$1" url="$2" sha="$3"
  if [ "$(git -C "$dir" rev-parse HEAD 2>/dev/null || true)" = "$sha" ] &&
    [ -z "$(git -C "$dir" status --porcelain 2>/dev/null)" ]; then
    return
  fi
  rm -rf "$dir"
  git init -q "$dir"
  git -C "$dir" fetch -q --depth 1 "$url" "$sha"
  git -C "$dir" -c advice.detachedHead=false checkout -q FETCH_HEAD
}

# analyze prints the sorted diagnostics for the module in $dir.
analyze() {
  local dir="$1"
  # The analyzer exits non-zero when it reports or when a package fails to
  # load; neither is a failure here.
  (cd "$dir" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    "$bin" -json ./... 2>/dev/null || true) |
    jq -r --arg root "$dir/" '
      .. | objects | select(has("posn") and has("message"))
      | select(.posn | startswith($root))
      | "\(.posn | ltrimstr($root)): \(.message)"' |
    LC_ALL=C sort -u
}

failed=()
for entry in "${corpus[@]}"; do
  read -r name url sha <<<"$entry"
  if [ ${#selected[@]} -gt 0 ] && [[ " ${selected[*]} " != *" $name "* ]]; then
    continue
  fi

  start=$SECONDS
  dir="$cache_dir/$name"
  checkout "$dir" "$url" "$sha"
  golden="$golden_dir/$name.txt"
  actual=$(analyze "$dir")
  count=$(printf '%s' "$actual" | grep -c . || true)

  if $update; then
    printf '%s\n' "$actual" | grep . >"$golden" || : >"$golden"
    echo "[$name] wrote $count diagnostics ($((SECONDS - start))s)"
  elif diff -u --label "golden/$name.txt" --label "actual/$name.txt" \
    "$golden" <(printf '%s\n' "$actual" | grep . || true); then
    echo "[$name] OK, $count diagnostics ($((SECONDS - start))s)"
  else
    echo "[$name] DIFF ($((SECONDS - start))s)"
    failed+=("$name")
  fi
done

if [ ${#failed[@]} -gt 0 ]; then
  echo "corpus diff in: ${failed[*]}"
  echo "If the change is intended, run scripts/corpus.sh -update and commit the golden files."
  exit 1
fi

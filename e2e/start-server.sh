#!/usr/bin/env bash
# Starts a throwaway Viceroy instance with an empty data dir for the smoke suite.
set -euo pipefail
bin="$(cd "$(dirname "$0")/.." && pwd)/bin/viceroy"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
cd "$dir"
"$bin" init >/dev/null
sed -i "s/127.0.0.1:8420/127.0.0.1:${PORT:-18421}/" viceroy.toml
"$bin" serve

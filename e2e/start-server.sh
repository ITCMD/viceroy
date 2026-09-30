#!/usr/bin/env bash
# Starts a throwaway Viceroy instance with an empty data dir for the smoke suite,
# plus a fake SimpleFIN Bridge on FAKE_SF_PORT (default 18430) and a fake IMAP server on
# 18431 (control API on 18432) seeded with internal/email/testdata.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
dir=$(mktemp -d)
"$root/bin/fakesimplefin" -listen "127.0.0.1:${FAKE_SF_PORT:-18430}" &
fake=$!
"$root/bin/fakeimap" -listen 127.0.0.1:18431 -control 127.0.0.1:18432 -seed "$root/internal/email/testdata" &
fakeimap=$!
trap 'kill $fake $fakeimap 2>/dev/null; rm -rf "$dir"' EXIT
cd "$dir"
"$root/bin/viceroy" init >/dev/null
sed -i "s/127.0.0.1:8420/127.0.0.1:${PORT:-18421}/" viceroy.toml
"$root/bin/viceroy" serve

#!/usr/bin/env bash
# Starts a throwaway Viceroy instance with an empty data dir for the smoke suite,
# plus a fake SimpleFIN Bridge on FAKE_SF_PORT (default 28430) and a fake IMAP server on
# 28431 (control API on 28432) seeded with internal/email/testdata, and a fake OpenRouter on
# 28433 for the chat panel (it also serves product page fixtures under /shop/ for the wishlist). These ports stay clear of a dev instance's fakes (1843x), and the
# script refuses to start if one is taken so the suite never talks to someone else's fake.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
for p in "${FAKE_SF_PORT:-28430}" 28431 28432 28433 "${PORT:-18421}"; do
  if (exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null; then
    echo "e2e: port $p is already in use; stop whatever is listening there" >&2
    exit 1
  fi
done
dir=$(mktemp -d)
"$root/bin/fakesimplefin" -listen "127.0.0.1:${FAKE_SF_PORT:-28430}" &
fake=$!
"$root/bin/fakeimap" -listen 127.0.0.1:28431 -control 127.0.0.1:28432 -seed "$root/internal/email/testdata" &
fakeimap=$!
"$root/bin/fakeopenrouter" -listen 127.0.0.1:28433 -shop "$root/internal/wishlist/testdata" &
fakeai=$!
trap 'kill $fake $fakeimap $fakeai 2>/dev/null; rm -rf "$dir"' EXIT
cd "$dir"
"$root/bin/viceroy" init >/dev/null
sed -i "s/127.0.0.1:8420/127.0.0.1:${PORT:-18421}/" viceroy.toml
sed -i 's#^openrouter_key = ""#openrouter_key = "test-key"#; s#^base_url = ""#base_url = "http://127.0.0.1:28433"#' viceroy.toml
# The wishlist's link reader normally refuses private addresses; the fake store is local.
# Close-out windows follow this date (the 3rd: last month can be closed out) so that suite
# doesn't depend on the day it runs.
VICEROY_CLOSEOUT_TODAY="$(date +%Y-%m-03)" VICEROY_ALLOW_PRIVATE_FETCH=1 "$root/bin/viceroy" serve

# Viceroy

Self-hosted budgeting app (Monarch-style). Single binary + one config file.

## Install

```sh
make build                 # needs Go 1.24+ and Node 20+; produces bin/viceroy
./bin/viceroy init         # writes viceroy.toml
$EDITOR viceroy.toml       # port, allowed subnets, reverse-proxy IPs, etc.
./bin/viceroy serve
```

Open the printed URL; the first visit creates the admin account.

To reach it from other devices, set `listen = "0.0.0.0:8420"` and add your LAN to
`allowed_cidrs`. For PWA install and push notifications, put it behind an HTTPS
reverse proxy, list the proxy's IP in `trusted_proxies` and set `public_url`. Then turn
on push per device in Settings → Notifications (alerts also appear under the bell).

Running it for real (systemd unit, Docker image, HTTPS proxies, backups and restore):
see [docs/DEPLOY.md](docs/DEPLOY.md). In short:

```sh
docker compose -f deploy/compose.yaml up -d --build   # or deploy/viceroy.service for systemd
viceroy backup                                         # also automatic, daily, 14 kept
viceroy restore data/backups/viceroy-<time>.tar.gz     # with the server stopped
viceroy reset-password you@example.com                 # locked out? prints a reset link
```

"Chat with your budget" on the dashboard needs an OpenRouter key: set
`[ai] openrouter_key` (and optionally `chat_model`) and restart.

## Development

```sh
make test                  # go vet, go test, frontend typecheck
make run                   # backend on :8420
make dev-web               # Vite dev server with hot reload, proxies /api to :8420
make sqlc                  # regenerate DB code after editing internal/db/{migrations,queries}
cd e2e && npm ci && npx playwright test   # browser smoke suite against a fresh instance
```

The e2e suite needs Chromium's system libraries once per machine:
`cd e2e && sudo npx playwright install-deps chromium`.

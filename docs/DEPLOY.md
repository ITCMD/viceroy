# Deploying Viceroy

Viceroy is one binary plus one config file (`viceroy.toml`). Everything it stores lives in
`data_dir`: the SQLite database, two generated keys, and daily backups. Pick one way to run it:

- [Plain binary](#plain-binary): try it out, or run it under your own process manager.
- [systemd](#systemd): a Linux server, sandboxed, starts on boot.
- [Docker](#docker): a container with a named volume.

Then put it [behind HTTPS](#https-and-reverse-proxies) if phones will use it (PWA install and
push notifications need HTTPS), and check [backups](#backups).

## Plain binary

```sh
make build                     # Go 1.27+ and Node 20+; produces bin/viceroy
./bin/viceroy init             # writes ./viceroy.toml
$EDITOR viceroy.toml           # port, allowed subnets, ...
./bin/viceroy serve
```

Open the printed URL. The first visit creates the admin account and household.

The settings you're most likely to change:

| Setting | Default | Notes |
| --- | --- | --- |
| `listen` | `127.0.0.1:8420` | `0.0.0.0:8420` to accept connections from other machines. |
| `allowed_cidrs` | `127.0.0.1`, `::1` | Client IPs allowed to connect, e.g. `["127.0.0.1", "192.168.0.0/24"]`. `"0.0.0.0"` allows everyone. Everyone else gets 403. |
| `trusted_proxies` | none | Your reverse proxy's IP, so the real client IP (from `X-Forwarded-For`) is checked against `allowed_cidrs`. |
| `public_url` | none | The HTTPS URL people use. Turns on secure cookies, HSTS and push. |
| `data_dir` | `./data` | Relative to the config file. |
| `[backup] keep` | `14` | Daily backups to keep; `0` turns them off. |
| `[backup] dir` | `<data_dir>/backups` | Ideally another disk. |

Command reference: `viceroy -h`. All commands take `-config path` (default `./viceroy.toml`)
before the command name.

### Environment overrides

These variables override the matching config keys. The Docker image and the systemd unit
use them for settings that belong to how Viceroy is installed. The server logs which
ones are set when it starts.

`VICEROY_LISTEN`, `VICEROY_ALLOWED_CIDRS` and `VICEROY_TRUSTED_PROXIES` (comma-separated),
`VICEROY_PUBLIC_URL`, `VICEROY_DATA_DIR`, `VICEROY_BACKUP_KEEP`.

Dates (what counts as "today" for budgets and email alerts) follow the server's time zone;
set `TZ`, e.g. `TZ=America/New_York`, if the server's zone isn't yours.

## systemd

Quick way: `make build && sudo deploy/install.sh --lan 192.168.0.0/24` does all of the
steps below (the `--lan` part only on first install; re-run it after `make build` to
upgrade). Manually:

`deploy/viceroy.service` runs Viceroy as a `viceroy` system user with a tight sandbox. The
config goes in `/etc/viceroy/viceroy.toml`; data (database, keys, backups) goes in
`/var/lib/viceroy`.

```sh
make build
sudo useradd --system --home-dir /var/lib/viceroy --shell /usr/sbin/nologin viceroy
sudo install -m 755 bin/viceroy /usr/local/bin/viceroy
sudo install -d -m 750 -g viceroy /etc/viceroy
sudo viceroy -config /etc/viceroy/viceroy.toml init
sudo chgrp viceroy /etc/viceroy/viceroy.toml && sudo chmod 640 /etc/viceroy/viceroy.toml
sudoedit /etc/viceroy/viceroy.toml                # listen, allowed_cidrs, ...
sudo install -m 644 deploy/viceroy.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now viceroy
journalctl -u viceroy -f                          # logs
```

The unit sets `VICEROY_DATA_DIR=/var/lib/viceroy`, so `data_dir` in the file is ignored.
If you point `[backup] dir` somewhere outside `/var/lib/viceroy`, uncomment the
`ReadWritePaths=` line in the unit (the sandbox makes the rest of the filesystem read-only).
To listen on a port below 1024 (built-in TLS on 443), uncomment the two capability lines.

Commands against a systemd install run as the service user:

```sh
sudo -u viceroy VICEROY_DATA_DIR=/var/lib/viceroy viceroy -config /etc/viceroy/viceroy.toml backup
```

**Upgrading:** `make build`, `sudo install -m 755 bin/viceroy /usr/local/bin/viceroy`,
`sudo systemctl restart viceroy`. Database migrations run on start. The daily backup
means you always have a copy from before the upgrade; for a fresh one, run `backup` first.

## Docker

```sh
docker compose -f deploy/compose.yaml up -d --build
```

or without compose:

```sh
docker build -t viceroy .
docker run -d --name viceroy --restart unless-stopped -p 8420:8420 \
  -e VICEROY_ALLOWED_CIDRS="127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16" \
  -e TZ=America/New_York \
  -v viceroy-data:/data viceroy
```

The image is a static binary on distroless (about 35 MB), running as a non-root user.
Everything lives in the `/data` volume: `viceroy.toml` (written on first start), the
database, keys and `backups/`.

- `VICEROY_LISTEN` and `VICEROY_DATA_DIR` are set by the image; leave them.
- `VICEROY_ALLOWED_CIDRS` in `compose.yaml` allows private networks (your LAN, plus
  Docker's bridge, which is where requests from the host itself appear to come from).
  It overrides `allowed_cidrs` in the file; narrow it to your LAN if you like.
- Other settings are in `/data/viceroy.toml`. The image has no shell or editor, so edit it
  through a throwaway container and restart:

  ```sh
  docker run --rm -it -v viceroy-data:/data alpine vi /data/viceroy.toml
  docker restart viceroy
  ```

Backup now, restore, version:

```sh
docker exec viceroy viceroy -config /data/viceroy.toml backup
docker stop viceroy
docker run --rm -v viceroy-data:/data viceroy restore /data/backups/viceroy-20261002-030000.tar.gz
docker start viceroy
```

(With compose the volume is named `deploy_viceroy-data`; see `docker volume ls`.)

**Upgrading:** pull the new code, `docker compose -f deploy/compose.yaml up -d --build`.

## HTTPS and reverse proxies

Phones need HTTPS to install Viceroy as an app and to get push notifications. Either use
the built-in TLS (`[tls] cert`/`key`), or, more simply, put a reverse proxy in front.
Either way:

1. Set `public_url` to the HTTPS address, e.g. `https://viceroy.example.com`.
2. Add the proxy's IP to `trusted_proxies` (`127.0.0.1` if it runs on the same machine;
   for Docker, the address it reaches the container from).
3. Keep `allowed_cidrs` to the clients you want (LAN, Tailscale's `100.64.0.0/10`, ...).

Once `public_url` is https, sign-in cookies are marked Secure, so plain-http URLs (like
`http://192.168.0.10:8420`) can no longer sign in. Use the HTTPS address everywhere.

**Caddy** (gets certificates automatically):

```
viceroy.example.com {
    reverse_proxy 127.0.0.1:8420
}
```

**nginx:**

```nginx
server {
    listen 443 ssl;
    server_name viceroy.example.com;
    # ssl_certificate ...; ssl_certificate_key ...;
    client_max_body_size 25m;            # Monarch CSV and screenshot imports
    location / {
        proxy_pass http://127.0.0.1:8420;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 10m;          # chat answers stream for a while
    }
}
```

Chat answers are streamed (server-sent events); Viceroy sends `X-Accel-Buffering: no` so
nginx doesn't hold them back.

**Tailscale:** `tailscale serve --bg 8420` gives you an HTTPS `*.ts.net` address on your
tailnet. Set that as `public_url` and `trusted_proxies = ["127.0.0.1"]`.

## Backups

What to protect is `data_dir`: `viceroy.db` plus `secret.key` (it decrypts the stored
SimpleFIN tokens and mailbox passwords; a database without it means re-linking every bank
and mailbox) and `vapid.json` (push subscriptions).

- **Automatic:** the server writes a backup whenever the newest one is a day old (checked
  at start and hourly) and keeps the last `[backup] keep` (14). Settings → General →
  Backups shows them and has "Back up now" (admins only).
- **On demand:** `viceroy backup` writes one into the backup folder; `viceroy backup -o
  file.tar.gz` writes it anywhere. Safe while the server runs (it uses SQLite's
  `VACUUM INTO`, and checks the copy's integrity).
- **Off the machine:** backups are plain `.tar.gz` files (mode 600). Copy the backup
  folder somewhere else (rsync, restic, a NAS share); treat them as secrets.

Each archive holds `viceroy.db`, `secret.key`, `vapid.json` and a copy of `viceroy.toml`
for reference.

**Restore:** stop the server, then

```sh
viceroy restore /path/to/viceroy-20261002-030000.tar.gz
viceroy serve
```

It checks the archive's database first, then swaps it and the keys into `data_dir`. The
files it replaced stay next to them with a `.before-restore-<time>` suffix; delete them
once you're happy. It refuses to run while something is listening on `listen`. It does not
touch your `viceroy.toml`; the archive's copy is there if you need it
(`tar xzf backup.tar.gz viceroy.toml`).

## Troubleshooting

- **403 Forbidden on every page:** your IP isn't in `allowed_cidrs`. Behind a proxy, add
  the proxy to `trusted_proxies`; in Docker, check `VICEROY_ALLOWED_CIDRS`.
- **Can't sign in after setting `public_url`:** you're on plain http; use the https URL.
- **No push notifications:** they need `public_url` with https, and turning push on per
  device in Settings → Notifications. iPhones also need the app added to the home screen.
- **Forgot a password:** an admin can make a reset link in Settings → Household. If nobody
  can sign in, run `viceroy reset-password you@example.com` on the server (same `-config`
  as the server; in Docker, `docker exec viceroy viceroy -config /data/viceroy.toml
  reset-password ...`). It prints a one-time link valid for 7 days.

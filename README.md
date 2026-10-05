# Viceroy
Self-hosted budgeting app (Monarch-style). Single binary + one config file.

**Viceroy solves the biggest issue I had with all the other paid options:** delayed sync. I especially had this issue with Capital One since they don't expose pending transactions. Viceroy, alternatively, lets you setup an email inbox (create one anywhere that lets you have imap login), give it to viceroy, and it will immediately process them (purchases, withdrawl, etc). This means it takes minutes to sync instead of hours to days.

### Key Features
- All in one dashboard where you can see your basic finances and ask AI if a purchase is in your budget
- Full budget planning allowing you to budget as little or as much as you want with dedicated Fixed and Flexible categories
- Recurring charge management to see upcoming charges until your next paycheck
- Rule creation to manage transactions automatically
- A suite of reporting tools like a Sankey cash flow view, debt free future planning, etc
- Goals to budget for major purchases or savings
- A wishlist, which is a custom goal that lets you save items you want and it will prioritize it for you based on how many people in your household want it x how much you want it x 1.5 if it saves you money divided by the cost
- Everything is kept on machine. AI prompts can be given to openrouter or any local LLM.
- Progressive Web App means you can "install" it on your phone and get notifications
- AI chatting and planning in lots of areas.

I initially came up with the name Viceroy since it's also a kind of royal name like Monarch. Then I found out there's a Viceroy butterfly that looks like a Monarch one. That was just too good not to do.

## Requirements
- A Email account with IMAP access (dedicated is preferred but really doesn't need to be) - this is what you'll point bank notifications to for Viceroy to parse. I have my own domain and do email through Dynu, which gives me a bunch of email accounts on my domain for pretty cheap. Gave Viceroy budgeting-randomstring@mydomain.com
- A VPS or local machine to run it on (doesn't need much, maybe 2GB of ram and 1CPU)
- Ideally a reverse proxy for https (required for the app and notifications)
- [OpenRouter API key](https://openrouter.ai/) - I use the deepseek pro and flash models and usually pay about $0.05/month in usage currently. Local LLMs work too.
- A bit of patience as you first train it on emails.

All of this costs significantly less than a subscription to Monarch, for example, at the time of writing.

Overly honest note, Yes, these screenshots show my real finances, which hopefully shows why I wanted to make this. For reference, I live in New England about as cheaply as you can and just finally got to a job where we're able to save consistently. I say that because in many places in the world, my expenses might seem super high or super cheap. It's all relative to location. Also one of my accounts was missing for a bit since SimpleFin had to update the Cap1 connection (an infrequent issue) so the finances are a bit off.

## Screenshots
<img width="1833" height="946" alt="image" src="https://github.com/user-attachments/assets/90560c39-dd79-44df-9f66-fba8e51b7f2e" />
<img width="1845" height="940" alt="image" src="https://github.com/user-attachments/assets/a8d3c9d4-711e-47d1-84bc-49463420b79a" />
<img width="1253" height="861" alt="image" src="https://github.com/user-attachments/assets/33d42d8f-a24b-4452-b9dd-c3bdc0d86d1e" />
<img width="1146" height="783" alt="image" src="https://github.com/user-attachments/assets/0fe72b74-b341-4110-88ba-b923b45ad019" />
<img width="1838" height="933" alt="image" src="https://github.com/user-attachments/assets/6f39d3a9-e6fc-4709-b182-f844e24bf81d" />

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

Worst case, ask AI, this is a pretty simple thing a free model should be able to do.

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

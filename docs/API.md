# Viceroy REST API

The REST API is the same JSON API the web app uses, under `/api`. It is off by default.

## Turning it on
1. Sign in as an admin and open **Settings → API**.
2. Switch on **Enable the REST API**.
3. **Generate key**: name it after what will use it, pick **Read only** or **Read & write**. The key (`vk_…`) is shown once; only a hash is stored.

Turning the switch off rejects every key; revoking a key stops just that one.

## Using a key
```sh
curl -H "Authorization: Bearer vk_..." https://viceroy.example.com/api/accounts

curl -X PATCH -H "Authorization: Bearer vk_..." -H "Content-Type: application/json" \
  -d '{"category_id": 12}' https://viceroy.example.com/api/transactions/345
```

- A key acts as the admin who created it. Read-only keys may only use `GET`.
- Key requests don't need the app's `X-Viceroy-CSRF` header.
- Money in responses is integer cents (negative = money out). Request amounts are dollar strings (`"12.34"`). Dates are `YYYY-MM-DD`.
- Errors return a 4xx/5xx status and `{"error": "message"}`.
- `allowed_cidrs` in `viceroy.toml` still applies.
- Managing keys and the API switch is only possible from the app, never with a key.

## Reference
- In the app: **Settings → API → API documentation** (`/settings/api-docs`), every endpoint grouped by area.
- `GET /api/docs`: the same list as JSON.
- `GET /api/openapi.json`: OpenAPI 3 for tools and client generators.

The list lives in `internal/server/apidocs.go`; `TestAPIDocsCoverRoutes` fails if a route is added without documenting it.

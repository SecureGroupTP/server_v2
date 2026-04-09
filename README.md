# server_v2

Minimal production-oriented Go server scaffold.

## Quick start

1. Configure infrastructure variables in `.env`.
2. Create local app config from template:

```bash
cp config/example.config.yaml config/config.yaml
```

3. Start stack:

```bash
docker compose up --build
```

TLS files:
- Put `server.crt` and `server.key` in `config/certs/` to enable `tcp+tls`, `https`, and `wss` listeners.
- If cert files are missing, server will keep running with non-TLS listeners only.

## Services

- `app` in container listens on `8080` (published as `${APP_PORT}`)
- `postgres` on `${POSTGRES_PORT}`
- `redis` on `${REDIS_PORT}`

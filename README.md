# forge-server

Go HTTP service scaffold using Echo, PostgreSQL, and Redis.

## Run

```bash
go run ./cmd/forge-server
```

Default endpoints:

- `GET /healthz`: process liveness.
- `GET /readyz`: PostgreSQL and Redis readiness.
- `GET /api/v1/ping`: API smoke check.

Configuration is read from environment variables. See `.env.example`.

## Layout

Directory rules are documented in `AGENTS.md`. Keep new code inside the smallest layer that owns the behavior.

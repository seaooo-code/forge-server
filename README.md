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
- `GET /api/v1/environments`: environment list.
- `GET /api/v1/environments/:code`: environment detail.

Configuration is read from environment variables. On startup the server loads `.env` from the
repository root when it exists. `.env.example` is only a template and is never loaded by default.
Set `ENV_FILE=/path/to/file` to load another dotenv file. Existing process environment variables
take precedence over values in the dotenv file.

## Layout

Directory rules are documented in `AGENTS.md`. Keep new code inside the smallest layer that owns the behavior.

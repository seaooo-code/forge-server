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
- `GET /api/v1/applications/options`: application form options.
- `GET /api/v1/feishu/users/search`: search Feishu users for owner selection.
- `GET /api/v1/auth/feishu/authorize`: redirect to Feishu OAuth authorization page.
- `GET /api/v1/auth/feishu/callback`: exchange Feishu OAuth code, fetch user info, and upsert `feishu_users`.
- `GET /api/v1/auth/session`: return current Feishu auth session from the server-side session cookie.
- `POST /api/v1/auth/logout`: delete current Feishu auth session.
- `GET /openapi.json`: OpenAPI 3.0 document for Apifox import.

Import `http://localhost:8080/openapi.json` in Apifox after starting the server locally.

PostgreSQL migrations under `migrations/` are applied automatically on startup. To run them manually:

```bash
go run ./cmd/forge-migrate
```

Configuration is read from environment variables. On startup the server loads `.env` from the
repository root when it exists. `.env.example` is only a template and is never loaded by default.
Set `ENV_FILE=/path/to/file` to load another dotenv file. Existing process environment variables
take precedence over values in the dotenv file.

Feishu OAuth requires `FEISHU_APP_ID`, `FEISHU_APP_SECRET`, and `FEISHU_REDIRECT_URI`. The
redirect URI must also be configured in the Feishu developer console. For `forge-console`, use
`http://127.0.0.1:5173/login/feishu/callback` as the local redirect URI and keep
`HTTP_CORS_ALLOW_CREDENTIALS=true`. Keep `FEISHU_OAUTH_SCOPE` empty to use the permissions already
configured for the Feishu app. Set it only when you need to explicitly request a narrower scope.
Feishu user search uses the current logged-in user's `user_access_token` and requires the
corresponding contact permission to be enabled for the app in the Feishu developer console.

OpenTelemetry tracing is disabled by default. To export traces to a local OpenTelemetry Collector,
set:

```bash
OTEL_ENABLED=true
OTEL_SERVICE_NAME=forge-server
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
```

To export traces and structured HTTP exchange logs to ClickStack, point the exporter at the
ClickStack OpenTelemetry Collector. For ClickStack Open Source, set the ingestion key from the
HyperDX app under Team Settings / API Keys:

```bash
OTEL_ENABLED=true
OTEL_SERVICE_NAME=forge-server
OTEL_SERVICE_VERSION=local
OTEL_DEPLOYMENT_ENVIRONMENT=local
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
OTEL_EXPORTER_OTLP_HEADERS=authorization=<your-clickstack-ingestion-key>
```

HTTP requests are traced through Echo, and Feishu outbound HTTP calls are traced when telemetry is
enabled. Every inbound HTTP endpoint also emits a structured `http.exchange` OTLP log containing the
raw request query, request headers, request body, response headers, and response body without
redaction. The log is emitted with the request context so it can be correlated to the HTTP span by
trace ID and span ID.

## Layout

Directory rules are documented in `AGENTS.md`. Keep new code inside the smallest layer that owns the behavior.

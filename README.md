# JournalFlow

Personal AI-assisted devotion journal. Go + PostgreSQL + Docker, backend calls a
Claude-compatible `/v1/messages` endpoint (Anthropic API or self-hosted).

## Quick start

```bash
cp .env.example .env
# edit .env: set SESSION_SECRET, POSTGRES_PASSWORD, ANTHROPIC_API_KEY / ANTHROPIC_BASE_URL

docker compose up --build
```

The app applies its own schema on startup (see `migrations/0001_init.sql`,
embedded into the binary via `internal/db/schema_embed.sql` — keep both in
sync if you add tables).

There's no sign-up page in the nav on purpose (this is a single-user app).
Create your one account once:

```bash
curl -X POST http://localhost:8080/register \
  -d "email=you@example.com&password=yourpassword&name=Your Name"
```

Then log in at `http://localhost:8080/login`. Consider deleting the
`/register` route (in `cmd/server/main.go`) after your account exists.

## Project layout

```
cmd/server/main.go       entrypoint, routing
internal/config          env var loading
internal/db              connection + schema bootstrap
internal/session         cookie session store (DB-backed)
internal/middleware      auth guard
internal/models          data structs
internal/handlers        HTTP handlers (auth, dashboard, journal flow)
internal/ai              Claude API client
migrations/              source-of-truth SQL schema
web/templates            server-rendered HTML (html/template)
web/static               CSS
```

## Status

See `HANDOFF.md` for what's implemented vs. what's left to build.

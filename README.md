# tareas

A tiny self-hosted todo app: single-user, server-rendered (no JS required),
JSON REST API, Postgres-backed. MCP integration is planned as a separate
service that talks to the API.

See [plan.md](plan.md) for the full architecture and decisions.

## Status

The build order in plan.md §14 is complete: migrations, auth, todos
service, web UI, JSON API + token management, i18n, and ops polish
(request body limits, compose healthcheck, GOMEMLIMIT, Postgres tuning,
secrets via .env, documented `pg_dump` backups). Remaining: the first real
deploy (push to main → GHCR → `compose pull && up -d`) and the deferred
MCP service (plan §13).

## REST API (`/api/v1`)

Bearer tokens only — session cookies never authenticate the API. Create a
token on the web UI's *API tokens* settings page, or exchange credentials:

```sh
curl -X POST https://host/api/v1/auth/login \
     -d '{"email": "me@example.com", "password": "..."}'
# → {"token": "..."}

curl -H "Authorization: Bearer $TOKEN" https://host/api/v1/todos
```

| Method & path | Behavior |
|---|---|
| `POST /api/v1/auth/login` | Verifies credentials, returns a fresh token |
| `POST /api/v1/auth/logout` | Revokes the presented token |
| `GET /api/v1/todos` | Flat active list, sorted by due date then title; `?includeCompleted=true` appends completed |
| `POST /api/v1/todos` | Create (`title`, optional `notes`, ISO `due_date`); 201 |
| `PATCH /api/v1/todos/{id}` | Partial: `title`, `notes` (null clears), `due_date` (null clears), `completed` |
| `DELETE /api/v1/todos/{id}` | Hard delete; 204 |

Errors are `{"error": "..."}`: 400 malformed JSON, 401 missing/bad/expired
token, 404 unknown id, 422 validation.

## Development

```sh
mise install                        # installs pinned Go, golangci-lint, sqlc, goose
mise exec -- go test ./...
mise exec -- golangci-lint run
mise exec -- sqlc generate          # after editing internal/todos/queries or internal/tokens/queries
```

Integration tests run against a real Postgres when `TEST_DATABASE_URL` is
set (CI provides a service container). Locally, use a throwaway container
on port 5544 — port 5432 on this host belongs to another project:

```sh
podman run -d --name tareas-test-pg -e POSTGRES_USER=tareas -e POSTGRES_PASSWORD=password -e POSTGRES_DB=tareas -p 5544:5432 docker.io/library/postgres:18-alpine
TEST_DATABASE_URL=postgres://tareas:password@127.0.0.1:5544/tareas mise exec -- go test -race ./...
podman rm -f tareas-test-pg
```

## Deploy

CI pushes `ghcr.io/dfalling/tareas:latest` on every push to `main`.

1. On the host: `cp .env.example .env` and set a real `POSTGRES_PASSWORD`
   (compose refuses to start without it).
2. `docker compose pull && docker compose up -d`. Migrations run on app
   startup under a Postgres advisory lock — no separate migration step.
3. Compose healthchecks the app against `/healthz` via the static
   `healthcheck` binary baked into the image (distroless has no shell, so
   the check cannot be a shell snippet), and the database with `pg_isready`.
   The app runs with `GOMEMLIMIT=64MiB` and Postgres with the low-memory
   settings from plan §11.

### Backups

Decided (plan §11): nightly host-cron `pg_dump` — the dataset is small and a
logical dump restores cleanly across Postgres versions, which volume
snapshots do not guarantee.

```sh
# crontab -e on the host
0 3 * * * cd /srv/tareas && docker compose exec -T db pg_dump -U tareas tareas | gzip > /var/backups/tareas/$(date +\%F).sql.gz
```

Restore with `gunzip -c <dump> | docker compose exec -T db psql -U tareas -d tareas`.

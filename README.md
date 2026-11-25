# tareas

A tiny self-hosted todo app: single-user, server-rendered (no JS required),
JSON REST API, Postgres-backed. MCP integration is planned as a separate
service that talks to the API.

## Status

Built and working: migrations, auth, todos service, web UI, JSON API +
token management, i18n, settings, and ops polish (request body limits,
compose healthcheck, GOMEMLIMIT, Postgres tuning, secrets via .env,
documented `pg_dump` backups). Remaining: the first real deploy (push to
main → GHCR → `compose pull && up -d`) and the deferred MCP service.

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

## Settings

The *Settings* page in the top navigation holds per-account preferences:

- **Auto-delete completed todos** — completed todos are permanently deleted
  N days (1–3650) after completion; leave the field empty to keep them
  forever (the default). A maintenance sweep runs hourly, so deletion can
  lag up to an hour past the deadline. The setting is stored per user
  (`users.auto_delete_completed_days`), so a future multi-user version keeps
  accounts independent.

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

### Self-hosting with Docker

The repo ships a [`compose.yaml`](compose.yaml) running two services: the
app (distroless, nonroot) and PostgreSQL 18 with a named volume. Secrets
live in an uncommitted `.env` next to it — compose interpolates
`POSTGRES_PASSWORD` and refuses to start without it (see
[`.env.example`](.env.example)).

```sh
git clone https://github.com/dfalling/tareas && cd tareas
cp .env.example .env          # then set a real POSTGRES_PASSWORD
docker compose pull
docker compose up -d
```

Migrations run on app startup under a Postgres advisory lock — no separate
migration step. The app listens on `http://your-server:8080`; session
cookies are `Secure`, so put your TLS proxy in front of it.

### First run: create your account

Open `http://your-server:8080/setup` and create your single account (email
and a password of at least 8 characters). You are logged in straight away
and land on the todo list.

There is no registration: the setup page exists only while the users table
is empty and disables itself afterwards (it then redirects to the login
page). Claim yours promptly after the first start — before the app is
reachable by anyone else — because whoever opens `/setup` first on a fresh
database creates the account.

API clients (and later the MCP service) authenticate with bearer tokens
instead: create one on the *API tokens* settings page after logging in.

Compose healthchecks both services: the app via the static `healthcheck`
binary baked into the image probing `/healthz` (distroless has no shell, so
the check cannot be a shell snippet), and the database with `pg_isready`.
The app runs with a `GOMEMLIMIT=64MiB` soft ceiling; Postgres gets
low-memory settings (shared_buffers 64MB, work_mem 4MB,
max_connections 20).

### Backups

Nightly host-cron `pg_dump` — the dataset is small and a
logical dump restores cleanly across Postgres versions, which volume
snapshots do not guarantee.

```sh
# crontab -e on the host
0 3 * * * cd /srv/tareas && docker compose exec -T db pg_dump -U tareas tareas | gzip > /var/backups/tareas/$(date +\%F).sql.gz
```

Restore with `gunzip -c <dump> | docker compose exec -T db psql -U tareas -d tareas`.

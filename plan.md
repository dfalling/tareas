# Tareas — self-hosted todo app (final plan)

Single-user, no-JS-first todo web app with a JSON REST API, tiny memory footprint,
simple Docker deployment, and an MCP integration deferred to a later phase.

All decisions below were made across the planning discussion; nothing is left as
"a or b" — every "or" has been resolved.

---

## 1. Decisions at a glance

| Concern | Decision |
|---|---|
| Language / HTTP | **Go**, stdlib `net/http` (Go 1.22+ method routing), no router framework |
| Templates | stdlib `html/template` + `embed.FS` |
| Database | **PostgreSQL 18** (`postgres:18-alpine`) |
| DB access | `pgx/v5` + `pgxpool`, `MaxConns = 5–10` |
| Queries | **sqlc** (build-time codegen, zero runtime weight); hand-written pgx where it fits better |
| Migrations | **goose**, embedded via `embed.FS`, run on startup under a Postgres advisory lock |
| Sessions (web) | `alexedwards/scs` with pgxstore; scs owns the sessions table |
| API auth | Opaque bearer tokens, sha256-hashed in DB, revocable per device |
| Passwords | Argon2id (~19 MiB params) |
| CSRF | `nosurf` (or small double-submit impl) — web routes only |
| Data layer rule | Cookies authenticate HTML routes **only**; bearer tokens authenticate `/api/v1` **only** |
| Sorting | Alphabetical everywhere: `lower(title), title, id` (no `position` column) |
| Dates | `DATE` columns only, no times; display format **DD-MM-YY** |
| Timezone | "Today" computed in the app from `APP_TIMEZONE` (never `CURRENT_DATE` in SQL) |
| i18n | Ready from day one: `T()` template func + per-locale message tables (`en`, `es`) |
| Version pinning | **mise** (`.mise.toml` for go, golangci-lint, sqlc, goose) |
| Updates | **Renovate** (non-major automerge, 3-day `minimumReleaseAge`, digest-pinned actions) |
| CI | lint (`golangci-lint`) + test (`go test -race`, Postgres 18 service container) |
| CD | Push to `main` → build & push image to **GHCR** (`ghcr.io/dfalling/tareas`) |
| MCP | **Deferred** — separate service later, calls the REST API, never touches Postgres |

---

## 2. Architecture

```text
Existing proxy (TLS)
        |
        v
Go app (one binary)
  - server-rendered HTML at / (tabs, forms, PRG)
  - JSON REST API at /api/v1
  - auth: scs cookie sessions + bearer token check
        |
        v
PostgreSQL 18
```

- One Go binary serves both the HTML app and the API.
- Shared todo logic lives in a `todos` service package; HTML handlers and JSON
  handlers both call it. No duplication.
- Postgres runs in its own container; never co-located with the app.
- Named volume for `pgdata`.

---

## 3. Product scope (v1)

A todo has: **title**, optional **notes**, optional **due date** (a plain date,
no time, settable and clearable), and a completed flag. Nothing else.

- Single user ("just me"). No registration, no email flows.
- Account is bootstrapped by a **one-time `/setup` page**, reachable only while
  the `users` table is empty.
- Deleting is a **hard delete** (no trash/soft-delete concept). Normal flow is
  marking completed; delete exists for permanent removal, guarded by a confirm
  page.
- Schema keeps `user_id` on every table anyway — costs nothing, keeps the door
  open for future multi-user without migrations.

---

## 4. Data model

```sql
users (
  id            bigserial primary key,
  email         text not null unique,        -- stored lowercased
  password_hash text not null,               -- argon2id
  created_at    timestamptz not null default now()
)

api_tokens (
  id           bigserial primary key,
  user_id      bigint not null references users(id),
  token_hash   text not null unique,         -- sha256(token)
  token_prefix text not null,                -- first ~8 chars, for UI identification
  name         text not null,                -- device label
  last_used_at timestamptz,
  expires_at   timestamptz,                  -- nullable = no expiry
  created_at   timestamptz not null default now()
)

todos (
  id           bigserial primary key,
  user_id      bigint not null references users(id),
  title        text not null,
  notes        text,
  due_date     date,                         -- nullable, plain DATE, clearable
  completed    boolean not null default false,
  completed_at timestamptz,                  -- set on toggle, used by Completed tab
  created_at   timestamptz not null default now(),
  updated_at   timestamptz not null default now()
)
```

Indexes:

- `todos (user_id, completed, due_date)` — covers every tab filter
- `api_tokens (expires_at)` — for the cleanup ticker
- `users` unique index is on the lowercased email

Notes:

- **No `sessions` table in our migrations** — `scs` creates and manages its own
  schema via pgxstore.
- **No `position` column** — ordering is alphabetical, decided once, app-wide.
- Timestamps for created/updated/completed; `due_date` stays a bare `DATE`.

---

## 5. Web UI

### Tabs (one section at a time)

The default view is tabbed navigation — no JS required, tabs are just links
that re-render the page with a different filter:

```text
GET /todos                    → Today (default)
GET /todos?view=upcoming
GET /todos?view=unscheduled
GET /todos?view=completed
```

| Tab | Filter | Order |
|---|---|---|
| **Today** | `!completed AND due_date <= today` (today **or overdue**) | alphabetical |
| **Upcoming** | `!completed AND due_date > today` | grouped under one heading per day; alphabetical within each day |
| **Unscheduled** | `!completed AND due_date IS NULL` | alphabetical |
| **Completed** | `completed = true` | `completed_at DESC` |

Details:

- Alphabetical means `ORDER BY lower(title), title, id` — case-insensitive,
  deterministic.
- Upcoming day headings: "Tomorrow", then locale-formatted dates ("Wed 25-11-26").
- **Overdue styling**: overdue items in Today render the date in red,
  e.g. `overdue · 18-11-26`.
- Completed items show a subtle relative "done" marker from `completed_at`.
- If the Completed tab ever grows huge, add a `?limit` later — the obvious lever,
  not needed now.

### Routes

```text
GET  /                       → 303 redirect to /todos
GET  /todos                  → tab view (?view=today|upcoming|unscheduled|completed)
POST /todos                  → create (title [, notes, due_date] from the inline form)
GET  /todos/{id}/edit        → edit form: title, notes, due date (+ clear-due-date button)
POST /todos/{id}/edit
POST /todos/{id}/toggle      → flip completed (button styled as a checkbox)
GET  /todos/{id}/delete      → confirm page ("Delete 'Buy milk'? [Confirm]")
POST /todos/{id}/delete      → hard delete
GET/POST /setup              → one-time bootstrap (only when users table empty)
GET/POST /login
POST /logout
GET/POST /settings/tokens    → minimal API token management (create / revoke)
```

### No-JS patterns

- **Toggle**: a `<form method="post">` wrapping a submit button styled as a
  checkbox — never a bare checkbox input.

  ```html
  <form method="post" action="/todos/42/toggle?view=today">
    <button type="submit" class="checkbox" aria-label="Toggle Buy milk">✓</button>
    <span>Buy milk</span>
  </form>
  ```

- **PRG everywhere**: POST handlers respond `303 See Other`. The redirect
  **carries the current tab through** (forms post to `...?view=upcoming`,
  redirect to `/todos?view=upcoming#todo-42`) so completing an item never
  dumps you on a different tab.
- Editing without JS lives on the per-todo edit page; the list page stays
  create / toggle / delete only.

### Rendering

- Templates and static assets embedded via `embed.FS`.
- One `SELECT` per request (`WHERE user_id = $1`), sliced into tab sections in
  Go — grouping and sorting are pure, easily tested functions. No SQL-side
  grouping gymnastics, no per-tab queries.
- "Today" is computed once per request in the app from `APP_TIMEZONE`.

---

## 6. REST API (`/api/v1`)

```text
POST   /api/v1/auth/login          → returns opaque token
POST   /api/v1/auth/logout         → revokes the presented token
GET    /api/v1/todos               → non-completed only; ?includeCompleted=true appends completed
POST   /api/v1/todos
PATCH  /api/v1/todos/{id}          → partial: title, notes, due_date (null clears it), completed
DELETE /api/v1/todos/{id}          → hard delete
```

- Bearer-token auth only. Because `/api/v1` never accepts cookies, CSRF does
  not apply there (the boundary rule in §1 is what makes this true).
- Responses are **flat lists** with a stable server-side sort
  (`due_date ASC NULLS LAST, then title`); clients (mobile, MCP) group/filter
  client-side. Query-param filters stay out of scope until a real consumer
  needs one — `includeCompleted` is the single exception, agreed.
- JSON error format: simple `{"error": "..."}`; 401 for bad/missing tokens,
  403 for foreign resources (moot while single-user, but enforced via
  `user_id` anyway).
- OpenAPI spec: optional, deferred until MCP needs a contract.

---

## 7. Auth model

**Web**

- Session cookie: `HttpOnly`, `Secure`, `SameSite=Lax`, managed by `scs`
  (pgxstore).
- CSRF token in every form (`nosurf`).
- Login rate-limited in memory (single instance): per-IP and per-account,
  small fixed window.

**API**

- `POST /api/v1/auth/login` returns a 256-bit random opaque token; only
  `sha256(token)` is stored, plus a plaintext `token_prefix` (≈8 chars) for
  device identification in the UI.
- Tokens are revocable per device; optional `expires_at`.
- No JWT — opaque tokens are simpler and safer at this scale.

**Passwords & bootstrap**

- Argon2id with ~19 MiB memory parameters.
- `/setup` is served **only while `users` is empty**; it creates the single
  account, then disables itself. No registration routes exist at all.

**Hygiene**

- A background ticker purges expired `api_tokens` rows (scs cleans its own
  sessions).

---

## 8. Dates & i18n

**Dates**

- Storage, API, and sorting all use ISO `DATE`. **DD-MM-YY is presentation
  only.**
- `<input type="date">` submits ISO `YYYY-MM-DD` regardless of display format —
  parse ISO, render DD-MM-YY. Never parse our own display format.
- Date patterns live **per locale** in the message tables, so a different
  display format per language needs no code change.

**i18n readiness (build this way from day one)**

1. Every UI string goes through a `T` template func backed by per-locale
   message tables (Go maps or embedded JSON: `en`, `es`). Zero string literals
   in templates — retrofitting is the expensive part; this is ~30 minutes now.
2. Locale resolution is a per-request function: reads `APP_LOCALE` env default
   now; upgrade path to cookie/user preference later.
3. `golang.org/x/text` for language matching, plural rules (`message.Plural`),
   and locale-aware formatting.
4. Whole-sentence keys with placeholders (`overdue_since: "Overdue since
   {{.Date}}"`), never concatenated fragments.
5. `<html lang="{{.Locale}}">` from the resolved locale.
6. Todo titles/notes are user data — never passed through `T`.

---

## 9. Tooling

### mise

`.mise.toml` pins all tool versions (Renovate keeps them current):

```toml
[tools]
go           = "1.25"    # latest stable at init; example
golangci-lint = "2.x"
sqlc         = "latest-pinned"
goose        = "latest-pinned"
```

`mise install` locally; `jdx/mise-action` reads the same file in CI — one
source of truth.

### sqlc

- Plain SQL in `.sql` files with `-- name: QueryName :one/:many` annotations;
  `sqlc generate` emits typed pgx/v5 code.
- **Update strategy**: explicit named queries instead of COALESCE tricks,
  which can't distinguish "clear due date" from "don't touch it":
  - `UpdateTodoMeta` (title, notes)
  - `SetTodoDueDate` (accepts NULL = cleared)
  - `ToggleTodo` (`UPDATE ... SET completed = NOT completed, completed_at = ...`)
- Hand-written pgx remains the escape hatch for anything awkward; both coexist.
- sqlc does **not** manage migrations — goose still does.

---

## 10. CI / CD

### CI — `.github/workflows/test.yml`

Mirrors the splitcount shape (triggers: PRs to `main` + pushes to `main`):

- **lint job**: checkout → `jdx/mise-action` → `golangci-lint run`
  - `.golangci.yml` enables `gofumpt` (formatting) and `gosec` (auth-heavy app).
- **test job**: checkout → mise-action → `go test ./... -race`
  - Postgres **18** service container (matches prod), health-check gated,
    exposed as `TEST_DATABASE_URL`.
  - Most tests are service-layer/`httptest` with a fake store and skip the DB;
    the container makes real-Postgres integration tests free to add.

### CD — `.github/workflows/deploy.yml`

Copied from `dfalling/splitcount`, essentially unchanged (language-agnostic;
the Go build happens inside Docker, so CD needs no language setup):

- On push to `main`
- `docker/login-action@v3` with `GITHUB_TOKEN` → GHCR
- `docker/metadata-action@v5`: tags `type=sha,prefix=` + `latest`
- `docker/build-push-action@v6` → `ghcr.io/dfalling/tareas`
- Optional upgrade: add `cache-from/to: type=gha` (splitcount doesn't have it;
  saves Go module re-downloads per deploy).

**Server-side note**: if the repo/package is private, the host needs
`docker login ghcr.io` (PAT) to pull, or the package is set public. Deployment
is `docker compose pull && up -d`; migrations run on app startup (advisory
lock), so CD needs no migration step.

### Renovate — `.github/renovate.json`

```jsonc
{
  "$schema": "https://docs.renovatebot.com/renovate-schema.json",
  "extends": [
    "config:recommended",
    ":automergeMinor",                 // patch + minor automerge, major never
    "helpers:pinGitHubActionDigests"   // tag takeover ≠ code execution
  ],
  "minimumReleaseAge": "3 days",       // cooling delay vs zero-days
  "schedule": ["weekly"]
}
```

- Managers covered automatically: gomod, mise, github-actions, dockerfile.
- `minimumReleaseAge` means an update PR doesn't exist until the release has
  been public 3 days — automerge can never fire on a fresh malicious release.
  Tradeoff: legit security patches are delayed equally; 3 days chosen as the
  balance for a personal app (bump to 7 if paranoid).
- Go modules are immutable post-publish (sumdb), so only *new* versions are a
  threat surface — exactly what the age gate covers.
- Optional extra hardening: `"pinDigests": true` to digest-pin Docker base
  images too (tags are mutable).
- Go version lives in both `.mise.toml` and the Dockerfile; Renovate updates
  them as separate PRs — merge both, drift is harmless and CI-visible.

---

## 11. Docker & ops

### Image

```dockerfile
FROM golang:1.25-alpine AS build       # version kept current by Renovate
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app /app
EXPOSE 8080
ENTRYPOINT ["/app"]
```

### Compose (v1 — no MCP service yet)

```text
services:
  app:  ghcr.io/dfalling/tareas:latest (distroless, nonroot)
  db:   postgres:18-alpine, named volume pgdata
```

Postgres tuning (low-memory):

```conf
shared_buffers = 64MB
work_mem = 4MB
max_connections = 20
```

### Runtime configuration (env)

`DATABASE_URL`, `LISTEN_ADDR`, `APP_TIMEZONE`, `APP_LOCALE`, session/token
secrets via env or Docker secrets. 12-factor, no config files.

### Production hygiene

- `/healthz` endpoint wired to the Docker healthcheck.
- Graceful shutdown on SIGTERM; `http.Server` with Read/Write timeouts; request
  body size limits.
- `GOMEMLIMIT=64MiB` as a soft ceiling.
- **Backups**: scheduled `pg_dump` (cron on the host) or documented volume
  snapshots — decided before first real data, not after.
- Memory floor: Go 15–40 MB, Postgres 100–200 MB → ~250–400 MB total. Postgres
  is the elephant; the app is not.

---

## 12. Testing

- **Unit**: service-layer logic — tab slicing/sorting/grouping are pure
  functions over a fetched slice; date-window logic ("today", overdue) tested
  against fixed clocks.
- **Handlers**: `httptest` against handlers backed by a fake store.
- **Integration** (optional, CI-ready): a few end-to-end tests against the CI
  Postgres via `TEST_DATABASE_URL`.

---

## 13. Deferred (not v1)

- **MCP**: separate tiny service (`list_todos`, `create_todo`,
  `complete_todo`, `delete_todo`) calling `/api/v1` with a personal access
  token created on the tokens settings page. Never touches Postgres. Transport
  (local stdio vs server-side `/mcp`) decided when built. The REST API as
  specified here is its contract.
- htmx progressive enhancement (the form-based patterns are already
  compatible).
- Recurrence, subtasks, tags/lists — none planned; migrations can add them.
- `?limit`/pagination on the Completed tab, per-tab API filters.

---

## 14. Build order

1. Repo scaffold: `.mise.toml`, `go.mod`, `.golangci.yml`, CI workflows,
   Renovate config, Dockerfile — pipeline green before features.
2. Migrations + schema (goose, embedded).
3. Auth: `/setup` bootstrap, sessions, login/logout, middleware.
4. Todos service layer + sqlc queries.
5. Web UI: tabs, create/toggle/edit/delete, PRG + view propagation.
6. REST API + tokens settings page.
7. i18n message tables (keys were used from step 5), `en` + `es` stubs.
8. Ops polish: `/healthz`, shutdown, backups, first deploy via GHCR.

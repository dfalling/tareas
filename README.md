# tareas

A tiny self-hosted todo app: single-user, server-rendered (no JS required),
JSON REST API, Postgres-backed. MCP integration is planned as a separate
service that talks to the API.

See [plan.md](plan.md) for the full architecture and decisions.

## Status

Migrations, auth (one-time setup, login, sessions), and the todos service layer
are in place. Next: the tabbed todo UI and the JSON API.

## Development

```sh
mise install                        # installs pinned Go, golangci-lint, sqlc, goose
mise exec -- go test ./...
mise exec -- golangci-lint run
mise exec -- sqlc generate          # after editing internal/todos/queries
```

## Deploy

CI pushes `ghcr.io/dfalling/tareas:latest` on every push to `main`.
Run with `docker compose up -d` on the server.

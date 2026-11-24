# tareas

A tiny self-hosted todo app: single-user, server-rendered (no JS required),
JSON REST API, Postgres-backed. MCP integration is planned as a separate
service that talks to the API.

See [plan.md](plan.md) for the full architecture and decisions.

## Status

Scaffold — tooling, CI/CD, and Docker in place; features not yet implemented.

## Development

```sh
mise install                        # installs pinned Go, golangci-lint, sqlc, goose
mise exec -- go test ./...
mise exec -- golangci-lint run
```

## Deploy

CI pushes `ghcr.io/dfalling/tareas:latest` on every push to `main`.
Run with `docker compose up -d` on the server.

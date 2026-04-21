# CHE1 Worker

Go worker service. Owns the database, external API integrations, and business
logic. Exposes **both REST and gRPC** inbound APIs for clients (Discord bot,
frontend, other services) and pushes real-time events to the frontend via
**WebSocket** and **Redis Pub/Sub**.

## Architecture

```
     ┌──────────────┐   REST   ┌────────────────────────┐
     │ Discord bot  │─────────▶│                        │
     ├──────────────┤   gRPC   │      CHE1 Worker       │
     │  Other svc   │─────────▶│                        │
     └──────────────┘          │  ┌──────────────────┐  │
                               │  │ service (tasks)  │  │
                               │  └───┬──────┬───────┘  │
                               │      │      │          │
                               │  ┌───▼──┐ ┌─▼──────┐   │
                               │  │  DB  │ │external│   │
                               │  │ (pg) │ │  APIs  │   │
                               │  └──────┘ └────────┘   │
                               │      │                 │
                               │  ┌───▼────────┐        │
                               │  │ events     │──▶ Redis Pub/Sub
                               │  │ (ws+redis) │──▶ Frontend (WS)
                               │  └────────────┘        │
                               └────────────────────────┘
```

## Layout

- [cmd/worker/main.go](cmd/worker/main.go) — entry point, wiring, graceful shutdown.
- [internal/config/](internal/config/) — env-based configuration.
- [internal/logging/](internal/logging/) — `log/slog` setup.
- [internal/db/](internal/db/) — Postgres pool (pgx) + repositories + migration.
- [internal/external/](internal/external/) — generic HTTP client for third-party APIs.
- [internal/service/](internal/service/) — business logic shared by REST and gRPC.
- [internal/httpapi/](internal/httpapi/) — REST server (stdlib `net/http`, Go 1.22 routing).
- [internal/grpcapi/](internal/grpcapi/) — gRPC server (health + reflection; register generated services here).
- [internal/ws/](internal/ws/) — WebSocket hub for frontend clients.
- [internal/pubsub/](internal/pubsub/) — Redis Pub/Sub publisher.
- [pkg/models/](pkg/models/) — shared types.
- [proto/worker.proto](proto/worker.proto) — gRPC service definition.

## Endpoints

**REST** (`HTTP_LISTEN_ADDR`, default `:8080`):

- `GET  /healthz`
- `POST /api/v1/tasks` — create task. Accepts two payload shapes:
  - Native: `{"kind": "...", "input": {...}, "created_by": "..."}`
  - Dashboard BFF: `{"event": "...", "guild_id": "...", "payload": {...}}` —
    `event` is stored as `kind`, `{guild_id, payload}` as `input`,
    `created_by` defaults to `"dashboard"`.
- `GET  /api/v1/tasks?status=&limit=` — list tasks
- `GET  /api/v1/tasks/{id}` — get task
- `POST /api/v1/tasks/{id}/complete` — mark task done/failed

All `/api/v1/*` require `Authorization: Bearer $INBOUND_API_KEY`.

CORS is enforced against `DASHBOARD_ALLOWED_ORIGINS`.

**gRPC** (`GRPC_LISTEN_ADDR`, default `:9090`):

- `grpc.health.v1.Health` (built-in)
- Reflection enabled
- Your generated `worker.v1.Tasks` service — register it in
  [internal/grpcapi/server.go](internal/grpcapi/server.go) after `make proto`.

**WebSocket** (`WS_LISTEN_ADDR`, default `:8090`, path `/ws`) — streams
`models.Event` JSON messages to every connected frontend client.

## Configuration

Copy `.env.example` to `.env`. `DATABASE_URL` is required; everything else
has sensible defaults.

## CHE1 Dashboard integration

This Worker is paired with the [CHE1 Dashboard](https://github.com/CHE1-Bot/Dashboard)
(Svelte frontend + Go BFF).

**Traffic shape:**

- Browser (Svelte, `:5173`) → Dashboard BFF (Go, `:8080`) → Worker (this service)
- For action events (tickets, moderation, giveaways), the Dashboard BFF does
  `POST {WORKER_URL}/api/v1/tasks` with
  `{"event", "guild_id", "payload"}` and `Authorization: Bearer {WORKER_API_KEY}`.
  Set the Dashboard's `WORKER_URL` to this Worker's base URL, and its
  `WORKER_API_KEY` equal to this Worker's `INBOUND_API_KEY`.
- The Dashboard frontend can also connect directly to the Worker's WebSocket
  at `ws://…:8090/ws` — origins are checked against `DASHBOARD_ALLOWED_ORIGINS`.

**Port collision in local dev:** both the Dashboard BFF and this Worker
default to `:8080`. When running both on one host, change one of them
(e.g. `HTTP_LISTEN_ADDR=:8081` here, then point the Dashboard at
`WORKER_URL=http://localhost:8081`).

**Outbound calls to the Dashboard** are handled by
[internal/dashboard/client.go](internal/dashboard/client.go). Set
`DASHBOARD_BASE_URL` + `DASHBOARD_API_KEY` to enable; the client is wired
in [cmd/worker/main.go](cmd/worker/main.go) and ready for callers to add
specific calls as the Dashboard's API surface grows.

## Run

```bash
make tidy
make run
```

To generate gRPC stubs:

```bash
make proto
```

Then register the generated service in
[internal/grpcapi/server.go](internal/grpcapi/server.go) where the comment
indicates.

# CHE1 Worker

Go worker service. Owns the database, external API integrations, and business
logic. Exposes **both REST and gRPC** inbound APIs for clients (Discord bot,
frontend, other services) and pushes real-time events to the frontend via
**WebSocket** and **Redis Pub/Sub**.

Pairs with:

- [CHE1-Bot/Dashboard](https://github.com/CHE1-Bot/Dashboard) — Svelte + Go BFF.
- [CHE1-Bot/Bot](https://github.com/CHE1-Bot/Bot) — Discord bot.

All three services share one Postgres database and two bearer-token secrets
(`WORKER_API_KEY`, `DASHBOARD_API_KEY`). See [CHE1 three-repo integration](#che1-three-repo-integration)
below.

## Architecture

```text
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

## CHE1 three-repo integration

This Worker is one of three services that share a Postgres database and two
bearer tokens:

| Secret              | This repo (env)     | Dashboard (env)     | Bot (env)            |
|---------------------|---------------------|---------------------|----------------------|
| `WORKER_API_KEY`    | `INBOUND_API_KEY`   | `WORKER_API_KEY`    | `WORKER_API_KEY`     |
| `DASHBOARD_API_KEY` | `DASHBOARD_API_KEY` | `DASHBOARD_API_KEY` | `DASHBOARD_API_KEY`  |

Generate them with `openssl rand -hex 32` and use the same values in all
three `.env` files.

### Inbound — Dashboard BFF → Worker

- Browser (Svelte, `:5173`) → Dashboard BFF (`:8080`) → Worker.
- For action events (tickets, moderation, giveaways, applications), the
  Dashboard BFF sends `POST {WORKER_URL}/api/v1/tasks` with
  `{"event", "guild_id", "payload"}` and
  `Authorization: Bearer {WORKER_API_KEY}`.
- Set the Dashboard's `WORKER_URL` to this Worker's REST base URL and its
  `WORKER_API_KEY` equal to this Worker's `INBOUND_API_KEY`.

### Inbound — Bot → Worker

- The Bot's [`internal/worker/client.go`](https://github.com/CHE1-Bot/Bot/blob/main/internal/worker/client.go)
  enqueues jobs via the same `POST /api/v1/tasks` and polls `GET /api/v1/tasks/{id}`
  for results.
- The Bot's [`internal/worker/subscriber.go`](https://github.com/CHE1-Bot/Bot/blob/main/internal/worker/subscriber.go)
  subscribes to `ws://worker:8090/ws` and reacts to `task.created`,
  `task.updated`, `task.completed` events. Each event's `payload` is the
  full `Task` (`id`, `kind`, `status`, `input`, `output`, `error`, `created_by`).

### Outbound — Worker → Dashboard

- Set `DASHBOARD_BASE_URL` + `DASHBOARD_API_KEY` to call back into the
  Dashboard's `/api/bot/*` surface. The thin client lives in
  [internal/dashboard/client.go](internal/dashboard/client.go) and is wired
  in [cmd/worker/main.go](cmd/worker/main.go).

### Shared Postgres schema

Whichever service boots first creates the shared tables (`tickets`,
`user_levels`, `mod_logs`, `applications`, `application_forms`, `giveaways`)
plus the Worker-owned `tasks` table. All migrations are idempotent
(`CREATE TABLE IF NOT EXISTS`), so boot order does not matter. The shared
schema mirrors [`CHE1-Bot/Bot/schema.sql`](https://github.com/CHE1-Bot/Bot/blob/main/schema.sql).

### Port collision in local dev

Both the Dashboard BFF and this Worker default to `:8080`. When running both
on one host, change one — e.g. `HTTP_LISTEN_ADDR=:8081` here, then point the
Dashboard at `WORKER_URL=http://localhost:8081`.

## Run locally

```bash
make tidy
make run
```

Health endpoints:

- `GET /healthz` — liveness, always `ok`.
- `GET /readyz` — readiness, pings Postgres; `503` while DB is unavailable.

## Docker

The Docker image is self-contained: it installs `protoc`, generates gRPC
stubs, and builds a distroless nonroot binary.

```bash
docker build -t che1/worker:latest .
docker run --rm \
  -e DATABASE_URL=postgres://che1:che1@host.docker.internal:5432/che1?sslmode=disable \
  -e INBOUND_API_KEY=... \
  -p 8080:8080 -p 8090:8090 -p 9090:9090 \
  che1/worker:latest
```

The [Dashboard's `docker-compose.yml`](https://github.com/CHE1-Bot/Dashboard/blob/main/docker-compose.yml)
references `che1/worker:latest` under the `full` profile, so
`docker compose --profile full up` in the Dashboard repo launches the whole
stack once this image is built (or pulled from GHCR — CI publishes it to
`ghcr.io/<owner>/<repo>:latest` on every push to `main`).

## gRPC

The gRPC server always exposes the standard Health service and reflection.

To also serve the `worker.v1.Tasks` service, generate stubs and build with
the `grpcgen` tag:

```bash
make proto-tools   # one-time: installs protoc-gen-go and protoc-gen-go-grpc
make proto         # generates gen/workerpb/ from proto/worker.proto
make build-grpc    # go build -tags grpcgen
```

The Docker image does this automatically. CI runs `make proto` and tests
with `-tags grpcgen` to keep the Tasks implementation in
[internal/grpcapi/tasks_server.go](internal/grpcapi/tasks_server.go) honest.

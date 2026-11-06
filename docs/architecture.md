# path: docs/architecture.md
# Forge CI Architecture Blueprint

Forge CI is a self-hosted Continuous Integration and log-streaming engine built for on-premise, cloud, or air-gapped environments.

## System Architecture

```mermaid
flowchart TD
    subgraph Clients ["Clients & External Hooks"]
        GH["GitHub Webhook"]
        UI["React Flow Web Dashboard"]
        Dev["CLI / API Client"]
    end

    subgraph Edge ["API Gateway & Routing"]
        ChiRouter["go-chi Router (:8080)"]
        SSEHub["SSE Event Streamer"]
        WHHandler["HMAC-SHA256 Webhook Handler"]
        BillingAPI["Daraja M-Pesa & Billing"]
    end

    subgraph CoreEngine ["Runner Engine"]
        DagEngine["DAG Topological Sort (Kahn's BFS)"]
        Scheduler["Parallel Step Scheduler (errgroup)"]
        DockerExec["Ephemeral Docker Executor"]
        Masker["AES-256-GCM Secret Masker"]
    end

    subgraph StorageInfra ["Infrastructure Layer"]
        PG[("PostgreSQL 16 (State & Backlog Logs)")]
        RedisQueue[("Redis (Pub/Sub & Task Queue)")]
        MinIO[("MinIO S3 (Artifacts & Binaries)")]
    end

    GH -->|POST /webhooks/github| WHHandler
    UI -->|GET /runs/{id}/logs| SSEHub
    UI -->|REST Operations| ChiRouter
    Dev -->|Pipeline Runs & Secrets| ChiRouter

    WHHandler -->|Enqueue Run| RedisQueue
    ChiRouter --> PG
    BillingAPI --> PG

    RedisQueue -->|Pull Jobs| Scheduler
    Scheduler --> DagEngine
    DagEngine --> DockerExec
    DockerExec -->|Demux stdout/stderr| Masker
    Masker -->|Realtime Pub| RedisQueue
    Masker -->|Chunked Batch Write| PG
    DockerExec -->|Upload Glob Matches| MinIO
    SSEHub -->|Subscribe live logs| RedisQueue
    SSEHub -->|Replay if Last-Event-ID| PG
```

## Security & Isolation Model

1. **Ephemeral Sandboxes**: Each pipeline step executes inside an ephemeral Docker container created strictly for that step and forcefully removed upon step exit or timeout.
2. **Network Isolation**: Build containers run with `NetworkMode: "none"` unless explicitly overridden, preventing unintended data exfiltration during untrusted builds.
3. **Read-Only Root Filesystems**: Container root filesystems are mounted read-only (`ReadonlyRootfs: true`), redirecting build operations to a bounded shared volume mounted at `/workspace`.
4. **Secret Protection**: Secrets are stored encrypted with AES-256-GCM under a 32-byte master key. Runtime step logs are filtered dynamically against the active secret table so secret tokens are replaced with `***` before entering Redis or PostgreSQL.

## Log Ingestion & Replay Mechanics

- **Live Streaming**: Log outputs from container stdout and stderr are demultiplexed, timestamped, masked, and published to Redis channel `logs:{runID}:{stepID}`.
- **Historical Reconnection**: When clients connect or reconnect using standard Server-Sent Events headers (`Last-Event-ID: <id>`), PostgreSQL serves rows where `id > lastID` before the client transitions seamlessly to the live Redis stream.


// path: docs/adr/0002-sse-redis-dual-layer-logs.md
# ADR 0002: Server-Sent Events & Redis Dual-Layer Log Streaming

* **Status**: Accepted
* **Deciders**: Chris (Nairobi, Kenya), Forge CI Engineering
* **Date**: 2026-11-06

---

## Context
CI/CD users expect real-time visibility into running pipelines. However, high-throughput build steps (such as verbose compiler outputs or test suites) can generate thousands of lines per second. Polling the database creates severe write amplification and query latency, while pure in-memory streaming fails if a client disconnects or needs historical catchup.

## Decision
We adopted a dual-layer logging architecture combining **Redis Pub/Sub** for real-time fanout with **PostgreSQL** for persistent historical storage, exposed via **Server-Sent Events (SSE)**:

1. **Docker Header Demuxing**:
   The `logs.DemuxReader` parses Docker's 8-byte multiplexed header stream into distinct `stdout` (type 1) and `stderr` (type 2) channels, with automatic fallback for raw line streams.
2. **Real-Time Redis Fanout**:
   Active log lines are published immediately to Redis channels (`logs:{runID}` and `logs:{runID}:{stepID}`). Multiple browser clients receive updates with sub-millisecond latency.
3. **Chunked Database Batching**:
   The `logs.Streamer` buffers entries in memory and flushes batches to PostgreSQL either when `batchSize` reaches 50 lines or `flushInterval` (250ms) expires, minimizing DB transactions.
4. **SSE Reconnection Catchup**:
   The HTTP endpoint `/runs/{runID}/logs?step={stepID}` supports the standard `Last-Event-ID` header. When a browser reconnects, the endpoint replays backlog lines from PostgreSQL starting after `Last-Event-ID`, before seamlessly subscribing to live Redis events.
5. **Dynamic AES Secret Masking**:
   All log lines are scrubbed of plaintext secrets, base64 variants, and URL-encoded forms before being published to Redis or inserted into PostgreSQL.

## Consequences
* **Positive**: Minimal database load through batched inserts; sub-millisecond log delivery to clients.
* **Positive**: Reconnection resilience via `Last-Event-ID` without lost log lines.
* **Positive**: Full offline compatibility via in-memory mock pub/sub (`MockRedisPubSub`).
* **Negative**: Requires Redis infrastructure in production deployments.


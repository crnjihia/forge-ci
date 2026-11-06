// path: docs/security.md
# Forge CI — Security Hardening & Threat Model

This document outlines the security architecture, threat model, attack vectors, and defense-in-depth mitigations implemented across Forge CI.

---

## 1. Executive Summary

Continuous Integration systems execute arbitrary code supplied by repository contributors. If unhardened, a malicious build script can compromise the host machine, pivot through internal corporate networks, exfiltrate credentials, or deplete host computing resources.

Forge CI addresses these risks with a multi-layered defense architecture:
1. **Container Isolation**: Ephemeral containers with read-only root filesystems, dropped capabilities, and strict resource quotas.
2. **Docker Daemon Protection**: Analysis and mitigation of Docker socket risks.
3. **Cryptographic Secret Protection**: Authenticated AES-256-GCM encryption at rest with real-time stream scrubbing.
4. **Timing-Safe Webhook Authentication**: Constant-time HMAC-SHA256 verification on all GitHub events.
5. **Billing Transaction Integrity**: Atomic database row-locking preventing double-spend exploits.

---

## 2. Threat Model: Docker Socket Mounting (`/var/run/docker.sock`)

### The Threat
Mounting `/var/run/docker.sock` inside a CI runner container grants that container direct access to the host's Docker Engine daemon API. Because Docker Engine runs as `root` on standard Linux hosts, any process with read/write access to the socket can:
* Spin up privileged containers mounting the host root filesystem (`-v /:/host`).
* Modify host system files (e.g., `/etc/shadow`, `/etc/sudoers`).
* Access other containers running on the same host and inspect their environment variables and memory.
* Escape the container sandbox entirely.

### Forge CI Mitigations

1. **No Docker Socket in Step Containers**:
   * Pipeline steps **never** receive the Docker socket. The socket is exclusively accessed by the runner daemon, never passed down to user-defined job containers.
2. **Read-Only Root Filesystem (`ReadonlyRootfs: true`)**:
   * User containers execute on an immutable root filesystem. Attackers cannot install rootkits, download binaries into system paths, or alter OS libraries.
3. **No New Privileges (`no-new-privileges:true`)**:
   * Prevents binaries with the `setuid` or `setgid` bit set (such as `sudo` or `su`) from gaining elevated privileges during step execution.
4. **Zero Capabilities & Unprivileged Execution**:
   * All Linux capabilities are dropped (`CapDrop: ["ALL"]`). Privileged mode (`Privileged: false`) is strictly enforced; step definitions attempting to enable privileged mode are rejected by the AST validator.
5. **Isolated Workspace Mounts**:
   * Containers mount only an isolated, ephemeral per-run directory (`/workspace:rw`). The runner's `Janitor` service forcefully deletes these directories and associated containers upon run completion or cancellation.
6. **Network Mode Restriction**:
   * Steps without declared external needs run with `NetworkMode: "none"`, preventing unauthorized network calls, cryptomining, or reverse shells.
7. **Rootless Docker Recommendation**:
   * In production deployments, Forge CI is designed to run against Rootless Docker (or Podman), meaning the Docker daemon itself runs under an unprivileged user namespace (`UID != 0`).

---

## 3. Secrets Threat Model & Dynamic Stream Scrubbing

### Encryption at Rest (AES-256-GCM)
* All secrets stored via the Secrets API or PostgreSQL are encrypted using Galois/Counter Mode (**AES-256-GCM**).
* GCM provides both **confidentiality** and **integrity authentication**. Any tampering with the ciphertext or stored nonce causes decryption to fail immediately.
* Master keys (`FORGE_MASTER_KEY`) must be exactly 32 bytes (256 bits).
* A unique, cryptographically random 12-byte nonce (`crypto/rand`) is generated for every single secret record.

### In-Flight Log Masking
To prevent secrets from leaking into stdout/stderr logs or SSE event streams:
1. **Dynamic Pattern Matching**: The `SecretManager` compiles an active dictionary of all plaintext secrets, their base64 representations, and their URL-encoded variants.
2. **Descending Length Sorting**: Secrets are sorted in descending order of length before replacement to prevent short substrings from breaking larger tokens.
3. **Streaming Demux Masking**: The `logs.Streamer` scrubs each log line before broadcasting to Redis and before batch-inserting into PostgreSQL.
4. **Zero Plaintext Exfiltration in API**: The `GET /secrets/{name}` API endpoint strictly returns a masked string (`********`) and never returns the decrypted plaintext over the network.

---

## 4. Webhook Security: HMAC-SHA256 Verification

GitHub webhook payloads are protected against spoofing and tampering:
* The shared secret (`GITHUB_WEBHOOK_SECRET`) is used to compute an HMAC-SHA256 signature of the raw HTTP request body.
* Verification uses `crypto/subtle.ConstantTimeCompare`:
  ```go
  subtle.ConstantTimeCompare([]byte(expectedSignature), []byte(signatureHeader)) == 1
  ```
  This eliminates timing side-channel attacks where an attacker measures slight differences in string comparison duration to deduce the secret byte-by-byte.

---

## 5. Billing Integrity & Metering Security

Forge CI features usage-based compute metering priced in Kenyan Shillings (KES):
* **Atomic Row-Level Locking**: Deductions acquire a pessimistic row lock (`SELECT balance_kes FROM billing_credits WHERE user_id = $1 FOR UPDATE`) inside an isolated transaction.
* **Negative Balance Prevention**: The transaction verifies that `balance_kes >= charge_kes` before subtracting. If insufficient, the transaction rolls back cleanly with an explicit error.
* **Replay Protection on M-Pesa Callbacks**: Safaricom Daraja STK Push callbacks are verified against unique `CheckoutRequestID` records in the `mpesa_transactions` table. Duplicate callback notifications for already processed transactions are idempotently handled.

---

## 6. Resource Limits & Denial-of-Service Defense

To prevent malicious or runaway pipelines from starving the host:
* **CPU Quotas**: Step definitions support `resources.cpu` translated to Docker `NanoCPUs` (e.g. `1.5` CPUs = `1,500,000,000` nanoseconds).
* **Memory Limits**: Memory strings (e.g. `512m`, `2g`) are strictly parsed and enforced. If a step exceeds its memory limit, the Linux kernel OOM-killer terminates the container, and Forge CI records an exit code with status `failure`.
* **Bounded Concurrency**: The scheduler caps parallel step execution to `MaxConcurrency` using a channel-based semaphore (`chan struct{}`), preventing unbounded goroutine or container creation.


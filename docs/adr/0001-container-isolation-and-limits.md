// path: docs/adr/0001-container-isolation-and-limits.md
# ADR 0001: Container Isolation and Resource Limits

* **Status**: Accepted
* **Deciders**: Chris (Nairobi, Kenya), Forge CI Engineering
* **Date**: 2026-11-06

---

## Context
A self-hosted, air-gappable CI/CD platform must safely execute untrusted user code inside containers without allowing hostile workloads to compromise the host operating system, pivot to neighboring containers, or starve host CPU and RAM resources.

## Decision
We enforce strict container sandboxing for all pipeline steps through the Docker Engine SDK abstraction (`executor.DockerClientAPI`):

1. **Read-Only Root Filesystem**:
   All step containers are created with `ReadonlyRootfs: true`. System binaries and `/` cannot be modified.
2. **Privilege Restriction**:
   `Privileged: false` is enforced globally. Containers are started with `SecurityOpt: ["no-new-privileges:true"]`, preventing setuid privilege escalation.
3. **Workspace Isolation**:
   Only an ephemeral, per-run directory is mounted read-write (`/workspace:rw`). A background `Janitor` cleans up workspace directories and orphaned containers.
4. **Hard Resource Limits**:
   User pipelines specify CPU quotas and memory bounds in `.forge-ci.yml`. These are translated to Docker `NanoCPUs` and `Memory` limits (bytes), preventing runaway processes from triggering host-wide out-of-memory (OOM) panics.
5. **Decoupled Architecture with Mock Support**:
   The executor interacts with a `DockerClientAPI` interface rather than directly invoking concrete Docker types. This enables full air-gapped testing and deterministic builds via `MockDockerClient` when no Docker daemon is running.

## Consequences
* **Positive**: High security boundary; step containers cannot alter host state or escalate to root.
* **Positive**: Resource isolation guarantees consistent build performance across concurrent runs.
* **Negative**: Steps requiring container-in-container builds (Docker-in-Docker) cannot run unprivileged without Kaniko or rootless build tools.


# path: CONTRIBUTING.md
# Contributing to Forge CI

Thank you for contributing to **Forge CI**, a portfolio-grade, containerized CI runner built by and for developers in the Kenyan and global engineering ecosystem.

## Engineering Principles

1. **Deterministic & Isolated Execution**: All pipeline steps execute within ephemeral Docker containers configured with read-only root filesystems, disabled networking by default, and bounded CPU/memory limits.
2. **Security First**: Secrets are never logged or stored in plain text. All logs pass through an AES-256-GCM secret masking pipeline (`***`).
3. **Kenyan Context & Offline First**:
   - Currency is Kenyan Shillings (`KES`).
   - Timezone standard is `Africa/Nairobi`.
   - External services (Safaricom Daraja M-Pesa, Africa's Talking SMS) must implement test mock adapters activated when `USE_MOCK_INTEGRATIONS=true`.
   - Never fabricate real personal identities, real phone numbers, or real M-Pesa transaction codes. Use synthetic data conforming to Kenyan numbering plans (`+2547XXXXXXXX` / `+2541XXXXXXXX`).

## Development Workflow

### Prerequisites
- Go 1.22+
- Node.js 20+ & npm
- Docker Engine & Docker Compose
- PostgreSQL 16 & Redis 7 (or run via `docker-compose up -d postgres redis minio`)

### Running Locally
1. Clone the repository:
   ```bash
   git clone https://github.com/crnji/forge-ci.git
   cd forge-ci
   ```
2. Copy environment variables:
   ```bash
   cp .env.example .env
   ```
3. Run the test suite:
   ```bash
   go test -v -race ./...
   ```

## Commit Standards

We follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:
- `feat(scope): add new feature`
- `fix(scope): resolve bug`
- `test(scope): add test suite`
- `docs(scope): update documentation`
- `chore(scope): repository maintenance`


// path: CONTRIBUTING.md
# Contributing to Forge CI

Thank you for your interest in contributing to **Forge CI**! This project is crafted for resilient, air-gappable, on-premise pipeline execution.

---

## Code of Conduct

We are committed to providing a welcoming, inclusive, and harassment-free environment for all contributors. Please treat everyone with respect and empathy.

---

## Development Setup

### Prerequisites
- **Go**: Version 1.22 or higher
- **Node.js**: Version 18 or higher (LTS recommended)
- **Docker**: Optional for local mock development (`USE_MOCK_INTEGRATIONS=true`), required for full container execution.

### Quick Local Start
1. Fork and clone the repository:
   ```bash
   git clone https://github.com/crnjihia/forge-ci.git
   cd forge-ci
   ```
2. Run the developer startup script:
   - **Windows PowerShell**: `powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1`
   - **Linux / macOS**: `./scripts/dev.sh`
3. Access the web dashboard at `http://localhost:5173` and the API at `http://localhost:8080`.

---

## Coding Guidelines

1. **Path Comments**: The first line of every source file must begin with a single-line path comment (e.g., `// path: cmd/api/main.go` or `# path: scripts/dev.sh`).
2. **Go Standards**:
   - Format with `gofmt` or `goimports`.
   - Run `go vet ./...` before submitting changes.
   - Write comprehensive unit tests alongside implementation files (`_test.go`).
3. **Frontend Standards**:
   - Write strict TypeScript with React 18 functional components and hooks.
   - Style with Tailwind CSS utility classes.
   - Verify zero build or type errors via `npm --prefix web run build`.
4. **Git Commit Messages**: Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:
   - `feat(scope): ...`
   - `fix(scope): ...`
   - `docs(scope): ...`
   - `test(scope): ...`
   - `refactor(scope): ...`
   - `chore(scope): ...`

---

## Submitting Pull Requests

1. Create a feature branch from `main`:
   ```bash
   git checkout -b feat/your-feature-name
   ```
2. Commit your changes with meaningful Conventional Commit messages.
3. Verify all tests pass locally:
   ```bash
   go test -v ./...
   npm --prefix web run build
   ```
4. Push your branch to GitHub and open a Pull Request against `main`.
5. Describe the motivation, architectural impact, and verification steps in the PR template.

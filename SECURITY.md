// path: SECURITY.md
# Security Policy

Forge CI treats security and container sandbox isolation as primary requirements.

---

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.0.x   | :white_check_mark: |
| < 1.0   | :x:                |

---

## Reporting a Vulnerability

If you discover a security vulnerability or sandbox escape in Forge CI, please do **NOT** open a public issue.

Instead, please send an encrypted or private report to:
- **Maintainer**: Chris ([@crnjihia](https://github.com/crnjihia))
- **Email**: security@crnjihia.dev (or via private GitHub Vulnerability Reporting)

Please include:
1. Detailed description of the vulnerability (e.g., Docker socket escape, privilege escalation, secret leakage).
2. Proof of Concept (PoC) pipeline configuration or script.
3. Impact assessment and potential mitigations.

We will acknowledge receipt of your vulnerability report within 48 hours and work with you to test and release a security advisory before public disclosure.

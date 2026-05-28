# Security policy

This is an assignment / template repository. The implementation
follows the security rules codified in [AGENTS.md §1](./AGENTS.md)
and is exercised by:

- The integration test
  `cmd/server.TestRun_DebugListenerServesPprof` — confirms the
  pprof / metrics admin endpoints are NOT reachable from the
  public API listener.
- The unit test
  `handler.TestWriteError_500HidesInternals` — confirms 5xx
  responses never leak DB error strings, constraint names, or
  DSN fragments.
- The unit tests `metrics.TestHTTPMiddleware_LabelsByRoute` /
  `…UnmatchedRoutesUseSentinel` — confirm metric labels can't be
  used to enumerate path parameters (cardinality leak).
- The lint suite — `golangci-lint` runs `gosec` on every push
  via [.github/workflows/ci.yml](.github/workflows/ci.yml).
- **GitHub default code scanning** (CodeQL on Go) is enabled at
  the repository level and runs on push and PR — see Settings →
  Code security → Code scanning.

## Reporting a vulnerability

If you discover a security issue in this repository:

1. **Do not open a public GitHub issue.** Public issues can be
   indexed and exploited before a fix is available.
2. Use **GitHub's private "Report a vulnerability"** workflow
   (Security tab → Report a vulnerability). This routes the
   report directly to the repository owners and is the preferred
   channel.
3. If the private workflow is unavailable, email the repository
   owner listed in [.github/CODEOWNERS](.github/CODEOWNERS).

Please include:

- A description of the issue and its impact.
- Steps to reproduce (a minimal failing test case is ideal).
- Affected commit SHA or version.

## Scope

In-scope:

- The wallet-transfer service code (`cmd/`, `internal/`).
- The schema migrations under `migrations/`.
- The CI workflows under `.github/workflows/`.

Out-of-scope:

- Findings in upstream dependencies (please report directly to
  the respective project — GitHub default CodeQL +
  `govulncheck` already cover the well-known surface).
- Findings in third-party services this repo connects to
  (PostgreSQL, Docker, etc.).

## Disclosure timeline

For accepted reports, we aim to:

- Acknowledge within **2 business days**.
- Provide a fix or mitigation plan within **14 business days**
  for critical issues, **30 days** for non-critical.
- Coordinate public disclosure with the reporter.

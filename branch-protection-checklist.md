# Branch Protection and GitHub Setup Checklist

## Repository setup

- Mark the repository as a template repository.
- Create one private repository per candidate from the template.
- Give the candidate write access to their repository.
- Keep `main` protected.

## Branch protection for `main`

Require:

- pull request before merge
- at least 1 approval
- dismissal of stale approvals after new commits
- required status checks before merge
- branch must be up to date before merge

Recommended required checks:

- `lint-format-test`
- `sonarqube`

## Repository variables for CI

Set these repository variables or replace the workflow commands directly:

- `LINT_CMD`
- `FORMAT_CHECK_CMD`
- `TEST_CMD`

Examples:

### Go

- `LINT_CMD=golangci-lint run ./...`
- `FORMAT_CHECK_CMD=test -z "$(gofmt -l .)"`
- `TEST_CMD=go test ./... -race -cover`

### Node

- `LINT_CMD=npm ci && npm run lint`
- `FORMAT_CHECK_CMD=npm ci && npm run format:check`
- `TEST_CMD=npm ci && npm test -- --coverage`

## SonarQube

Set repository secrets:

- `SONAR_TOKEN`
- `SONAR_HOST_URL`

Optionally configure your quality gate in SonarQube and make the GitHub check required.

## GitHub Copilot code review

This is enabled in GitHub settings, not via a workflow file.

Recommended setup:

1. Enable Copilot code review for the repository or organization.
2. Turn on automatic review for all pull requests in the repository.
3. Keep repository custom instructions enabled so `.github/copilot-instructions.md` is used.

## Optional

- Add `CODEOWNERS` for reviewer routing.
- Enable secret scanning if available on your plan.
- Enable code scanning if you want security checks in addition to SonarQube.

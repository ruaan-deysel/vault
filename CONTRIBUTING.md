# Contributing to Vault

Vault is a Go backup daemon for Unraid with a Svelte 5 web UI, shipped as an Unraid `.plg` plugin.

> **[`AGENTS.md`](AGENTS.md) is the single source of truth** for architecture,
> conventions, safety boundaries, and validation. Path-specific rules live in
> [`.github/instructions/`](.github/instructions/). Files in
> [`.github/agents/`](.github/agents/) are optional GitHub Copilot profiles.

## Prerequisites

- **Go 1.26.5** (CI version) — the binary is pure Go (`CGO_ENABLED=0`).
- **Node 22** (CI version) — for the Svelte 5 web UI in `web/`.
- **pre-commit** — run either `make pre-commit-install` or `./scripts/setup-pre-commit.sh` once.

`./scripts/setup-pre-commit.sh` is the quickest setup path: it installs Python/pip, pre-commit, Go tools, and Node prerequisites, then installs hooks.

If you install tools manually, ensure these are available on `PATH`:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
```

## Local Development

```bash
make deps               # go mod download && go mod tidy
make build-local        # Build for Linux/amd64 → build/vault-linux-amd64 (also builds the web UI)
make test               # go test ./... -v
make test-short         # go test ./... -short
make test-coverage      # go test ./... with coverage.out + coverage.html
make lint               # golangci-lint with .golangci.yml
make lint-web           # npm run lint in web/
make security-check     # gosec + govulncheck + go mod verify
make clean              # remove build artifacts and coverage output
```

Run a single test:

```bash
go test ./internal/db/... -run TestJobCreate -v
```

Web UI:

```bash
cd web && npm run build    # Build the UI
cd web && npm run dev      # Vite dev server (proxies /api -> http://localhost:24085)
cd web && npm run lint     # Lint the UI (also: make lint-web)
cd web && npm test         # Run Vitest
```

Running the daemon locally:

```bash
./build/vault daemon --db=vault.db --addr=:24085
```

Frontend development loop:

- Run `npm run dev` in `web/` while the daemon is running on `:24085`; Vite proxies `/api` to `http://localhost:24085` (see `web/vite.config.js`).
- Build `web/dist` with `npm run build` before `go build`/`make build-local`, because `web/embed.go` embeds files from `dist/*`.

Docker build:

- `make docker-build` builds the image with `VERSION`, `COMMIT`, and `BUILD_DATE` passed as Docker build arguments from the `Makefile`.

For project layout and deeper design details, see [Architecture](docs/architecture.md).

## Post-Change Workflow

For ordinary contributions, run the local checks relevant to the change:

1. Targeted tests while iterating.
2. `make test` and `make lint` for Go changes.
3. `npm test`, `npm run lint`, and `npm run build` in `web/` for UI changes.
4. `make pre-commit-run` before committing.

The maintainer runs the release-facing `make build` → approved `make deploy` →
`make verify` → UI verification gate. `make deploy`, `make verify`, and
`make redeploy` target a configured Unraid host and must not be run without
explicit approval. `make redeploy` includes uninstall.

## Changelog

User-visible and release-facing changes add an entry under `## [Unreleased]`
in `CHANGELOG.md` using [Keep a Changelog](https://keepachangelog.com/)
sections: `### Added`, `### Changed`, `### Fixed`, `### Removed`, `### Security`
(any other `###` heading is silently dropped).

- Explain **what** changed **and why** — entries stand alone with no PR context.
- Reference issue numbers where applicable (e.g. `closes #123`).
- Bullets start with a `-` and a space at column 0. Inline markdown that renders: `**bold**`, `` `code` ``, `*italic*` — nothing else.

`CHANGELOG.md` is consumed by three systems — the in-app About/View Changelog modal (parser at `internal/release/changelog.go`), the `release.yml` GitHub-release notes extractor, and operator-facing upgrade diffs — so a malformed entry breaks all three.

## Commits & Pull Requests

- **Conventional Commits** with a scope: `feat(scope):`, `fix(scope):`, `refactor(scope):`, `docs:`, `chore:`, `deps:`. Examples from history: `fix(api):`, `feat(vm):`, `refactor(web):`, `chore(deps):`.
- Branch per change off `main`; open a PR against `main`.
- PRs get automated **CodeRabbit** and **Copilot** review — address their feedback before merge.
- Run `make pre-commit-run` before committing.

## Code Search

Both `rg` (ripgrep) and `ast-grep` are available. Default to `rg`; reach for `ast-grep` only when you need AST-aware matching. See the Code Search section in `AGENTS.md` for the gotchas (notably: `ast-grep` does not support `.svelte`).

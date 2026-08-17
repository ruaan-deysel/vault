# Development

This page is a concise local development quick-start. For full contributor and PR workflow details, see [CONTRIBUTING.md](https://github.com/ruaan-deysel/vault/blob/main/CONTRIBUTING.md). For architecture and deeper build details, see [Architecture](architecture.md).

## Prerequisites

- Go **1.26.5**
- Node **22**

## Local workflow

```bash
make deps
make build-local
./build/vault daemon --db=vault.db --addr=:24085
```

In a second terminal, start the frontend dev server:

```bash
cd web
npm run dev
```

Validation checks:

```bash
make test
make lint
make security-check
```

Docker build:

```bash
make docker-build
```

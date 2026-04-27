# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go build .                        # build binary
go run . --help                   # check flag behavior
go test -v ./...                  # run tests
go test -race ./...               # run tests with race detector
golangci-lint run                 # lint
go fmt ./...                      # format
ko build --base-import-paths --local .  # build container image
```

## Architecture

Single-binary Prometheus service discovery tool for GCP Memorystore (Memcached). All logic lives in `main.go`.

**Core flow:**
1. `MemorystoreSDConfig` holds config (project, location, filter, refresh interval)
2. `MemorystoreDiscovery` embeds `refresh.Discovery` (Prometheus SDK) for periodic polling
3. Each refresh calls GCP `ListInstances()`, iterates instances and their nodes, attaches `__meta_memorystore_memcached_*` labels
4. Results written to `memorystore.json` (file_sd_configs format) and served via HTTP at `:8888/memorystore.json`
5. Prometheus metrics at `/metrics`, pprof at `/debug/pprof/`

**Key design:** No host address is set on targets — callers use Prometheus relabeling rules to construct real targets. Each Memcached node becomes a separate target.

**Integration tests** (`integration_test.go`) spin up a fake gRPC Memorystore server for end-to-end coverage.

## Conventions

- Conventional Commits: `fix:`, `feat:`, `chore(deps):` etc., subject ≤72 chars
- Prometheus label keys as package-level constants
- Use existing `slog` loggers, not bare `log`
- `memorystore.json` is runtime-generated, not committed

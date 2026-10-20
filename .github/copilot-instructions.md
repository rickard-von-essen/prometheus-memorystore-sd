# Repository instructions

## Build, test, and lint

Run commands from the module root. Use the Go version/toolchain declared in
`go.mod`; CI also selects Go from that file.

```sh
go build .                              # Produces ./prometheus-memorystore-sd
go run . --help                         # Inspect CLI flags without starting discovery
go test -v ./...                        # Full suite
go test -v -count=1 -run '^TestMemorystoreIntegrationProducesServiceDiscoveryFile$' .
go test -v -count=1 -run '^TestMemorystoreIntegrationServesHTTPOutput$' .
go test -race ./...                     # Before shipping user-facing changes
go fmt ./...                           # Before committing Go changes
golangci-lint run                       # Uses .golangci.yml (v2 configuration)
golangci-lint fmt                       # Applies configured gci and gofumpt formatters
ko build --base-import-paths --local .  # Local container build
```

CI tests on Linux amd64 and arm64 with `CGO_ENABLED=0`. Race detection requires
CGO enabled, so do not inherit that CI setting for the race command. The lint
tool version is pinned in `.github/workflows/build.yml`.

## Architecture

This is a single Go binary for Prometheus discovery of GCP Memorystore
Memcached nodes. Production code is currently in the root `main` package in
`main.go`.

- Kingpin flags populate `MemorystoreSDConfig`. `NewDiscoverer` creates a
  `MemorystoreDiscovery` embedding Prometheus `refresh.Discovery`, which schedules
  `refresh` calls (60 seconds by default). The GCP client is created lazily and
  cached; an explicit `CredentialsFile` can override default client credentials.
- Each refresh uses the GCP `ListInstances` iterator for the configured project
  and location, optionally filtering by instance. It clones instance labels for
  each node and returns one `targetgroup.Group` per node. Keep consuming the
  iterator until `iterator.Done` so all pages are included.
- The adapter imported from Prometheus's
  `documentation/examples/custom-sd/adapter` consumes discovery updates and
  writes file-SD JSON. `ouputHTTPHandler` reads that same output file; it does
  not independently query GCP. Defaults are `memorystore.json` and
  `:8888/memorystore.json`; `--output.http-path=""` disables only the target HTTP
  endpoint. Metrics and pprof share the exporter-toolkit HTTP server.
- Discovery metrics must be registered before constructing the discoverer:
  `discovery.RegisterConfig`, `discovery.NewRefreshMetrics`, then
  `discovery.RegisterSDMetrics`. Pass the metrics entry keyed by `conf.Name()`
  into `NewDiscoverer`; it expects this repository's `memorystoreMetrics` type.

## Discovery contracts and tests

- Target addresses deliberately remain `"undefined"` in both the target and
  group labels. Consumers use the README's multi-target exporter relabeling
  rules to derive `__param_target` from node host/port and set `__address__` to
  the exporter. Do not replace this with direct node scraping.
- Group sources are `<full instance resource name>/<node ID>`. The `lasts` map
  tracks previous sources, and removed nodes must emit empty groups so the
  adapter removes stale targets. Only replace `lasts` after a successful refresh.
- Metadata keys are package-level constants under
  `__meta_memorystore_memcached_`. GCP user-label keys pass through
  `strutil.SanitizeLabelName`; clone instance labels before adding node labels.
  Coordinate output changes with the README's label/relabeling documentation
  and `expectedMemorystoreJSON` in `integration_test.go`. The current
  `memory_size_gb` label contains `NodeConfig.MemorySizeMb` unchanged; its name
  does not imply a conversion.
- `startMemorystoreIntegration` runs a fake Memcache gRPC server over `bufconn`,
  injects `clientOptions` with authentication disabled, and uses a temporary
  output file and a private Prometheus registry. These tests need no GCP
  credentials or live resources. Config registration is guarded by `sync.Once`
  because the Prometheus discovery registry is global.
- Both integration tests exercise the real refresh/adapter path; the HTTP test
  additionally wraps `ouputHTTPHandler` with `httptest.NewServer`. Reuse these
  helpers and `waitForOutputFile` when extending output coverage. Existing
  guidance calls for table-driven cases, especially for pagination and refresh
  scheduling.

## Repository conventions

- Reuse the existing `slog`/`promslog` logger and Prometheus registry wiring.
- `.golangci.yml` uses `gci` import groups: standard library, third-party, then
  this module; `gofumpt` has extra rules enabled.
- `memorystore.json`, the built binary, and `dist/` are ignored generated
  artifacts, not source files.
- Use Conventional Commits and PR titles (`feat:`, `fix:`, `chore(deps):`,
  etc.); keep commit subjects under 72 characters. The semantic PR check
  validates the title. Semantic-release on `main` maintains `CHANGELOG.md` and
  invokes GoReleaser; `.goreleaser.yml` controls binaries, version metadata,
  container images, and signing.
- For target-generation changes, include output evidence in the PR and describe
  any deployment or relabeling changes.

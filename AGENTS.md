[//]: # vim: textwidth=80

# Repository Guidelines

## Project Structure & Module Organization

The Go module root holds all build and runtime assets. `main.go` implements the
Memorystore service discovery loop and is accompanied by `go.mod` / `go.sum` for
dependency pinning. A generated file such as `memorystore.json` is written at
runtime and should stay out of version control. Add new packages or helpers in
subdirectories keyed to their responsibility, and colocate supporting tests with
the code they cover.

## Build, Test, and Development Commands

- `go build .` compiles the CLI into `./prometheus-memorystore-sd`.
- `go run . --help` is the fastest way to confirm flag behavior during
development.
- `go test -v ./...` runs the current unit test suite.
- `ko build --base-import-paths --local .` produces a container image identical
to CI.

## Coding Style & Naming Conventions

Use the standard Go toolchain formatting: run `go fmt ./...` before every
commit, and keep imports sorted via `goimports` or your editor. Follow idiomatic
Go style with camelCase identifiers and exported names prefixed by a doc
comment. Prefer package-level constants for Prometheus label keys, and reuse
existing `slog` loggers rather than creating bare `log` instances.

## Testing Guidelines

Author table-driven `_test.go` files beside the code under test. Aim to cover
new branches, especially around Memorystore API pagination and refresh
scheduling. When tests depend on mock data, add fixtures with descriptive names
and document their intent. Run `go test -race ./...` before shipping user-facing
changes.

## Commit & Pull Request Guidelines

Match the established Conventional Commit format (`fix:`, `chore(deps):`,
`chore(release): ...`). Group related changes together and keep the subject
under 72 characters. PRs should include a concise summary, mention any linked
GitHub issues, and note deployment or relabeling steps if behavior changes.
Attach test output or screenshots when altering Prometheus target generation so
reviewers can validate the impact quickly.

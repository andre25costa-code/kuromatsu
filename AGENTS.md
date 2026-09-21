# Repository Guidelines

## Project Structure & Module Organization

Kuromatsu is a Go personal AI assistant derived from PicoClaw. `cmd/kuromatsu/` contains the CLI; `cmd/membench/` and `cmd/nativebench/` contain benchmark tools. Core packages live in `pkg/`, including agent orchestration, gateway, providers, configuration, and tools. Unit tests sit beside source files. Docker integration suites and fixtures live in `integration/`. `assets/` holds media, `workspace/` holds onboarding templates, and `config/` provides example configuration. Use `software-spec/` for design documentation, `docker/` and `deploy/` for deployment, and `build/` for generated binaries. Native inference uses the `llama.cpp/` submodule and local `models/`.

## Build, Test, and Development Commands

Install Go 1.25.13 or newer and Make; formatting and linting require golangci-lint v2. Docker integration commands also require Bash and Docker Compose.

- `make build`: generate sources and build the CLI into `build/`.
- `make run ARGS="--help"`: build and run the CLI locally.
- `make test`: generate sources and run Go tests.
- `make integration-test`: run Docker-backed integration suites.
- `make fmt`: apply the configured Go formatters.
- `make lint`: run configured linters.
- `make check`: download/verify dependencies, format, vet, and test before submitting.

Make defaults to `CGO_ENABLED=0` and build tags `goolm,stdjson`. Preserve these tags when running targeted tests: `go test -tags goolm,stdjson ./pkg/session/ -run TestName -v`.

## Coding Style & Naming Conventions

Follow idiomatic Go: tabs for indentation, lowercase package names, exported `CamelCase` identifiers, and descriptive filenames such as `native_fallback.go`. Follow `.golangci.yaml`; formatting includes gofmt, gofumpt, goimports, gci, and golines with a 120-character target. Group imports as standard library, external dependencies, then local module.

## Testing Guidelines

Use Go's `testing` package and existing testify helpers where appropriate. Name tests `TestXxx` in `*_test.go` files. Add regression coverage for behavior changes. Integration tests typically use `*_integration_test.go` and the `integration` build tag; extend `integration/suites/` for Docker-backed CI coverage. CI runs tests without a numeric coverage threshold.

## Commit & Pull Request Guidelines

Use focused, imperative English Conventional Commits, matching history: `fix(agent): preserve ready responses`. Branch from and target `main`. Complete the PR template with the change rationale, related issues, test environment, and required AI involvement disclosure. Include logs or screenshots when useful. Run `make check`, ensure CI passes, and obtain maintainer review; see `CONTRIBUTING.md` for details.

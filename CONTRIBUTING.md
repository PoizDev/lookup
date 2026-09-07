# Contributing to Lookup

Thank you for improving Lookup. Keep changes focused, explain user-visible behavior, and include tests for every behavioral change.

## Development environment

- The Go toolchain version declared by `go.mod`
- A C toolchain with CGO support; Lookup's tree-sitter parser cannot be built with `CGO_ENABLED=0`
- On Linux release builds, a musl toolchain such as `musl-gcc`

```bash
go mod download
go build ./...
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
goreleaser check
test -z "$(gofmt -l .)"
```

Run `gofmt -w` on changed Go files. Use the tool versions pinned by CI where reproducibility matters. Do not weaken assertions or remove tests to make a change pass. Provider tests must use local HTTP test servers and must never contain real credentials.

## Release architecture

Lookup cannot be built with `CGO_ENABLED=0` because its tree-sitter parser contains C code. Release builds therefore run natively: Linux amd64/arm64 use `musl-gcc` with static external linking, macOS amd64/arm64 use native Clang runners, and Windows amd64 uses native MinGW. Each runner executes `goreleaser build --single-target`, smoke-tests the binary, and uploads it. Only after every platform build and configured Linux distro smoke job succeeds does the aggregation job create archives, SHA-256 checksums, and the GitHub Release.

Validate release configuration changes with:

```bash
goreleaser check
GOOS="$(go env GOOS)" GOARCH="$(go env GOARCH)" goreleaser build --single-target --snapshot --clean
```

Linux snapshots additionally require `CC=musl-gcc`; the checked-in GoReleaser override supplies that compiler name and static linker flags.

## Project structure

- Analyzer rules implement `internal/analyzer.Rule`; generic rules live under `internal/analyzer/generic`, while language-specific semantic rules live under `internal/language/<language>/rules` for Go, Python, Rust, and C#.
- AI providers implement the contracts in `internal/ai` and belong in `internal/ai/providers`. Preserve request cancellation, finite timeouts, bounded retries, and secret sanitization.
- Reporters implement `internal/reporter.Reporter`. JSON, SARIF, and completion output must remain machine-readable and free of banners or progress output.
- Language support is registered through `internal/language.Registry`; avoid coupling core graph code to one language.

## Pull requests

Create a focused branch, add tests first for behavior changes, and describe motivation, compatibility impact, and validation commands in the pull request. CI must pass before review. New dependencies require a maintenance and security justification.


By contributing, you agree that your contribution is licensed under AGPL-3.0-or-later.

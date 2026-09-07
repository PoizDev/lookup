# Changelog

All notable changes to Lookup are documented here. The project follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0]

### Added

- Deterministic codebase analysis for Go, Python, Rust, and C# using Tree-sitter-backed parsing, normalized semantic facts, code graph construction, and bounded dataflow analysis.
- Security, correctness, architecture, performance, and maintainability rule packs with a documented public rule catalog.
- Evidence-backed adjudication contract separating deterministic observations from bounded contextual AI adjudication, with unresolved candidates retained as `NOT_REVIEWED` when review is disabled or unavailable.
- `STATIC_RISK` scoring and availability-gated `VERIFIED_HEALTH` scoring with deterministic review coverage semantics.
- Smart, all, and off AI review modes with scan-wide request/token budgets, bounded concurrency, caching, redaction, and fail-closed provider behavior.
- OpenAI, Anthropic, Gemini, and Ollama providers.
- Terminal, Markdown, JSON, and SARIF 2.1.0 reporters, including GitHub Code Scanning integration.
- Interactive `lookup init`, Bash/Zsh/Fish/PowerShell completion, `NO_COLOR` support, quiet/non-TTY behavior, and explicit scan completion/report destination UX.
- Markdown intentionally retains personality for the canonical zero-finding result, while JSON, SARIF, quiet mode, and non-TTY terminal output remain personality-free.
- Stable/prerelease-aware update discovery, cached interactive notifications, and user-approved checksum-verified self-update for official-installer installations.
- Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 release artifacts with SHA-256 checksums and native installer scripts.
- CI and release pipelines covering formatting, tests, race detection, vet, build, GoReleaser validation, platform builds, Linux distro smoke tests, and CI vulnerability scanning.

### Security

- Updated the release toolchain to Go 1.26.8 and `golang.org/x/net` to v0.55.0 to include fixes for vulnerabilities reachable through the previous toolchain/dependency set.

[Unreleased]: https://github.com/poizdev/lookup/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/poizdev/lookup/releases/tag/v0.1.0

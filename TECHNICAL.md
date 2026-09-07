# Lookup — Technical Architecture

This document describes Lookup's internal architecture, analysis pipeline, reliability model and release engineering.

For installation and normal CLI usage, see [README.md](README.md).

---

## Technology Stack

| Component            | Technology                  | Purpose                                   |
| -------------------- | --------------------------- | ----------------------------------------- |
| Language             | Go 1.26+                    | Core CLI and analysis engine              |
| CLI                  | Cobra                       | Commands, flags and completion generation |
| Interactive UI       | Bubble Tea                  | `lookup init` setup wizard                |
| Styling              | Lip Gloss                   | Terminal design system                    |
| Parsing              | Tree-sitter                 | Source parsing                            |
| Configuration        | TOML                        | Persistent user configuration             |
| Logging              | Zap                         | Structured diagnostics                    |
| Git ignore semantics | go-git                      | Repository ignore matching                |
| Output               | JSON / Markdown / SARIF     | Human and machine reporting               |
| Distribution         | GoReleaser + GitHub Actions | Release builds and artifacts              |

Tree-sitter requires CGO. Release architecture therefore treats platform builds explicitly rather than assuming pure-Go cross compilation.

---

# System Architecture

Lookup is separated into independent pipeline stages:

```text
┌─────────────────────┐
│      CLI / Cobra    │
└──────────┬──────────┘
           │
           ↓
┌─────────────────────┐
│ Configuration Layer │
└──────────┬──────────┘
           │
           ↓
┌─────────────────────┐
│ Repository Discovery│
└──────────┬──────────┘
           │
           ↓
┌─────────────────────┐
│ Language Adapter    │
│ + Tree-sitter       │
└──────────┬──────────┘
           │
           ↓
┌─────────────────────┐
│ Unified Code Graph  │
└──────────┬──────────┘
           │
           ↓
┌─────────────────────┐
│ Analyzer Engine     │
│ Deterministic Rules │
└──────────┬──────────┘
           │
           ↓
┌─────────────────────┐
│ Potential Findings  │
└──────────┬──────────┘
           │
           ↓
┌─────────────────────┐
│ Bounded Contextual  │
│ AI Adjudication     │
└──────────┬──────────┘
           │
           ↓
┌─────────────────────┐
│ Finalization        │
└──────────┬──────────┘
           │
           ↓
┌─────────────────────┐
│ Deterministic       │
│ Scoring             │
└──────────┬──────────┘
           │
           ↓
┌─────────────────────┐
│ Presentation        │
│ Filtering           │
└──────────┬──────────┘
           │
           ↓
┌────────────────────────────┐
│ Reporters                  │
│ Terminal / MD / JSON/SARIF │
└────────────────────────────┘
```

The AI layer is intentionally **not** the analysis engine.

Deterministic rules produce potential findings first. **Detection is not judgment.** A rule may explicitly declare a result static-authoritative; all other candidates require contextual adjudication before becoming final findings.

AI adjudicates bounded static candidates; it does not independently search the repository for new findings. If AI is unavailable, Lookup retains the deterministic candidates as `NOT_REVIEWED` instead of promoting or discarding them.

---

# Package Layout

```text
cmd/lookup/
    CLI entrypoint and commands

internal/ai/
    Provider-neutral AI review pipeline

internal/ai/providers/
    OpenAI, Anthropic, Gemini and Ollama adapters

internal/analyzer/
    Analyzer engine and generic rules

internal/apperror/
    Typed application errors

internal/completion/
    Shell detection and completion installation

internal/config/
    Configuration loading and precedence

internal/discovery/
    Repository walking and ignore handling

internal/finding/
    Normalized finding model

internal/graph/
    Unified code graph

internal/language/
    Language adapter contracts

internal/language/golang/, python/, rust/, csharp/
    Tree-sitter adapters, semantic extractors and language rules

internal/progress/
    Pipe-safe progress reporting

internal/reporter/
    Terminal, Markdown, JSON and SARIF reporters

internal/scoring/
    Deterministic health scoring

internal/setup/
    Bubble Tea setup wizard

internal/ui/
    Shared terminal design system

internal/update/
    Release discovery, cache, provenance and safe executable replacement
```

---

# Repository Discovery

Discovery walks the target repository and applies:

```text
explicit skip directories
.gitignore rules
nested .gitignore rules
file size limits
test-file policy
language classification
```

Lookup uses Git-compatible ignore semantics rather than a simplified glob approximation.

Ignored directories are skipped before parsing to reduce unnecessary I/O and analysis cost.

The discovery layer recognizes several common source and configuration file extensions, but classification does not automatically mean semantic analysis support exists for that language.

---

# Language Support Model

Language adapters expose three support levels:

| Level | Name       | Capability                                                   |
| ----- | ---------- | ------------------------------------------------------------ |
| L1    | Parse      | Source can be parsed                                         |
| L2    | Structural | Symbols, functions, types, imports and calls enter the graph |
| L3    | Semantic   | Language-specific analysis rules are enabled                 |

Lookup registers **Go, Python, Rust, and C#** as active parse, structural, and semantic adapters. `Capabilities()` is the source of truth for coverage; each reports L3 because it implements the `SemanticAdapter` contract.

The architecture is intentionally adapter-based so additional languages can be added without redesigning the analysis engine.

A language adapter implements responsibilities equivalent to:

```go
Language()
Capabilities()
Parse()
ExtractSymbols()
ExtractFunctions()
ExtractTypes()
ExtractImports()
ExtractCalls()
LanguageRules()
```

---

# Semantic Graph and Facts

Lookup parses each supported file once. The language adapter still populates the unified graph with files, symbols, functions, types, imports, calls, and relationships, and may also implement the optional `SemanticAdapter` extension. The Go, Python, Rust, and C# adapters use that extension to attach one immutable semantic document derived from the same Tree-sitter tree.

```text
Tree-sitter AST
       ↓
Language Adapter
       ↓
Unified Graph + Semantic Document
       ↓
Ecosystem Detection + Fact Index
       ↓
Bounded Dataflow
       ↓
Deterministic Rule Packs
```

Normalized facts represent sources, sinks, guards, routes, middleware, database and HTTP operations, command/file/crypto operations, resource lifecycle, concurrency, propagation, calls, returns, and control-flow decisions. Facts have stable IDs, source locations, enclosing functions, inputs/outputs, expressions, and operation metadata. Go-only behavior such as `defer`, goroutines, channels, contexts, and WaitGroups remains in the Go extractor/rules instead of leaking into the shared model.

Imports deterministically activate `net/http`, `database/sql`, `os/exec`, `crypto/*`, templates, filesystem APIs, Gin, Fiber v2/v3, and GORM. Engine-level ecosystem gating prevents inactive framework packs from executing. Gin and Fiber emit the same HTTP query/path/header/body source facts. Route facts retain method, path, handler, group path, ordered visible middleware chain, and an explicitly unknown protection semantic; middleware names are never treated as authentication proof.

The semantic index and dataflow traces are cached on the immutable graph snapshot, so rules follow parse once, extract once, index once, analyze many. Taint state is versioned by program location: assignments create new versions, literal overwrites kill later flow without erasing earlier sink evidence, explicit sanitizer facts stop propagation, and calls/returns are bounded by step and call-depth limits. Cross-file calls propagate only when the target symbol can be resolved unambiguously.

---

# Dataflow and Evidence

The taint engine is deliberately bounded rather than whole-program symbolic execution. It follows direct HTTP sources through local assignment, aliases, concatenation, `fmt.Sprintf`, positional function arguments, simple parameter returns, and a bounded call depth. Cycles and step depth terminate deterministically. Safe SQL placeholder calls do not become sinks.

Taint findings contain ordered source, propagation, and sink evidence. Each step carries a physical location, expression, message, and related function. Terminal and Markdown summarize these steps, JSON exposes them additively, and SARIF creates physical `codeFlows` from the same evidence without recomputing dataflow in the reporter.

Static confidence is evidence-derived: direct source-to-sink is `0.95`, local propagation is `0.90`, bounded interprocedural propagation is `0.80`, structural proof is normally `0.85`, and explicitly heuristic context is `0.50`.

---

# Analyzer Engine

Lookup evaluates Security, Correctness, Architecture, Performance, and Maintainability findings. Rule registration rejects empty and duplicate IDs. Rules and findings are sorted deterministically after concurrent execution. IDs use Go/framework namespaces such as `GO-RES-001`, `GIN-SEC-001`, `FIBER-SEC-001`, and `GORM-SEC-001`; the generic layout rule remains `ARC-001`.

The runtime catalog combines the generic architecture rule with active Go, Python, Rust, and C# language/ecosystem packs. The complete current catalog and migration mapping are maintained in [`docs/RULES.md`](docs/RULES.md) rather than duplicated here.

## Release language ecosystem packs

Python normalizes Flask, FastAPI and Django request inputs to shared HTTP source facts and recognizes provenance-backed DB-API, subprocess/shell, filesystem, pickle, TLS, PyTorch, OpenCV and Ultralytics operations. C# performs the same normalization for ASP.NET Core and distinguishes EF Core raw APIs from interpolated/parameterized calls before modeling System process, filesystem, HTTP, Task and TLS operations. Rust normalizes Axum, Actix Web and Tonic inputs and recognizes Tokio, SQLx, Diesel, reqwest and std systems operations.

Dataflow-backed packs consume `Graph.DataflowTraces()` and therefore reuse the immutable semantic index and bounded trace snapshot. Structural configuration rules consume normalized facts directly. No ecosystem rule reparses source.

Base severity is declared by each rule. Deterministic evidence may refine static confidence and, where a rule explicitly supports it, static severity. AI adjudication may preserve or lower the static proposal, but it cannot strengthen severity or confidence. Maintainability findings remain quality signals, while source-to-sensitive-sink findings are security risk signals. Category/severity scoring weights remain deterministic.

---

# AI Review Architecture

Before an eligible contextual candidate reaches a provider, `internal/reviewcontext`
builds a typed `ReviewPacket` from the already-built semantic document, code graph,
cached bounded dataflow traces, symbol definitions, adjacent comments, and detected
ecosystems. The packet keeps observation and hypothesis separate and gives every
evidence item a stable packet-local ID. AI adjudicates bounded static candidates; it
does not independently search the repository for new findings.

Construction is deterministic and hard-bounded per candidate: 120 source lines, 12
symbols, 4 callers, 4 callees, graph depth 2, 12 provenance steps, 32 evidence items,
and approximately 3,000 input tokens by default. Primary evidence has highest
retention priority. Packets expose byte/token estimates, evidence count, truncation,
missing information, and `SUFFICIENT`, `INCOMPLETE`, or `INSUFFICIENT` context state.
Structurally insufficient packets short-circuit to `UNCERTAIN`; incomplete or
truncated packets tell the provider to prefer `UNCERTAIN` rather than invent context.

Provider decisions may cite only evidence IDs present in the transmitted packet.
Invented references invalidate `CONFIRMED`/`DOWNGRADED`; non-actionable verdicts have
invalid references removed. Canonical redaction happens before both transmission and
cache-key calculation, and the prompt schema version includes the semantic packet so
older shallow-context entries safely miss.

Lookup supports:

```text
OpenAI
Anthropic
Google Gemini
Ollama
```

AI review is a bounded contextual adjudication stage that can be disabled operationally. The default mode is `smart`; `all` bypasses eligibility and `off` (including the `--no-ai` alias) makes no provider call. In `off` mode, deterministic analysis and `STATIC_RISK` remain available, while contextual candidates remain `NOT_REVIEWED` and may make `VERIFIED_HEALTH` unavailable:

```text
PotentialFinding
      ↓
Deterministic ReviewPlan
      ↓
Assessment Cache
      ↓
Canonical Context + Redaction
      ↓
Scan-Global Budget + Token-Aware Batching
      ↓
Bounded Worker Pool
      ↓
Provider
      ↓
Structured Response
      ↓
Schema Validation
      ↓
Typed ReviewDecision
      ↓
Certainty Gate / Finalization
      ↓
FinalFinding
      ↓
Deterministic Scoring
      ↓
Presentation Filters
      ↓
Reporter
```

Smart eligibility combines confidence, evidence strength, structured source/propagation/sink evidence, category and contextual rule characteristics. Selected candidates are ordered deterministically and stratified across rule IDs so one repetitive family cannot monopolize the budget. ALL skips SMART selection but still obeys hard limits. OFF builds no review packets. Severity/category flags filter displayed final findings only; they never alter canonical candidates, review coverage, or scores.

The provider receives one canonical minimal context instead of the entire finding or repository. Recognizable API keys, bearer tokens, JWTs, cloud credentials, private keys and credential assignments are replaced with stable placeholders before every provider request. It may confirm, downgrade, reject as unsupported, or mark a candidate uncertain. It cannot invent findings/evidence, exceed static severity or confidence, or choose scores. Provider output is normalized into a typed decision before the certainty gate.

Code snippets are line-bounded and oversized contexts are deterministically minimized. A reusable preflight `ReviewPlan` owns the selected IDs, exact request estimates, packet metadata, batches, active budget, and typed skip reasons. Batch limits remain provider-call bounds; a mutex-protected scan ledger prevents concurrent calls and retry splits from exceeding the global request/input/output ceilings. Defaults are 40 findings, 6 requests, 60,000 estimated input tokens, and 16,000 planned output tokens.

AI assessments use a separate platform/XDG-aware persistent cache keyed by stable finding/evidence context, rule/prompt versions, provider and effective model. Cache I/O and corrupt entries degrade to a fresh review rather than failing analysis.

Provider output must conform to a structured response containing:

```text
finding ID
evidence_relation (`SUPPORTS`, `CONTRADICTS`, or `INSUFFICIENT`)
claim_strength (`UNCHANGED` or `LOWER`) when evidence supports the claim
severity (bounded by the static proposal) when applicable
confidence (bounded by the static proposal) when applicable
reason
recommendation
evidence_ids (packet-local evidence references)
```

Lookup derives the canonical adjudication state (`CONFIRMED`, `DOWNGRADED`, `UNSUPPORTED`, or `UNCERTAIN`) from this evidence relation and bounded claim strength; the provider does not choose the canonical verdict. Responses are normalized and validated before they can modify a finding.

Invalid, malformed, incomplete, or evidence-inconsistent provider output is rejected.

---

# AI Failure Model

AI is designed as a degradable dependency.

If a provider:

```text
times out
returns malformed output
is rate limited
cannot be reached
fails a batch
```

Lookup preserves deterministic candidates as `NOT_REVIEWED` instead of terminating the scan or implicitly confirming them.

This gives the pipeline the following behavior:

```text
Static analysis works
      +
AI available
      ↓
Adjudicated final findings + retained audit candidates

Static analysis works
      +
AI unavailable
      ↓
NOT_REVIEWED candidates + static-authoritative finals + warning
```

`--ai-review=off` and its backward-compatible `--no-ai` alias disable the entire provider stage.

---

# Provider Reliability

Provider network calls use bounded lifecycles.

The provider layer supports:

```text
context cancellation
finite request timeouts
bounded retries
Retry-After handling
exponential backoff
jitter
maximum retry budget
credential-safe errors
```

The CLI's root cancellation context propagates to provider operations.

A Ctrl-C therefore cancels in-flight AI work instead of leaving an independent network request running.

---

# Scoring Model

Lookup calculates deterministic category and overall scores with explicit provenance. `STATIC_RISK` uses deterministic candidates before contextual verification is complete. `VERIFIED_HEALTH` uses only static-authoritative, confirmed, and downgraded final findings, and exists only when every score-eligible contextual candidate is decisive. Unsupported candidates add no penalty; uncertain and not-reviewed candidates prevent verified-score availability.

Default category weights:

| Category        | Weight |
| --------------- | -----: |
| Security        |    30% |
| Correctness     |    25% |
| Architecture    |    20% |
| Performance     |    15% |
| Maintainability |    10% |

Finding penalties depend on severity and confidence.

Conceptually:

```text
Category Score
    =
100
-
Σ(severity penalty × confidence)
```

Scores are clamped at zero.

The final overall score is the weighted sum of all category scores.

This keeps scoring deterministic for a given normalized finding set.

---

# Configuration Resolution

Configuration precedence is:

```text
CLI flags
    ↓
Environment variables
    ↓
config.toml
    ↓
Defaults
```

This means an explicit command-line option always overrides environment and persistent configuration.

Lookup uses the operating system's native user configuration directory.

On Unix, this respects mechanisms such as `XDG_CONFIG_HOME`.

Credential-containing configuration files use restricted permissions:

```text
config directory: 0700
config file:      0600
```

---

# Terminal and Machine Output Separation

Lookup deliberately separates human UI from machine output.

For:

```text
JSON
SARIF
```

stdout contains only the serialized report.

Diagnostics, warnings and logs remain on stderr.

Therefore the following is safe:

```sh
lookup --no-ai . --output json | jq .
```

and:

```sh
lookup . --output sarif > results.sarif
```

Spinners and banners are suppressed where they could corrupt machine-readable output.

---

# SARIF

Lookup implements SARIF 2.1.0 for integration with code-scanning systems.

The reporter provides:

```text
stable rule IDs
severity mapping
repository-relative paths
source locations
code-flow information
Lookup version metadata
deterministic ordering
```

Severity mapping follows SARIF-compatible levels:

```text
Critical / High → error
Medium          → warning
Low / Info      → note
```

SARIF is used directly by the bundled GitHub Action.

---

# Secret-Safe Terminal Rendering

A security scanner must not create a second secret leak while reporting the first one.

Lookup therefore treats terminal evidence specially.

Sensitive values detected in code snippets are redacted before interactive rendering.

The terminal reporter also applies width-aware wrapping and truncation so pathological source lines cannot destroy the CLI layout.

Machine reporters remain structurally independent from terminal styling.

---

# Error Architecture

Lookup uses typed application errors rather than classifying failures by matching strings.

Major categories include:

```text
authentication
rate limiting
network timeout
configuration
filesystem permission
path not found
provider initialization
```

Domain layers return typed errors.

The UI layer translates those errors into user-facing messages and possible remediation steps.

This keeps rendering concerns out of core analysis logic.

---

# Shell Integration

Completion scripts are generated through Cobra for:

```text
bash
zsh
fish
PowerShell
```

`lookup completion install` adds an installation layer around those generators.

The integration provides:

```text
shell detection
platform-aware paths
user-level installation
idempotent updates
explicit profile modification
safe fallback when detection fails
```

The same integration can optionally run during `lookup init`.

Completion generation itself remains stdout-pure so package managers and dotfile systems can use it independently.

---

# Release Architecture

Tree-sitter requires CGO, which changes how Lookup is distributed.

Lookup does **not** rely on a single `CGO_ENABLED=0` cross-build.

## Linux

Linux release binaries are built with CGO enabled and statically linked against musl.

Targets:

```text
linux/amd64
linux/arm64
```

Static Linux artifacts avoid runtime dependency on the host glibc loader.

The release workflow performs representative distro smoke tests against environments including:

```text
Ubuntu
Debian
Fedora
Arch Linux
Alpine
Nix
```

## macOS

Native builds:

```text
darwin/amd64
darwin/arm64
```

are produced on macOS runners with CGO enabled.

## Windows

Lookup builds:

```text
windows/amd64
```

on a native Windows runner with a C toolchain available for Tree-sitter.

---

# Update Discovery and Self-Update

```text
GitHub Releases
      ↓
Cached discovery
      ↓
SemVer channel selection
      ↓
Notification policy
      ↓
Installation provenance
      ↓
Artifact + SHA-256 checksum
      ↓
Platform-safe replacement
```

`internal/update` owns release discovery, SemVer comparison, the update cache, notification cooldown, artifact resolution, checksum verification, installer receipts and replacement mechanics. Cobra commands and terminal UI consume this layer without owning supply-chain policy.

Passive discovery is best-effort and uses a short bounded HTTP client. Network, rate-limit, invalid-response and cache errors never fail the scan. The cache permits at most one stable discovery request per 24 hours, while a seven-day notification cooldown prevents the same version from appearing on every invocation. A newly discovered version can notify immediately. `LOOKUP_NO_UPDATE_CHECK=1`, CI, non-TTY stderr, completion generation, the update command itself and development builds suppress passive discovery.

Explicit `lookup update --check` bypasses the cache and therefore reports discovery failures with a non-zero exit status. Prerelease discovery requires `--prerelease`; stable users are never moved onto an RC, beta or alpha channel implicitly.

Notification text is diagnostics/UI rather than report data. It is written only to interactive stderr. Terminal, Markdown, JSON and SARIF stdout contracts remain independent, so redirected and piped machine output is byte-clean.

Self-update requires an installation receipt written by the official `install.sh` or `install.ps1`. The receipt binds the canonical `poizdev/lookup` repository and official-installer method to the exact executable path. Missing, malformed or mismatched receipts are treated as manual/package-manager ownership, and Lookup refuses to overwrite the executable.

An approved update downloads the platform artifact and `checksums.txt` over HTTPS, verifies SHA-256, extracts into a temporary directory and executes the candidate's `version --short` before replacement. Unix writes a same-directory staged file and atomically renames it over the existing executable while preserving permissions. Windows stages `lookup.exe.update.exe` and starts a PowerShell helper that waits for the current process to exit before replacing the executable.

# Release Gate

A release is not published immediately after compilation.

The workflow is structured as:

```text
Tag
 ↓
Quality Gate
 ↓
Platform Builds
 ↓
Platform Smoke Tests
 ↓
Linux Distro Smoke Matrix
 ↓
Artifact Aggregation
 ↓
SHA-256 Checksums
 ↓
Final Candidate Validation
 ↓
GitHub Release
```

Publishing only happens after required upstream jobs succeed.

Prerelease tags such as:

```text
v0.1.0-rc.1
```

are published as GitHub prereleases.

Stable tags such as:

```text
v0.1.0
```

produce normal releases.

---

# GitHub Action Supply Chain

The bundled Action does not blindly execute a remote installation pipe.

Instead it:

```text
resolves the selected Lookup version
detects runner OS and architecture
downloads the exact release archive
downloads checksums.txt
verifies SHA-256
extracts into a temporary tool directory
adds Lookup to PATH
runs SARIF analysis
optionally uploads SARIF to Code Scanning
```

Provider credentials are mapped only to the selected provider's environment variable and are masked in GitHub Actions logs.

---

# Security Boundaries

Important security properties include:

```text
API keys are never intentionally logged
provider errors sanitize credentials
config files are private on Unix
terminal secret evidence is redacted
release archives are checksum verified
GitHub Actions dependencies are pinned
AI failure does not bypass deterministic analysis
machine output is isolated from terminal UI
```

See [SECURITY.md](SECURITY.md) for vulnerability reporting.

---

# Testing Strategy

Lookup uses tests at several architectural boundaries:

```text
analyzer rule tests
language adapter tests
AI normalization tests
provider behavior tests
configuration tests
gitignore fixture tests
reporter tests
SARIF tests
completion tests
wizard tests
scoring tests
```

Release quality gates additionally execute:

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./...
gofmt
goreleaser check
```

Platform-specific smoke tests then validate the actual built binaries.

---

# Design Principles

Lookup follows a few intentional engineering constraints.

**Deterministic before probabilistic.**
Static rules produce candidates. Static certainty or bounded adjudication produces final findings; AI does not replace the analyzer.

**Machine output is a contract.**
JSON and SARIF must never be polluted by UI output.

**AI must be degradable.**
A provider outage must not make local static analysis unusable.

**Language-specific parsing stays behind adapters.**
The analyzer should not depend directly on Tree-sitter grammar internals.

**Release support requires evidence.**
A platform is not considered supported merely because cross compilation appears possible.

**Security tools must not leak what they detect.**
Secret findings are redacted in terminal presentation.

---

# Current Scope

Lookup v0.1.0 is intentionally focused.

Active semantic language adapters cover **Go, Python, Rust, and C#**. Their analysis is syntax- and provenance-backed rather than compiler-complete: runtime Python dispatch, Rust macro/trait resolution, and compiler-level C# type inference are intentionally unresolved where Lookup cannot prove them deterministically.

The architecture defines boundaries for additional language adapters, analyzer rules, and provider integrations, but unsupported capabilities should not be inferred from those extension points.

---

# Related Documentation

* [README.md](README.md) — installation and usage
* [CONTRIBUTING.md](CONTRIBUTING.md) — development workflow
* [SECURITY.md](SECURITY.md) — vulnerability reporting
* [CHANGELOG.md](CHANGELOG.md) — release history
* [LICENSE](LICENSE) — AGPL-3.0-or-later

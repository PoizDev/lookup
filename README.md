<div align="center">


<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./docs/assets/lookup-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./docs/assets/lookup-light.svg">
  <img alt="Lookup" src="./docs/assets/lookup-light.svg" width="460">
</picture>

### Codebase intelligence from your terminal.

**Static analysis · Evidence · AI review · SARIF · CI/CD**

[![Release](https://img.shields.io/github/v/release/poizdev/lookup?style=flat-square)](../../releases)
[![CI](https://img.shields.io/github/actions/workflow/status/poizdev/lookup/ci.yml?branch=main\&style=flat-square\&label=CI)](../../actions)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square\&logo=go\&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-AGPL--3.0-purple?style=flat-square)](LICENSE)

**Linux · macOS · Windows**

</div>

---

**Lookup** is a static-analysis-first, evidence-grounded, AI-adjudicated codebase intelligence CLI written in Go.

It discovers source files, builds a unified code graph, and always runs deterministic analyzers across security, correctness, architecture, performance and maintainability before bounded contextual AI adjudication. AI cannot create findings or strengthen the static proposal, but contextual candidates require a decisive review before they become final findings.

Everything runs from the terminal and can produce human-readable reports or machine-readable output for CI systems.

```text
Repository
    ↓
Discovery
    ↓
Parsing
    ↓
Unified Code Graph
    ↓
Deterministic Analysis
    ↓
Potential Findings
    ↓
Bounded Contextual AI Adjudication
    ↓
Finalization
    ↓
Deterministic Scoring
    ↓
Presentation Filtering
    ↓
Terminal / Markdown / JSON / SARIF
```

> **Language support:** Lookup provides Tree-sitter parse, structural graph extraction, and normalized semantic analysis for **Go, Python, Rust, and C#**. Analysis is syntax-backed: runtime Python dispatch, Rust macro/trait resolution, and compiler-level C# type inference remain intentionally unresolved.

Deep ecosystem intelligence currently covers Python web (Flask, FastAPI, Django), DB-API/SQLAlchemy-oriented facts, PyTorch, OpenCV and Ultralytics; C# ASP.NET Core, EF Core and selected System APIs; and Rust Tokio, Axum, Actix Web, SQLx, Diesel, reqwest and Tonic. Coverage is API- and provenance-specific rather than compiler-complete.

## Highlights

* Deterministic static analysis before any AI request
* Unified code graph for symbols, functions, types, imports and calls
* Security, correctness, architecture, performance and maintainability findings
* Bounded AI adjudication that cannot create findings, escalate static severity/confidence, or alter `STATIC_RISK`
* OpenAI, Anthropic, Gemini and Ollama providers
* Offline deterministic mode with `--no-ai`; contextual candidates remain `NOT_REVIEWED`
* Terminal, Markdown, JSON and SARIF 2.1.0 reporters
* GitHub Code Scanning integration
* Native Bash, Zsh, Fish and PowerShell completion
* Interactive `lookup init` setup wizard
* Linux, macOS and Windows release artifacts
* SHA-256 verified installers and release archives
* Pipe-safe JSON/SARIF output

---

# Installation

## Linux & macOS

Install the latest release:

```sh
curl -sSfL https://raw.githubusercontent.com/poizdev/lookup/main/install.sh | sh
```

Lookup installs to:

```text
~/.local/bin/lookup
```

by default and does **not** require `sudo`.

Install a specific version:

```sh
curl -sSfL https://raw.githubusercontent.com/poizdev/lookup/main/install.sh \
  | LOOKUP_VERSION=v0.1.0 sh
```

Choose another destination:

```sh
curl -sSfL https://raw.githubusercontent.com/poizdev/lookup/main/install.sh \
  | LOOKUP_INSTALL_DIR="$HOME/bin" sh
```

The installer automatically detects the operating system and architecture, downloads the matching release artifact and verifies its SHA-256 checksum.

## Windows

From PowerShell:

```powershell
irm https://raw.githubusercontent.com/poizdev/lookup/main/install.ps1 | iex
```

The default installation directory is:

```text
%LOCALAPPDATA%\Programs\Lookup\bin
```

## Manual installation

Prebuilt archives and `checksums.txt` are available under [GitHub Releases](../../releases).

Release targets:

| Platform | Architecture | Artifact              |
| -------- | ------------ | --------------------- |
| Linux    | amd64        | static musl `.tar.gz` |
| Linux    | arm64        | static musl `.tar.gz` |
| macOS    | amd64        | `.tar.gz`             |
| macOS    | arm64        | `.tar.gz`             |
| Windows  | amd64        | `.zip`                |

Linux releases are statically linked with musl to avoid host glibc and dynamic-loader dependencies across common distributions.

---

# Updating

Check the canonical GitHub Releases source without changing the executable:

```sh
lookup update --check
```

Official-installer installations can be upgraded with explicit user approval:

```sh
lookup update
```

Prereleases are excluded by default. Use `--prerelease` only when intentionally testing release candidates. Package-manager and manual installations are never overwritten; update those with their owning package manager or reinstall from GitHub Releases.

Lookup periodically checks GitHub Releases while running interactively. Update notices are written to terminal stderr without contaminating JSON or SARIF stdout. Disable passive checks and notifications with:

```sh
LOOKUP_NO_UPDATE_CHECK=1 lookup .
```

Explicit `lookup update` commands still work when this variable is set. You can also watch the repository's [Releases](../../releases) through GitHub notifications.

---

# Setup

Run the interactive setup wizard:

```sh
lookup init
```

The wizard can configure:

```text
AI provider
API credentials
Model
Ollama endpoint
Output format
Ignored directories
Provider validation
Shell integration
```

Supported providers:

```text
OpenAI
Anthropic
Google Gemini
Ollama
```

Lookup stores its configuration in the platform-native user configuration directory.

On Unix systems, configuration directories and credential-containing files are created with user-only permissions.

---

# Shell Completion

Lookup supports:

```text
Bash
Zsh
Fish
PowerShell
```

Automatically detect the current shell and install completion:

```sh
lookup completion install
```

Or generate completion manually:

```sh
lookup completion bash
lookup completion zsh
lookup completion fish
lookup completion powershell
```

You can also install for an explicitly selected shell when automatic detection is ambiguous.

---

# Usage

Scan the current repository:

```sh
lookup .
```

Deterministic analysis and candidate generation are deterministic, including `STATIC_RISK`. AI adjudication may affect final finding states. Scoring is deterministic for a given normalized input and finalized finding set, while `VERIFIED_HEALTH` requires decisive contextual adjudication and may vary within bounded AI review. An uncertain, unreviewed, context-insufficient, budget-skipped, or provider-failed candidate keeps verified health unavailable.

Smart AI Review selects a deterministic, rule-diverse subset of contextual candidates. `--ai-review=all` attempts every eligible contextual candidate, while `--ai-review=off` disables packet construction and provider calls; `--no-ai` remains an alias for `off`. ALL remains subject to the same scan-wide safety budget.

Default per-scan limits are 40 reviewed findings, 6 provider requests, 60,000 estimated input tokens, and 16,000 planned output tokens. Override them with `--ai-max-findings`, `--ai-max-requests`, `--ai-max-input-tokens`, and `--ai-max-output-tokens`. Lookup reports tokens and requests, not speculative dollar costs.

Run deterministic analysis without any AI provider:

```sh
lookup . --ai-review=off
```

Scan another repository:

```sh
lookup ~/Projects/example
```

Filter by severity:

```sh
lookup . --severity high
```

Include Go test files:

```sh
lookup . --include-tests
```

Enable debug diagnostics:

```sh
lookup . --debug
```

## Output formats

Human-readable terminal output:

```sh
lookup .
```

Markdown report:

```sh
lookup . --output markdown
```

JSON:

```sh
lookup . --output json > lookup.json
```

SARIF:

```sh
lookup . --output sarif > lookup.sarif
```

Machine-readable JSON and SARIF are written cleanly to stdout while diagnostics remain on stderr, making Lookup safe to use in shell pipelines and CI.

---

# AI Providers

The easiest way to configure a provider is:

```sh
lookup init
```

Environment variables are also supported.

### OpenAI

```sh
export LOOKUP_PROVIDER=openai
export LOOKUP_OPENAI_API_KEY="..."
```

### Anthropic

```sh
export LOOKUP_PROVIDER=anthropic
export LOOKUP_ANTHROPIC_API_KEY="..."
```

### Gemini

```sh
export LOOKUP_PROVIDER=gemini
export LOOKUP_GEMINI_API_KEY="..."
```

### Ollama

```sh
export LOOKUP_PROVIDER=ollama
```

Ollama does not require an API key.

To guarantee that no AI provider is contacted:

```sh
lookup --no-ai .
```

## Known limitations

- Local reasoning/thinking models may consume their configured generation budget before producing Lookup's required final structured response. Lookup fails closed in this case and does not promote the affected candidate. Improved reasoning-model compatibility is planned for a follow-up release.

---

# GitHub Actions

Lookup ships as a GitHub Action and can send SARIF findings directly to GitHub Code Scanning.

```yaml
name: Lookup

on:
  pull_request:
  push:
    branches:
      - main

permissions:
  contents: read
  security-events: write

jobs:
  lookup:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4

      - uses: poizdev/lookup@v0.1.0
        with:
          version: v0.1.0
          provider: openai
          api-key: ${{ secrets.OPENAI_API_KEY }}
```

For deterministic analysis without an external AI provider, use Lookup CLI arguments through the Action configuration.

The Action downloads a versioned release artifact and verifies its checksum before execution.

---

# How Lookup Works

Lookup intentionally separates deterministic analysis from bounded contextual AI adjudication.

A finding must first originate from an analyzer rule. Static-authoritative findings need no AI review. Other candidates require contextual adjudication before they become decisive; when AI review is disabled or unavailable, Lookup preserves them as `NOT_REVIEWED` rather than promoting or discarding them.

AI is bounded by the static proposal: it cannot create findings, increase severity or confidence, or alter `STATIC_RISK`. Lookup therefore always provides deterministic analysis and can run without a provider, while `VERIFIED_HEALTH` may be unavailable until every score-eligible contextual candidate has a decisive review outcome.

The deterministic analysis remains:

```text
repeatable
testable
available in offline deterministic mode
CI-friendly
```

while allowing AI providers to help eliminate noisy or context-insensitive findings.

For the complete architecture, rule system, scoring model, AI lifecycle and release design, see:

**[TECHNICAL.md](TECHNICAL.md)**

---

# Version

```sh
lookup version
```

For scripts:

```sh
lookup version --short
```

Example:

```text
0.1.0
```

---

# Contributing

Contributions are welcome.

Before opening a pull request, see [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, testing requirements, code organization and release architecture.

For feature requests, bug reports and reproducible problems, open a [GitHub Issue](../../issues).

---

# Security

Do **not** report security vulnerabilities through a public issue.

Please follow the private reporting process described in [SECURITY.md](SECURITY.md).

---

# Support & Contact

Found a bug or have an idea?

* Open a [GitHub Issue](../../issues)
* Check existing [Issues](../../issues) before reporting duplicates
* For project-related contact, reach the maintainer through **[@poizdev](https://github.com/poizdev)**

When reporting a problem, including the following usually helps:

```text
lookup version --short
Operating system
Architecture
Command used
Relevant error output
Minimal reproduction repository or example
```

Never include API keys, credentials or repository secrets in an issue.

---

# License

Lookup is licensed under the **GNU Affero General Public License v3.0 or later — AGPL-3.0-or-later**.

See [LICENSE](LICENSE) for the complete license text.

---

<div align="center">

<img src="./docs/assets/lookup-icon.svg" alt="Lookup" width="38">

Built for terminals, repositories and CI pipelines.

</div>
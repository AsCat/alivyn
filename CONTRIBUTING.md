# Contributing to Alivyn

Thank you for helping build Alivyn. English and Chinese are both welcome in issues and discussions.

## Current development stage

This repository is a foundation prototype. The [v0.1 specification](docs/spec-v0.1.md), [architecture](docs/architecture.md), and [scenarios](scenarios/README.md) describe the planned behavior. Please distinguish implemented behavior from proposals in documentation and pull requests.

## Local workflow

1. Install Go 1.26 or allow automatic toolchain download.
2. Run `go test ./...` and `go vet ./...`. For TUI changes, also run `bash scripts/tui-smoke.sh` in a local terminal.
3. Format Go changes with `gofmt`.
4. For changes to contact policy, memory, or scheduling, include a replayable scenario that shows the external behavior and protects the user's contact preferences.

Keep changes focused. Explain the user-visible behavior, how you verified it, and any migration impact. Do not include model API keys, chat transcripts, or other private user data in issues or tests.

## Project principles

- The user controls proactive contact and stored memory.
- The same AI individual continues across model and channel changes.
- Public material clearly identifies the individual as AI.
- Core behavior remains available in the open-source project.

Please read the [Code of Conduct](CODE_OF_CONDUCT.md) and [Security Policy](SECURITY.md) before contributing.

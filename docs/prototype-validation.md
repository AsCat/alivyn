# Foundation prototype validation

Recorded on 2026-10-23. This is a local technology probe, not a product release.

| Path | Result | What it establishes |
| --- | --- | --- |
| `go test ./...` | Passed | Mock OpenAI-compatible request and response, credential redaction on provider errors, and three scenario fixture consistency checks. |
| `go vet ./...` | Passed | No current Go static-analysis findings. |
| `go run ./cmd/alivyn version` | Passed on macOS arm64 | CLI starts under Go 1.26. |
| `go run ./cmd/alivyn doctor` | Passed on macOS arm64 | Pure-Go SQLite can create, insert, and read a temporary database. |
| `go run ./cmd/alivyn demo` | Passed in a local terminal | Bubble Tea v2 can render a basic interface and quit with `q`. |
| `CGO_ENABLED=0` cross-builds | Passed for macOS amd64, Linux amd64, and Linux arm64 | The prototype compiles without CGo for these targets; execution was verified only on macOS arm64. |
| `CGO_ENABLED=0 go build` | Passed on macOS arm64; binary approximately 11 MB | The prototype can be packaged as one executable. |

The compatible model test uses a local mock server. A real DeepSeek account, Telegram bot, Feishu application, background service, persistent memory, and contact policy have not yet been exercised. The fixture tests validate the *input specification*, not the expected behavior of an agent runtime.

The current module path is `alivyn` because the GitHub organization has not been reserved. Change it to the final repository path before a public release.

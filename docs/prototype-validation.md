# Foundation prototype validation

Recorded on 2026-10-23. These are local preview checks, not a stable product release.

| Path | Result | What it establishes |
| --- | --- | --- |
| `go test ./...` | Passed | Mock OpenAI-compatible request and response, credential redaction on provider errors, persistent conversation across chat runs, private local file permissions, and three scenario decision replays. |
| `go vet ./...` | Passed | No current Go static-analysis findings. |
| `go run ./cmd/alivyn version` | Passed on macOS arm64 | CLI starts under Go 1.26. |
| `go run ./cmd/alivyn doctor` | Passed on macOS arm64 | Pure-Go SQLite can create, insert, and read a temporary database. |
| `go run ./cmd/alivyn tui` | Passed in a local macOS terminal | The continuous conversation renders and exits with Esc. Unit tests cover saved history after reopening and visible model errors. |
| `bash scripts/tui-smoke.sh` | Passed on macOS arm64 | Starts the actual TUI under a pseudo terminal and verifies the AI identity header; the same check runs in macOS and Ubuntu CI. |
| `go run ./cmd/alivyn demo` | Passed in a local terminal | Bubble Tea v2 can render a basic interface and quit with `q`. |
| `CGO_ENABLED=0` cross-builds | Passed for macOS amd64, Linux amd64, and Linux arm64 | The prototype compiles without CGo for these targets; execution was verified only on macOS arm64. |
| `CGO_ENABLED=0 go build` | Passed on macOS arm64; binary approximately 11 MB | The prototype can be packaged as one executable. |

The compatible model test uses a local mock server. Replay tests parse supported interview phrases and run deterministic policy decisions, but do not deliver messages. A real DeepSeek account, Telegram bot, Feishu application, or background service has not yet been exercised. Conversation history and source-linked interview memory persist locally; broader memory extraction remains pending.

The module path is `github.com/AsCat/alivyn` to match the intended personal repository.

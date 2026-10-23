# Alivyn · 意续

**An open-source AI presence that remembers, grows, and reaches out.**

Alivyn is a self-hosted project for one continuous AI individual. It is designed to remember shared experiences, maintain a consistent personality, notice the passage of time, and decide when a message is welcome. Users bring their own model provider and choose how Alivyn can contact them.

> **Status: local preview.** The current repository can create one AI identity, configure a DeepSeek or custom compatible model, keep a continuous local TUI conversation, and replay contact-policy scenarios. Autonomous background contact and messaging bots are still being built. Follow the [roadmap](docs/roadmap.md) for the implementation sequence.

[简体中文](README.zh-CN.md) · [Vision](docs/vision.md) · [v0.1 specification](docs/spec-v0.1.md) · [Architecture](docs/architecture.md) · [Roadmap](docs/roadmap.md) · [Prototype checks](docs/prototype-validation.md)

## The experience we are building

You mention an interview on Wednesday. Later you move it to Friday. Alivyn remembers the change, stays quiet on Wednesday, and checks in on Friday if the time and your contact preferences allow it. When your laptop sleeps and wakes, Alivyn can recognize the gap in its own timeline without sending the same greeting every time.

The first release will provide a terminal interface, a persistent local runtime, configurable model providers, memory with correction and deletion, and Telegram and Feishu connections. A visual space and voice calls are planned for a later phase.

Alivyn is AI. Its personality, mood, and continuity are product models and experiences; the project does not claim consciousness or human feelings.

## Try the local preview

Requires Go 1.26 or Go's automatic toolchain download. `init` stores your individual and model settings locally; it never asks for or stores an API key.

```sh
git clone https://github.com/AsCat/alivyn.git
cd alivyn
go run ./cmd/alivyn init
printf 'API key: '
read -rs ALIVYN_API_KEY
printf '\n'
export ALIVYN_API_KEY
go run ./cmd/alivyn model check
go run ./cmd/alivyn
```

Choose `deepseek` to use the current `deepseek-flash` preset, or `custom` to enter an OpenAI-compatible base URL and model ID. The preset follows [DeepSeek's current model guide](https://api-docs.deepseek.com/quick_start/pricing/). Remote custom endpoints must use HTTPS; plain HTTP is allowed only for a model server on the same machine. The key is read from `ALIVYN_API_KEY` on each run and is never written to the config or database. On macOS, local data is under the user config directory; set `ALIVYN_HOME` to choose another location. Your provider receives the conversation context needed to answer.

The default command opens one continuous TUI conversation. Press Enter to send, PgUp/PgDn to read history, and Esc to leave. `chat` remains a plain terminal fallback; both interfaces use the same local history. `history` prints stored messages, `doctor` checks SQLite, and `demo` remains a rendering probe. None of these commands starts the background agent runtime.

Replay a contact-decision story without a model key or channel account:

```sh
go run ./cmd/alivyn replay scenarios/interview-rescheduled.json
go run ./cmd/alivyn replay scenarios/wake-without-spam.json
go run ./cmd/alivyn replay scenarios/pause-stops-outbound.json
```

The replay makes deterministic decisions from normalized events in the fixtures. It does not yet extract events from free-form conversation or send messages.

## How it is being built

- **One identity across channels:** a user-created individual has one history rather than a separate persona per chat app.
- **Three loops:** responses, internal state updates, and outbound contact each have explicit boundaries.
- **An inspectable decision:** every proposed message can be scheduled, reconsidered, canceled, or left unsent.
- **User control:** pause, quiet hours, frequency limits, memory correction, export, and deletion take priority over generated text.
- **Reproducible behavior:** the [scenarios](scenarios/README.md) define the first three stories that the runtime must pass.

## Contributing

The codebase is at an early stage. Read [CONTRIBUTING.md](CONTRIBUTING.md) for the current workflow. The project is licensed under [Apache-2.0](LICENSE). Security reports should follow [SECURITY.md](SECURITY.md).

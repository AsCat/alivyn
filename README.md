# Alivyn · 意续

**An open-source AI presence that remembers, grows, and reaches out.**

Alivyn is a self-hosted project for one continuous AI individual. It is designed to remember shared experiences, maintain a consistent personality, notice the passage of time, and decide when a message is welcome. Users bring their own model provider and choose how Alivyn can contact them.

> **Status: local preview.** The current repository can create one AI identity, configure a DeepSeek or custom compatible model, keep a continuous local TUI conversation, remember and revise an explicitly mentioned interview, and run a persistent contact loop with a local test outbox. Real messaging channels are still being built. Follow the [roadmap](docs/roadmap.md) for the implementation sequence.

[简体中文](README.zh-CN.md) · [Vision](docs/vision.md) · [v0.1 specification](docs/spec-v0.1.md) · [Architecture](docs/architecture.md) · [Roadmap](docs/roadmap.md) · [Prototype checks](docs/prototype-validation.md)

## The experience we are building

You mention an interview on Wednesday. Later you move it to Friday. Alivyn remembers the change, stays quiet on Wednesday, and checks in on Friday if the time and your contact preferences allow it. When your laptop sleeps and wakes, Alivyn can recognize the gap in its own timeline without sending the same greeting every time.

The first release will provide a terminal interface, a persistent local runtime, configurable model providers, memory with correction and deletion, and Telegram and Feishu connections. A visual space and voice calls are planned for a later phase.

Alivyn is AI. Its personality, mood, and continuity are product models and experiences; the project does not claim consciousness or human feelings.

## Try the local preview

Requires Go 1.26 or Go's automatic toolchain download. Start with a time-advanced story using the real local runtime and a disposable database. It needs no model key and does not change your identity or conversation:

```sh
go run ./cmd/alivyn demo en
```

`init` stores your individual and model settings locally. The model key is stored separately in a private user config file with `0600` permissions.

```sh
git clone https://github.com/AsCat/alivyn.git
cd alivyn
go run ./cmd/alivyn init
printf 'API key: '
read -rs ALIVYN_API_KEY
printf '\n'
printf '%s' "$ALIVYN_API_KEY" | go run ./cmd/alivyn model key import
unset ALIVYN_API_KEY
go run ./cmd/alivyn model check
go run ./cmd/alivyn
```

Choose `deepseek` to use the current `deepseek-flash` preset, or `custom` to enter an OpenAI-compatible base URL and model ID. The preset follows [DeepSeek's current model guide](https://api-docs.deepseek.com/quick_start/pricing/). Remote custom endpoints must use HTTPS; plain HTTP is allowed only for a model server on the same machine. `ALIVYN_API_KEY` takes precedence; DeepSeek also accepts `DEEPSEEK_API_KEY`; otherwise Alivyn reads the local private `model.key` file. The key is never written to `config.json` or the database. On macOS, local data is under the user config directory; set `ALIVYN_HOME` to choose another location. Your provider receives the conversation context needed to answer.
At setup, enter the user's IANA timezone (for example `Asia/Shanghai`) when the process may run on a server in another timezone. Existing local configs without this field continue to use the machine's local timezone.

The default command opens one continuous TUI conversation. Press Enter to send, PgUp/PgDn to read history, and Esc to leave. `chat` remains a plain terminal fallback; both interfaces use the same local history. `history` prints stored messages, `doctor` checks SQLite, and `demo tui` checks terminal rendering. `demo en` uses a disposable database to show a revised memory, a quiet Wednesday, a Friday follow-up, a wake greeting, and duplicate suppression. Plain `demo` shows the same story in Chinese. None of these commands starts the background agent runtime.

The current memory extractor recognizes explicit interview statements such as “interview on Wednesday afternoon”, “interview moved to Friday”, and “cancel the interview”. It records the source message, time, and whether the fact was user-stated or model-inferred. It treats a day or part of a day as approximate, not an exact appointment. Unrecognized wording creates no memory. Inspect active facts with `go run ./cmd/alivyn memory list` or export them as JSON with `go run ./cmd/alivyn memory export`. `go run ./cmd/alivyn memory forget interview` removes the active fact and redacts interview-related text from local conversation history. This export contains active derived memory only; it is not a full account export.

Replay a contact-decision story without a model key or channel account:

```sh
go run ./cmd/alivyn replay scenarios/interview-rescheduled.json
go run ./cmd/alivyn replay scenarios/wake-without-spam.json
go run ./cmd/alivyn replay scenarios/pause-stops-outbound.json
```

The interview replay extracts its dates from the same user-message parser used by the TUI and plain chat. Replay still does not send messages, and the extractor currently covers only a narrow set of interview phrases.

## Run the local contact loop

After setup, Alivyn stores a follow-up intent when you mention a supported future interview. Run the contact loop in a separate terminal or under a process supervisor:

```sh
go run ./cmd/alivyn contact status
go run ./cmd/alivyn contact policy 22:00 08:00 2
go run ./cmd/alivyn serve
```

`serve once` processes one tick and exits. Use `contact pause` and `contact resume` to control all outbound attempts. `contact intents` shows pending and completed intentions; `contact audit` explains sent, skipped, canceled, and failed decisions; `contact outbox` shows test deliveries. Each test delivery also joins the continuous local conversation. The outbox is local SQLite data: **no chat message is sent yet**. The policy is checked again immediately before each delivery, and an intent is written at most once to the test outbox, even after a restart. A sleeping laptop cannot run the loop until it wakes; a continuously available server can keep it running.

## How it is being built

- **One identity across channels:** a user-created individual has one history rather than a separate persona per chat app.
- **Three loops:** responses, internal state updates, and outbound contact each have explicit boundaries.
- **An inspectable decision:** every proposed message can be scheduled, reconsidered, canceled, or left unsent.
- **User control:** pause, quiet hours, frequency limits, memory correction, export, and deletion take priority over generated text.
- **Reproducible behavior:** the [scenarios](scenarios/README.md) define the first three stories that the runtime must pass.

## Contributing

The codebase is at an early stage. Read [CONTRIBUTING.md](CONTRIBUTING.md) for the current workflow. The project is licensed under [Apache-2.0](LICENSE). Security reports should follow [SECURITY.md](SECURITY.md).

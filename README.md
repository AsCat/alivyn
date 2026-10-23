# Alivyn · 意续

**An open-source AI presence that remembers, grows, and reaches out.**

Alivyn is a self-hosted project for one continuous AI individual. It is designed to remember shared experiences, maintain a consistent personality, notice the passage of time, and decide when a message is welcome. Users bring their own model provider and choose how Alivyn can contact them.

> **Status: foundation prototype.** The current repository contains the product design, replay scenarios, and a small CLI technology probe. It is not yet a persistent companion or a messaging bot. Follow the [roadmap](docs/roadmap.md) for the implementation sequence.

[简体中文](README.zh-CN.md) · [Vision](docs/vision.md) · [v0.1 specification](docs/spec-v0.1.md) · [Architecture](docs/architecture.md) · [Roadmap](docs/roadmap.md) · [Prototype checks](docs/prototype-validation.md)

## The experience we are building

You mention an interview on Wednesday. Later you move it to Friday. Alivyn remembers the change, stays quiet on Wednesday, and checks in on Friday if the time and your contact preferences allow it. When your laptop sleeps and wakes, Alivyn can recognize the gap in its own timeline without sending the same greeting every time.

The first release will provide a terminal interface, a persistent local runtime, configurable model providers, memory with correction and deletion, and Telegram and Feishu connections. A visual space and voice calls are planned for a later phase.

Alivyn is AI. Its personality, mood, and continuity are product models and experiences; the project does not claim consciousness or human feelings.

## Try the foundation prototype

Requires Go 1.26 or Go's automatic toolchain download. No API key is needed for this probe.

```sh
go run ./cmd/alivyn version
go run ./cmd/alivyn doctor
go run ./cmd/alivyn demo
```

`doctor` checks local SQLite operation. `demo` opens a small Bubble Tea interface to validate terminal rendering. Neither command starts the planned agent runtime.

## How it is being built

- **One identity across channels:** a user-created individual has one history rather than a separate persona per chat app.
- **Three loops:** responses, internal state updates, and outbound contact each have explicit boundaries.
- **An inspectable decision:** every proposed message can be scheduled, reconsidered, canceled, or left unsent.
- **User control:** pause, quiet hours, frequency limits, memory correction, export, and deletion take priority over generated text.
- **Reproducible behavior:** the [scenarios](scenarios/README.md) define the first three stories that the runtime must pass.

## Contributing

The codebase is at an early stage. Read [CONTRIBUTING.md](CONTRIBUTING.md) for the current workflow. The project is licensed under [Apache-2.0](LICENSE). Security reports should follow [SECURITY.md](SECURITY.md).

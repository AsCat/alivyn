# Alivyn · 意续

**An open-source AI presence that remembers, grows, and reaches out.**

Alivyn is a self-hosted project for one continuous AI individual. It is designed to remember shared experiences, maintain a consistent personality, notice the passage of time, and decide when a message is welcome. Users bring their own model provider and choose how Alivyn can contact them.

> **Status: local preview.** A project bot has passed a real phone test for QR pairing, private text replies, and proactive delivery. The relay currently runs only on the operator's Mac; no public HTTPS relay or default onboarding URL is available yet. External-account access has not been verified. Follow the [roadmap](docs/roadmap.md) for the implementation sequence.

[简体中文](README.zh-CN.md) · [Vision](docs/vision.md) · [v0.1 specification](docs/spec-v0.1.md) · [Architecture](docs/architecture.md) · [Actor architecture proposal](docs/actor-architecture.md) · [Character model](docs/character-model.md) · [Roadmap](docs/roadmap.md) · [Prototype checks](docs/prototype-validation.md)

## The experience we are building

You mention an interview on Wednesday. Later you move it to Friday. Alivyn remembers the change, stays quiet on Wednesday, and checks in on Friday if the time and your contact preferences allow it. When your laptop sleeps and wakes, Alivyn can recognize the gap in its own timeline without sending the same greeting every time.

The first release will provide a terminal interface, a persistent local runtime, configurable model providers, memory with correction and deletion, and Telegram and Feishu connections. A visual space and voice calls are planned for a later phase.

Alivyn is AI. Its personality, mood, and continuity are product models and experiences; the project does not claim consciousness or human feelings.

## Try the local preview

Requires Go 1.26 or Go's automatic toolchain download. Start with a time-advanced story using the real local runtime and a disposable database. It needs no model key and does not change your identity or conversation:

```sh
go run ./cmd/alivyn demo en
go run ./cmd/alivyn world demo en
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

`world demo en` fast-forwards seven days in the original authored world. `world demo actor en` shows the experimental goal-driven ledger: one recording batch keeps the same object ID across repairs, source checks, and consent decisions; each event cites its causal parent. Both demos use disposable databases and need no model key. `world show` and `world timeline` display the installed individual's setting and recorded events for the active engine. The world advances in the configured timezone while the Feishu runtime is running, even without incoming messages. These are simulated character-life events, never offline experiences shared with the user. Tidehaven is enabled by default; `world set none` disables it and `world set tidehaven` enables it again. Restart a running channel after changing the pack. The actor engine uses a bounded goal policy; an open-ended world proposer remains future work.

The default voice is **natural**: direct and concise, with warmth earned through conversation. Alivyn begins as a stranger, becomes acquainted through actual exchanges, and grows more familiar across days of conversation. Repeated greetings alone do not move that relationship forward; a request for more distance takes priority. `alivyn persona show` displays the style and character background. Use `alivyn persona set natural|romantic|companion` to change the style, or `alivyn persona background set '...'` to write a short character background. A custom background also shapes the first greeting. Restart a running channel after a persona change. In Feishu, replies have a small, bounded variation in arrival time, measured from the incoming message; slow model calls do not add an extra pause. Alivyn uses local time as context but does not guess what the user is doing from the clock.

An optional **vivid temperament pack** gives the same individual more specific preferences, mixed motives and emotional carryover. It is an original Alivyn character treatment inspired by the *mechanisms* of expressive community companions, not their character, artwork or wording. It keeps the individual's name, world and memories. In `character_v2`, source-linked episodes can carry a meaningful exchange into later conversations; vivid Feishu replies can occasionally arrive in two or three short bubbles. Use `alivyn persona pack list`, then `alivyn persona pack set vivid`; `set natural` returns to the existing treatment. Restart a running channel after switching. Voice, images, phone tools and separate character identities are future work. For an isolated model audition without changing the live configuration, run `ALIVYN_AUDITION_PACK=vivid alivyn actor audition character_v2 "bad day"` and compare with `ALIVYN_AUDITION_PACK=natural`. See [experience packs](docs/experience-packs.md) for current boundaries and the capability plan.

The current memory extractor recognizes explicit interview statements and short personal disclosures such as “My name is Ari” and “I like coffee.” Personal facts keep their source, survive a long conversation, and can be corrected by a later statement. “Forget my name” or “Forget that I like coffee” removes the corresponding active fact and redacts its source messages. Interview memories retain their existing correction and `memory forget interview` behavior. `memory list` shows active facts; `memory export` returns JSON with `events` and `personal` arrays. The export contains active derived memory only, not a full account export. Longer or ambiguous statements are not automatically promoted to durable personal memory.

The experimental `character_v2` engine uses a stable character card, ordered conversation, a direct dialogue draft and an independent consistency check; a separate background worker maintains short-lived character state. See [Character dialogue v2](docs/character-dialogue.md) for the architecture, limitations and comparisons. Enable it with `alivyn actor set character_v2`, then restart the channel. `alivyn actor audition character_v2 all` runs isolated multi-turn scenarios using your configured provider/key, without sending messages. V2 also groups short Feishu message bursts, supports intentional quiet after a completed exchange, and considers sourced spontaneous contact in the background. `alivyn contact simulate` inspects that behavior in disposable histories using your configured model/key, without sending messages. The earlier `actor_v1` engine remains available for comparison. New installations still default to `legacy` during evaluation.

Replay a contact-decision story without a model key or channel account:

```sh
go run ./cmd/alivyn replay scenarios/interview-rescheduled.json
go run ./cmd/alivyn replay scenarios/wake-without-spam.json
go run ./cmd/alivyn replay scenarios/pause-stops-outbound.json
```

The interview replay extracts its dates from the same user-message parser used by the TUI and plain chat. Replay still does not send messages, and the extractor currently covers only a narrow set of interview phrases.

Send `/help` for controls. Use `/freeze`, `/wake` and `/quiet` directly; `/diagnostics` → `/diagnostics confirm` opens saved character, routine, memory and state views, and `/resume` exits. Feishu v2 supports chosen absences with durable deferred replies. See [controls and availability](docs/character-dialogue.md#mood-routine-and-availability).

Send `/reset` (or `/清除记忆`) as a separate message in Feishu or terminal chat to immediately clear local conversation, memories, relationship and simulated life progress. The initial character settings, model configuration and connection remain. The command is handled locally without a model call. See [reset scope and character choices](docs/character-dialogue.md#reset-to-the-initial-character).

## Run the local contact loop

After setup, Alivyn stores a follow-up intent when you mention a supported future interview. Run the contact loop in a separate terminal or under a process supervisor:

```sh
go run ./cmd/alivyn contact status
go run ./cmd/alivyn contact policy 22:00 08:00 2
go run ./cmd/alivyn serve
```

`serve once` processes one tick and exits. Use `contact pause` and `contact resume` to control outbound attempts. `contact intents` shows pending and completed intentions; `contact audit` explains sent, skipped, canceled, and failed decisions; `contact outbox` shows local test deliveries. `alivyn serve` writes only to the local SQLite outbox. `alivyn channel feishu run` uses the same policy to send proactive messages to the paired private chat. On the first channel attempt, the model writes a brief opening from the established character and specific reason. World updates are bound to an exact recorded event and are retried instead of sending an unrelated fallback if composition fails. The policy is rechecked after generation and before delivery. Attempts, generated text, retries, and confirmed deliveries survive restarts. A sleeping laptop cannot run the loop until it wakes; a continuously available server can keep it running.

## Feishu scan to connect (development preview)

The intended default is one user command, `alivyn channel feishu connect`: scan the **project shared bot** QR code and send the one-time pairing code in a private chat. The command keeps running after pairing to reply to messages. Users do not create a Feishu app, request permissions, or enter an App ID or secret. Codes expire after ten minutes; sending a new code from the same Feishu account transfers the connection to a new local Alivyn instance. The project bot works in the operator's Feishu account, but there is no public HTTPS relay or default relay URL yet, so the no-argument command currently reports that the service is unavailable. Developers can use `alivyn channel feishu connect <relay HTTPS URL>` with a self-hosted relay. See the [channel spike](docs/channel-spike.md) for operator setup.

Private messages pass through the shared relay to the local runtime, then to the user's model provider. The relay retains account bindings and token hashes, while incoming message bodies live only in a short-lived memory queue. **Incoming messages may be lost after five minutes offline or when the queue fills, and a crash after a successful send may cause a duplicate reply.** Local history and model credentials remain on the user's machine. Proactive messages use Feishu's one-hour UUID deduplication window; after 55 minutes of uncertain delivery, the runtime stops retrying and records a failure rather than risk a late duplicate. `alivyn channel feishu add` is an optional automated personal-bot setup, not a required user step; it has been verified with the operator's real account.

## How it is being built

- **One identity across channels:** a user-created individual has one history rather than a separate persona per chat app.
- **Three loops:** responses, internal state updates, and outbound contact each have explicit boundaries.
- **An inspectable decision:** every proposed message can be scheduled, reconsidered, canceled, or left unsent.
- **User control:** pause, quiet hours, frequency limits, memory correction, export, and deletion take priority over generated text.
- **Reproducible behavior:** the [scenarios](scenarios/README.md) define the first three stories that the runtime must pass.

## Contributing

The codebase is at an early stage. Read [CONTRIBUTING.md](CONTRIBUTING.md) for the current workflow. The project is licensed under [Apache-2.0](LICENSE). Security reports should follow [SECURITY.md](SECURITY.md).

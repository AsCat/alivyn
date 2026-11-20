# Adapting DaFeiYu mechanisms for Alivyn

Status: first affect-to-behavior slice implemented on 2026-11-21. Reference: [whyever77/dafeiyu-astrbot](https://github.com/whyever77/dafeiyu-astrbot) at `main@64274354`. The upstream runtime uses Python, AstrBot, NapCat, and QQ; Alivyn uses a Go kernel, Feishu, and capability interfaces. Preserve attribution and the MIT notice when copying code. Verify character art, voice assets, and third-party services separately.

The design goal is one sourced person state that changes dialogue, outreach, activity, and eventually voice expression. Silence is not evidence of abandonment or affection. A proposed action is not a completed action. All modules use the same provenance, contact policy, and permission receipts.

| Phase | Mechanism | Acceptance |
| --- | --- | --- |
| 1. Affect and behavior | Sourced state, elapsed-time settling, shared projection for speech, inner wake, and contact | A supported event changes later participation; transient tendencies fade; forgetting and reset remove influence; internal values stay private. |
| 2. Shared contact ledger | Coordinate ordinary outreach and activity sharing | One quiet-hours, unanswered, cadence, deduplication, and delivery-result policy. |
| 3. Perception and expression | Feishu images, audio understanding, and TTS via `perceive` and `express` | Every claim traces to an actual attachment or receipt; failed media delivery is reported accurately. |
| 4. Own activities | Goals, checkpoints, artifacts, and authorized read-only media or web activity | A goal resumes across days with sourced results and can end quietly. Device writes require scoped approval and receipts. |
| 5. Observer | Timeline, memory graph, and a small room reading the same event stream | The view and dialogue agree about time, knowledge, and activity. |

The first slice adds an optional `moment.affect` pulse to post-delivery reflection: internal valence, activation, and social pull, supported by one to three existing evidence IDs. It is not an affection score. Activation and social pull settle after two hours; the pulse expires after eight. The same moment reaches ordinary speech, inner wakes, and contact choice. Old snapshots without a pulse still work. A later slice should index affect changes as replayable events and allow actual life events to update the state without a chat turn. Judge naturalness across conversations, not a single line. Upstream features described as local, disabled by default, or awaiting real integration must be verified separately.

An isolated four-turn model audition saved sourced pulses on every turn, and replies shortened after an explicit invitation to stop finding topics. This did not send Feishu messages or establish long-term naturalness. Empty or malformed model JSON gets one bounded retry; unsupported evidence is still rejected.

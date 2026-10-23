# Replay scenarios

These JSON files describe the first external behaviors that Alivyn must reproduce. They are **specification fixtures** today; the scenario runner is scheduled for the first runtime milestone. Times include UTC offsets so a replay is deterministic. `source_text` preserves what the user said, while normalized fields describe the event that the model or parser should extract.

`go test ./scenarios` checks that fixtures have valid timestamps, chronological events, contact limits, and expectations attached to clock ticks. It does not run an agent or prove those expectations yet.

Each fixture lists input events and expected outcomes. The future runner will advance a controllable clock, feed these events to the runtime, and compare the resulting contact decisions and deliveries with `expect`.

1. `interview-rescheduled.json`: an outdated follow-up must be canceled.
2. `wake-without-spam.json`: a long gap can produce one contextual greeting; repeated wake events must not.
3. `pause-stops-outbound.json`: pause prevents delivery even if an intent was already due.

The fixtures are deliberately small so both contributors and users can understand why the agent contacted someone or stayed quiet.

# Replay scenarios

These JSON files describe the first external behaviors that Alivyn must reproduce. The `alivyn replay <file>` command now runs a deterministic local policy simulation and checks every expected decision. It does not call a model, parse `source_text`, or deliver any real message. Times include UTC offsets. `source_text` preserves what the user said, while normalized fields describe the event that a future model or parser should extract.

`go test ./scenarios` checks fixture structure and time consistency. `go test ./internal/replay` runs the decision simulation and verifies its expected outcomes. Neither test proves that a future model can extract the normalized events from user speech.

Each fixture lists input events and expected outcomes. The runner advances through those event timestamps and compares simulated decisions and delivery counts with `expect`.

1. `interview-rescheduled.json`: an outdated follow-up must be canceled.
2. `wake-without-spam.json`: a long gap can produce one contextual greeting; repeated wake events must not.
3. `pause-stops-outbound.json`: pause prevents delivery even if an intent was already due.

The fixtures are deliberately small so both contributors and users can understand why the agent contacted someone or stayed quiet.

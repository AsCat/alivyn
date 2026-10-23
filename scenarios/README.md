# Replay scenarios

These JSON files describe the first external behaviors that Alivyn must reproduce. The `alivyn replay <file>` command runs a deterministic local policy simulation and checks every expected decision. Interview `user_message` events are parsed from `source_text` by the same conservative extractor used in live chat. It does not call a model or deliver any real message. Times include UTC offsets.

`go test ./scenarios` checks fixture structure and time consistency. `go test ./internal/replay` runs the decision simulation and verifies its expected outcomes. These tests cover the supported interview phrases, not arbitrary natural language.

Each fixture lists input events and expected outcomes. The runner advances through those event timestamps and compares simulated decisions and delivery counts with `expect`.

1. `interview-rescheduled.json`: an outdated follow-up must be canceled.
2. `wake-without-spam.json`: a long gap can produce one contextual greeting; repeated wake events must not.
3. `pause-stops-outbound.json`: pause prevents delivery even if an intent was already due.

The fixtures are deliberately small so both contributors and users can understand why the agent contacted someone or stayed quiet.

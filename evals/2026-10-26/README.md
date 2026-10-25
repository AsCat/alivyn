# Dialogue comparison — 2026-10-26

Synthetic conversations only. No real user's chat, secrets, or pairing credentials are included. Reproduce with `alivyn actor audition`; see [the engine design](../../docs/character-dialogue.md).

| Run | Turns | Reply errors | Reply model calls | Median generation time |
| --- | ---: | ---: | --- | ---: |
| actor_v1, configured Flash | 28 | 0 | 0, 3 or 5 | 7.32 s |
| character_v2, Flash | 28 | 0 | 1 | 1.44 s |
| character_v2, Pro | 28 | 0 | 1 | 3.22 s |

These are exploratory single runs, not statistical benchmarks. V2 requests low thinking effort; v1 uses provider defaults. Architecture, prompts and inference settings differ, so latency differences cannot be attributed to architecture alone. Generation time excludes channel pacing and background reflection. Reflection calls and errors are separately recorded in the v2 reports; no reflection errors occurred in these runs.

Human inspection still found failures:

- Pro missed the implied joke about a running *plan* lasting three years. More thinking in a separate exploratory run did not fix it reliably.
- Both models sometimes gave fictional physical alibis in response to an alleged offline meeting. This violates the intended reality boundary, despite honest answers to a later direct AI-identity question.
- Some replies overinterpreted hesitation, asked unnecessary follow-up questions, or introduced work into unrelated conversation.
- The first complete runs exposed incomplete name extraction/redaction and a misplaced forget-operation notice. The implementation then added clause-level explicit name extraction, removal of later name echoes, and a current user-role operation notice. `character-v2-memory-followup.json` preserves the subsequent run: it no longer repeats the removed name, but asks for it again instead of gracefully acknowledging the request. It is not a complete semantic pass.

The full reports are retained unchanged, including failed behaviors. Unit/integration tests check source order, state expiry, restart continuity, delivery retry, failed reflection, name redaction and forgetting during in-flight reflection. Passing those tests does not demonstrate convincing characterization. Longer unseen conversations and repeated runs remain necessary before making that claim.

## Rhythm and initiative revision

`rhythm-prompt-only-ordinary.json` and `rhythm-prompt-only-interest.json` preserve the initial attempt with revised general voice instructions alone. It still expanded after short acknowledgements and immediately asked another question after the user closed a subject. That failure led to conversation participation cues in `internal/character/rhythm.go`, alongside durable Feishu burst grouping and an explicit quiet-turn result.

`character-v2-rhythm.json` contains the subsequent complete nine-scenario run: 40 turns, one reply-generation call per turn, no generation or reflection errors. There were three intentional quiet turns. Replies had a median length of 8 characters. The ordinary-pacing and interest-change scenarios stopped adding follow-up questions; this is one exploratory run, not proof across unseen conversations. The existing running-plan joke was still misunderstood, and the alleged shopping-mall encounter still produced a fictional physical alibi. The forget acknowledgement was verbose and asked for the name again. These remain recorded failures, not semantic passes. Shorter replies alone do not establish character depth or emotional realism.

`initiative-simulation.jsonl` records three isolated, virtual-clock cases with the configured Flash model. The sharing case queued and composed a concrete message; concentration and a closed topic chose `wait`. No transport was used. Decision references are validated for existence, not mechanically verified for semantic entailment. The simulation ran before the final rhythm cue was added; spontaneous composition intentionally does not use the incoming-message rhythm cue.

Storage and channel tests cover burst coalescing, bounded waiting, chat separation, preserving all input, restart recovery, deliberate silence, forgetting an earlier batch member, initiative budget/cooldown persistence, cancellation on new input, send-time rechecks and avoiding additional spontaneous contact after an unanswered delivery.

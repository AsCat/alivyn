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

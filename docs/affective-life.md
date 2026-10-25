# Affective life and behavioral continuity (RFC 03)

Status: partially implemented. `actor_v1` now passes the chosen focus, the character's own desire and feeling, ordered recent dialogue, and sourced unfinished threads into expression. Short-lived continuity expires or is removed on forgetting. New world events arising from a topic the user actually discussed can create one proactive contact candidate, subject to the existing contact policy. Evolving long-term views and general device capabilities remain design goals.

## Why this layer exists

Alivyn persists memories, relationship stages, Tidehaven events, and a decision before language generation. Familiarity still mostly advances with meaningful turns and shared days. There is no durable account of how an event changes the character's outlook and later choices. A reply can therefore be factually sound yet sound like a scripted check-in.

Related projects show a useful principle: [AstrBot's group-chat enhancement plugin](https://github.com/Him666233/astrbot_plugin_group_chat_plus) separates reply decisions, context, mood hints, proactive messages, and typing delay; [better-dsh-pet](https://github.com/ysppwn721/better-dsh-pet) maps mood to idle movement and speech bubbles. Alivyn borrows the principle that **internal state should change behavior**, without copying these projects' code or equating turn counts with affection.

## Model

```text
Sourced event + elapsed time
    → appraisal against values, goals, and relationship
    → projection: traits / attention / affect / view of this person / open loops
    → action choice: answer, share, ask, disagree, repair, or stay quiet
    → language realization → delivery outcome and feedback
```

| State | Lifetime | Role |
| --- | --- | --- |
| `Traits` | Character-version scale | Values, sensitivities, and conflict habits; one exchange cannot rewrite them. |
| `Drives` | Across days | Own goals, such as resolving the recording permission question. |
| `RelationalView` | Across turns, revisable | Knowledge, trust, reservations, explicit boundaries, and shared experiences; separate from turn-count stages. |
| `AffectiveEpisode` | Minutes to days | Event-caused curiosity, ease, hesitation, irritation, relief, and similar tendencies. Episodes can coexist and carry intensity, evidence, and expiry. |
| `OpenLoop` | Until resolved or expired | Unanswered questions, the character's mistakes, and unresolved world conflicts. |

Affective state is a **character interpretation of recorded events**, not evidence of subjective experience. User feelings remain uncertain unless expressed or grounded in specific context. Silence, repeated greetings, and response gaps do not automatically mean rejection. Time may return short-lived tendencies toward baseline; it must not invent resentment or intimacy.

## Event and update contract

Each state change writes a replayable, brief appraisal record with `event_id`, provenance, relevant observation, affected value or goal, state delta, confidence, expiry, and correction source. A model may propose an appraisal; the runtime validates the event, provenance, scope, and expiry before commit. An appraisal cannot create a physical-world fact or a Tidehaven event. Corrections, forgetting, and explicit user boundaries invalidate related inferences, unsent contact candidates, and generation context.

An emotion label is insufficient. A state must have an observable behavioral consequence: whether to mention one's own work, ask a question, disagree, shorten the reply, or initiate contact at a permitted time. Language generation receives only relevant state and evidence for the chosen act. Internal scores and diagnostics do not leak into ordinary chat.

## Three cross-turn examples

1. **Misread and repair:** Alivyn treats an ordinary greeting as a request for help; the user says the answer made no sense. Record her mistaken assumption, repair it briefly, and reduce unsupported motive guesses in later turns. Do not label the user angry without evidence.
2. **Life of her own:** A recorded disagreement with Lin Chen about recording rights makes Alivyn more careful about claims. When the user discusses promises, she may express a considered view. A bare greeting does not require an archive update.
3. **Time away:** After the computer wakes, Alivyn knows there was a gap. An unresolved thread may still matter while short-lived affect decays. She does not claim to have watched the user's life or become hurt by their absence.

All three use the same appraisal and decision path, without special reply templates for input phrases.

## Implementation and acceptance

1. Add versioned appraisal events and a replayable projection alongside the existing ledger, with restart recovery and event deduplication. Use existing relationship stages as input without inventing retrospective feelings.
2. `actor.Frame` now carries recent dialogue and sourced unfinished threads. `Decision` records action, focus, the character's own desire, and feeling. Realization receives those short cues and ordered dialogue, while factual claims still require selected evidence. Durable, replayable long-term view changes remain open work.
3. The world loop now creates a contact candidate when a real shared topic receives a new event. It skips strangers, catch-up events, and explicit distance requests, and cancels the candidate when the user resumes the conversation. Pausing, quiet hours, rate limits, and reality boundaries still govern delivery. “Missing” or “worrying about” a user cannot override contact policy.
4. Evaluate full arcs: repeated greetings, a character mistake, user correction, disagreement, a world event, days offline, changed boundaries, and a model swap. Verify sourced transitions, changed later choices, and expiry. Use human blind review for naturalness and identity consistency. Roll out to the paired Feishu instance only after acceptance, with a rollback path.

The acceptance test is whether we can explain why she chose an action and observe a durable difference afterward. A nicer single line, random delay, expressive animation, or a larger prompt does not complete this layer.

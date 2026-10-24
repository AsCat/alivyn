# Character and relationship model

Alivyn should feel consistent because earlier interactions change later choices. Delayed replies and colorful prose can support that effect, but they cannot create it by themselves.

The creators of *Westworld* described hosts through recurring daily loops, distinct character journeys, and stories that guests could interrupt ([WIRED interview](https://www.wired.com/2016/09/couple-behind-hbos-westworld-want-know-wrong-us/), [The New Yorker interview](https://www.newyorker.com/culture/persons-of-interest/on-the-ranch-with-the-creators-of-westworld)). Alivyn borrows the design question: what does this individual usually notice and do, and what changes when a real person enters their life? It does not copy any character or claim consciousness.

## Current implementation

The first world pack is **Tidehaven / 汐湾**, a fictional coastal city. The individual restores recordings at the Old Harbor sound archive. Lin Chen, a colleague, wants to publish a recovered interview sooner than the individual thinks is responsible. The unresolved permission question gives the character a continuing goal and a reason for disagreement outside the user's needs.

Each local day advances a durable world state and appends an event to SQLite. The original engine uses a seven-day authored arc. The experimental `actor_v1` engine uses a bounded goal policy: actions require prerequisites, every recording batch keeps one object ID, and an event points to its causal parent. State and event commit together, so restarting on the same day cannot create a duplicate. A conversation changes the experimental world's next action only when its accepted reply actually used an event from the current batch; unrelated user words, unused planner evidence, and rejected drafts do not enter the world diary. The background runtime advances the active engine even if no one sends a message. A long offline gap is capped at seven simulated days of catch-up; the system does not claim to have witnessed real external events during sleep.

`alivyn world demo [en|zh-CN]` shows the original seven days; `alivyn world demo actor [en|zh-CN]` shows ten goal-driven days and their causal links. Both use isolated databases and no model key. `alivyn world show` and `alivyn world timeline` inspect the installed character's active world ledger. These commands show fictional events, not hidden model reasoning. When first enabled, `actor_v1` imports earlier authored events rather than resetting the character's past. The model receives canon and recorded events as grounding; it must answer truthfully if asked whether the setting is fictional or the individual is AI.

This is a **starter world pack**. The experimental goal policy validates action prerequisites, time, object continuity, and provenance, but it still has a small set of authored actions. It is not an open-ended simulated society. The next milestone is a replaceable, versioned pack and model-suggested event candidates that pass the same validation before commitment.

- **Character background:** one locally configured paragraph establishes social instincts and values. The default individual is slow to trust and dislikes empty promises. A custom background reaches the model even on the first greeting.
- **Relationship distance:** a persisted state starts at `stranger`, then moves through `acquaintance`, `familiar`, and `close` as meaningful conversations happen across days. Greetings alone do not promote it. An explicit request for distance limits the stage.
- **Continuity:** source-linked personal memories and the recorded fictional world timeline can affect later replies. The model may refer to authored in-world events and user-shared facts, while keeping those sources distinct.
- **Rhythm:** Feishu replies have a short, bounded timing variation measured from message arrival. A slow model call absorbs that time. This is delivery pacing, not a claim that the individual was busy doing something else.
- **Voice review:** common assistant phrases and unsupported guesses about the user's feelings trigger one rewrite attempt.
- **First-contact social beats:** the default character has a few authored responses for recognition tests such as “It's me” and false claims of an offline meeting. They read the actual recent dialogue and are limited to early acquaintance. This prevents the model from fabricating recognition or answering a social tease with a technical status report. Custom character backgrounds continue through model generation.

## Next character milestone

1. Move the starter canon into a versioned, editable world pack with stable values, habits, relationships, places, and conversational boundaries. Existing timelines must retain their pack version.
2. Record momentary mood as a time-bounded interpretation of sourced events. Mood may influence tone, but should never be presented as a human bodily feeling or invented incident.
3. Connect each proactive impulse to a concrete memory, user preference, or existing conversation. Record why it was sent or suppressed.
4. Evaluate conversation arcs, not just single replies: first contact, first disclosure, a boundary request, correction, forgetting, days apart, and a resumed conversation. A response should respect what the character knows at that point.

These are product models of behavior. Alivyn must answer honestly when asked whether it is AI.

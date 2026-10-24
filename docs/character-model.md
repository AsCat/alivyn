# Character and relationship model

Alivyn should feel consistent because earlier interactions change later choices. Delayed replies and colorful prose can support that effect, but they cannot create it by themselves.

The creators of *Westworld* described hosts through recurring daily loops, distinct character journeys, and stories that guests could interrupt ([WIRED interview](https://www.wired.com/2016/09/couple-behind-hbos-westworld-want-know-wrong-us/), [The New Yorker interview](https://www.newyorker.com/culture/persons-of-interest/on-the-ranch-with-the-creators-of-westworld)). Alivyn borrows the design question: what does this individual usually notice and do, and what changes when a real person enters their life? It does not copy any character or claim consciousness.

## Current implementation

The first world pack is **Tidehaven / 汐湾**, a fictional coastal city. The individual restores recordings at the Old Harbor sound archive. Lin Chen, a colleague, wants to publish a recovered interview sooner than the individual thinks is responsible. The unresolved permission question gives the character a continuing goal and a reason for disagreement outside the user's needs.

Each local day advances a durable world state and appends an authored event to SQLite. The event and progress update commit together; restarting on the same day cannot produce another event. The first seven days form a coherent arc. A meaningful user conversation can change the next day's choice without copying the user's private words into a world diary. TUI, plain chat, Feishu replies, and proactive composition read the same world timeline. The background runtime advances it even if no one sends a message. A long offline gap is capped at seven simulated days of catch-up; the system does not claim to have witnessed real external events during sleep.

`alivyn world demo [en|zh-CN]` shows the seven days in an isolated database, without a model key. `alivyn world show` and `alivyn world timeline` inspect the installed character's setting and recent events. These commands show the fictional story ledger, not hidden model reasoning. The model receives the canon and recorded events as grounding, with an instruction to be truthful if directly asked whether the world is fictional or the individual is AI. It may describe life in the setting naturally, but may not imply an offline meeting with the user.

This is a **starter world pack**, with authored daily events and one interaction-sensitive branch. It establishes persistence and truthful provenance; it is not yet an open-ended simulated society or an autonomous planner. The next milestone is a replaceable world pack format and a bounded event planner whose proposed events pass canon, time, and provenance checks before commitment.

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

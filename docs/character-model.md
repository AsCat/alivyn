# Character and relationship model

Alivyn should feel consistent because earlier interactions change later choices. Delayed replies and colorful prose can support that effect, but they cannot create it by themselves.

The creators of *Westworld* described hosts through recurring daily loops, distinct character journeys, and stories that guests could interrupt ([WIRED interview](https://www.wired.com/2016/09/couple-behind-hbos-westworld-want-know-wrong-us/), [The New Yorker interview](https://www.newyorker.com/culture/persons-of-interest/on-the-ranch-with-the-creators-of-westworld)). Alivyn borrows the design question: what does this individual usually notice and do, and what changes when a real person enters their life? It does not copy any character or claim consciousness.

## Current implementation

- **Character background:** one locally configured paragraph establishes social instincts and values. The default individual is slow to trust and dislikes empty promises. A custom background reaches the model even on the first greeting.
- **Relationship distance:** a persisted state starts at `stranger`, then moves through `acquaintance`, `familiar`, and `close` as meaningful conversations happen across days. Greetings alone do not promote it. An explicit request for distance limits the stage.
- **Continuity:** source-linked personal and event memories can affect later replies. The model may only refer to events the user actually shared; it cannot invent a life outside the conversation.
- **Rhythm:** Feishu replies have a short, bounded timing variation measured from message arrival. A slow model call absorbs that time. This is delivery pacing, not a claim that the individual was busy doing something else.
- **Voice review:** common assistant phrases and unsupported guesses about the user's feelings trigger one rewrite attempt.
- **First-contact social beats:** the default character has a few authored responses for recognition tests such as “It's me” and false claims of an offline meeting. They read the actual recent dialogue and are limited to early acquaintance. This prevents the model from fabricating recognition or answering a social tease with a technical status report. Custom character backgrounds continue through model generation.

## Next character milestone

1. Define a small character card with stable values, habits, conversational boundaries, and one or two recurring interests. The card should be editable without exposing model internals to normal chat.
2. Record momentary mood as a time-bounded interpretation of sourced events. Mood may influence tone, but should never be presented as a human bodily feeling or invented incident.
3. Connect each proactive impulse to a concrete memory, user preference, or existing conversation. Record why it was sent or suppressed.
4. Evaluate conversation arcs, not just single replies: first contact, first disclosure, a boundary request, correction, forgetting, days apart, and a resumed conversation. A response should respect what the character knows at that point.

These are product models of behavior. Alivyn must answer honestly when asked whether it is AI.

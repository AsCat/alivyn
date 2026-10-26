# Alivyn experience packs

Status: the first temperament and episode loop works; perception, device actions, and separate character instances remain future work.

`persona pack` changes how **the same Alivyn individual** responds. `natural` preserves the existing treatment. `vivid` adds more specific preferences, conflicting motives, and emotional carryover. Switching keeps the name, fictional world, conversation history, and contact limits. It does not create another person or reinterpret their past.

```sh
alivyn persona pack list
alivyn persona pack set vivid
alivyn persona pack show
```

Restart a running channel to apply the setting. After a delivered `character_v2` exchange, the background worker may record one meaningful, source-linked episode: what happened, how the character responded, and an interpretation that can later change. Up to eight recent episodes return to the model. Routine greetings need no episode. Forgetting a personal fact or resetting the character clears derived episodes along with their source context. These notes are narrative memory, not raw model reasoning.

The vivid pack may split one `character_v2` Feishu reply into two or three short bubbles. It remains one logical turn in local history. Each bubble has a durable send receipt, so a known successful first bubble is not resent when a later bubble fails. A crash between a successful send and its local receipt can still duplicate the last bubble because this path has no bubble-level platform idempotency key.

Compare the packs in an isolated model audition without changing live configuration or sending Feishu messages:

```sh
ALIVYN_AUDITION_PACK=natural alivyn actor audition character_v2 "bad day"
ALIVYN_AUDITION_PACK=vivid   alivyn actor audition character_v2 "bad day"
```

Next, separate character instances will need distinct IDs for configuration, world, memories, relationship, and channel bindings. Multimodal input/output can then attach to channel adapters; authorized web and virtual-phone tools can write sourced observations into the life ledger. A future desktop companion can render the same engine state. Alivyn borrows technical patterns from expressive community companions; it does not distribute their character art or wording. Their [persona license](https://github.com/linsport/dsh-whale-persona/blob/main/LICENSE) and [third-party asset notices](https://github.com/SAKURAfan1023/dafeiyu-bot/blob/main/THIRD_PARTY_NOTICES.md) distinguish code from visual-character rights.

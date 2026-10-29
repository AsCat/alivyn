# Alivyn experience packs

Status: the first temperament and episode loop and a public-source reading loop work; voice, device actions, and separate character instances remain future work.

`natural` and `vivid` change how **the same Alivyn individual** responds. `natural` preserves the existing treatment. `vivid` adds more specific preferences, conflicting motives, and emotional carryover. Switching between these keeps the name, fictional world, conversation history, and contact limits.

`dafeiyu-astrbot` is a separate guest character using an upstream AstrBot v2.1 persona prompt. It requires a separate `ALIVYN_HOME`; switching an existing history between Alivyn and the guest is rejected. This is a text persona trial, not the full upstream bot or its phone/media tools. See [the trial provenance and limits](dafeiyu-trial.md).

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

## Her own reading time

With `character_v2`, original Alivyn chooses one public feed excerpt during the local 10:00–21:00 window each day. Candidates come from BBC science, BBC arts, and NASA RSS feeds, up to four per source. The character picks according to her identity and recent reading history. The source title, excerpt, and URL are stored separately from her tentative private reaction. She has read an excerpt, not the full article. Feed or model failures retry later without fabricating an experience; this worker does not block direct chat replies.

A later conversation can refer to the reading. If there was real conversation in the preceding 72 hours and the relationship has moved beyond first acquaintance, the new reading may also queue a proactive message after three minutes. The usual pause, quiet hours, daily limit, pending reply, and fresh user message checks still apply. The character can choose not to send. The standalone guest persona does not share this life history.

```sh
alivyn life status
alivyn life explore
```

The latter attempts today's reading immediately, but does not bypass the once-per-day rule. `/reset` clears these experiences and unsent contacts. Public feed publishers receive no personal chat history. This slice is text only; voice and singing belong to a later audio adapter.

Next, separate character instances will need distinct IDs for configuration, world, memories, relationship, and channel bindings. Multimodal input/output can then attach to channel adapters; authorized web and virtual-phone tools can write sourced observations into the life ledger. A future desktop companion can render the same engine state. Alivyn borrows technical patterns from expressive community companions; it does not distribute their character art or wording. Their [persona license](https://github.com/linsport/dsh-whale-persona/blob/main/LICENSE) and [third-party asset notices](https://github.com/SAKURAfan1023/dafeiyu-bot/blob/main/THIRD_PARTY_NOTICES.md) distinguish code from visual-character rights.

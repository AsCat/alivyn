# Messaging channel integration spike

Reviewed on 2026-10-24 against provider documentation. These are integration choices, not tested account connections.

## Telegram

Use a BotFather-created bot, with `getUpdates` long polling for a local daemon and `sendMessage` for outbound contact. Long polling avoids requiring a public callback address. The user must [contact the bot first](https://core.telegram.org/bots/tutorial); onboarding should guide the user to press Start, capture the resulting chat ID, then send a confirmation message. The [Bot API](https://core.telegram.org/bots/api) and [FAQ](https://core.telegram.org/bots/faq) describe update offsets and the incompatibility of long polling with an active webhook. The wizard should detect an existing webhook and offer an explicit fix rather than silently replacing it.

## Feishu

Use an **application bot** for a one-to-one companion. A [custom bot](https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot) only posts into its current group and cannot handle the intended private conversation. The [bot overview](https://open.feishu.cn/document/client-docs/bot-v3/bot-overview) covers receive and send capabilities; message APIs require the right application scopes and recipient ID.

### Shared bot: proposed quick pairing

Feishu supports [sharing an application bot by AppLink or QR code](https://open.feishu.cn/document/develop-robots/add-bot-to-external-group?lang=zh-CN). With external one-to-one sharing enabled and first-use review disabled by its owner, an external user can scan, confirm the app information, and open the bot chat. This is the proposed default for an easy first experience: Alivyn shows a one-time pairing code in the terminal; the user scans the shared bot QR and sends the code in the chat; the service links that Feishu user to the correct local Alivyn instance and sends a test reply. The user creates no Feishu app and handles no App ID, App Secret, or `open_id`.

The project must operate the shared bot and a relay for this mode. Its owner must enable external sharing and complete the required organizational or mainland-China personal identity verification. The user must open the bot chat first; scanning alone does not authorize unsolicited messages. Feishu then emits a first-chat or chat-entry event containing the user's `open_id`, after which the bot can send messages. A shared relay will necessarily handle message content in transit, so document that path clearly, avoid message-content logs and persistent relay transcripts, and keep model credentials and durable memory in the user's own runtime. The QR path needs an account-level trial before it can be described as available.

### Self-owned bot: fully self-hosted pairing

Personal users are a viable starting point. Feishu's [account guide](https://www.feishu.cn/hc/zh-CN/articles/985882202335-%E7%AC%AC%E4%B8%80%E6%AD%A5-%E5%88%9B%E5%BB%BA%E4%BC%81%E4%B8%9A%E4%B8%8E%E9%82%80%E8%AF%B7%E6%88%90%E5%91%98) says an individual can create a free organization. The [platform changelog](https://open.feishu.cn/changelog?abilityType=Tool&lang=zh-CN) records review-free publishing and immediate permission grants for personal users' self-built apps. A bot used by its creator within the same organization does not need external sharing. [External-user conversations](https://open.feishu.cn/document/develop-robots/add-bot-to-external-group?lang=zh-CN) are a separate capability with extra confirmation and identity requirements.

Make the first Feishu path a guided personal setup: open the developer console; create a self-built app; enable the application bot; request only private-message receive and bot-send permissions; choose WebSocket events; publish to the creator's own availability scope; enter App ID and App Secret through Alivyn's local secret prompt. The CLI should then wait for the creator to send one message to the bot, capture the recipient ID from that event, reply with a test message, and confirm proactive delivery. Users should never have to copy an `open_id` or configure a public callback URL. The [WebSocket event mode](https://open.feishu.cn/document/ukTMukTMukTM/uYDNxYjL2QTM24iN0EjN/event-subscription-configure-) works with outbound internet access and no public IP. Its three-second event-processing limit means the handler should persist and acknowledge events quickly, then run model work asynchronously. Console steps still require the user to act in their own Feishu account; test the exact screens before claiming a fixed setup time.

## Next validation

1. Create a Feishu bot under a project-controlled account; test external sharing, its QR code, first-use confirmation, and first-chat events with a separate personal account.
2. Build a minimal shared relay and one-time pairing flow; verify that it routes each Feishu account to only its paired Alivyn runtime and does not persist message content.
3. Create a fresh personal Feishu organization and self-built bot; complete `alivyn channel add feishu` with local secret storage as the independent self-hosted path.
4. Verify real one-to-one receiving, replying, and proactive contact on a phone for both paths, including pause, quiet hours, and restart deduplication; publish actual recovery steps.

No bot tokens or application secrets belong in this repository or its issue tracker.

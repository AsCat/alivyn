# Messaging channel integration spike

Reviewed on 2026-10-23 against provider documentation. These are integration choices, not tested account connections.

## Telegram

Use a BotFather-created bot, with `getUpdates` long polling for a local daemon and `sendMessage` for outbound contact. Long polling avoids requiring a public callback address. The user must [contact the bot first](https://core.telegram.org/bots/tutorial); onboarding should guide the user to press Start, capture the resulting chat ID, then send a confirmation message. The [Bot API](https://core.telegram.org/bots/api) and [FAQ](https://core.telegram.org/bots/faq) describe update offsets and the incompatibility of long polling with an active webhook. The wizard should detect an existing webhook and offer an explicit fix rather than silently replacing it.

## Feishu

Use an **application bot** for a one-to-one companion. A [custom bot](https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot) only posts into its current group and cannot handle the intended private conversation. The [bot overview](https://open.feishu.cn/document/client-docs/bot-v3/bot-overview) covers receive and send capabilities; message APIs require the right application scopes and recipient ID.

The [WebSocket event mode](https://open.feishu.cn/document/ukTMukTMukTM/uYDNxYjL2QTM24iN0EjN/event-subscription-configure-) can receive events on a machine with outbound internet access, without a public IP. Its documentation currently says it applies to enterprise self-built applications, so the onboarding guide must check what a personal or small-team developer can actually create and authorize. Its three-second event-processing limit means the event handler should persist and acknowledge events quickly, then run model work asynchronously.

## Next validation

1. Create test accounts and applications under the intended onboarding conditions.
2. Complete each wizard from a fresh install, including permission setup and a test incoming and outgoing message.
3. Record permissions, platform errors, recovery steps, and whether a non-enterprise individual can finish Feishu setup.
4. Reconsider the domestic first channel if Feishu cannot meet a self-hosted, low-friction personal setup.

No bot tokens or application secrets belong in this repository or its issue tracker.

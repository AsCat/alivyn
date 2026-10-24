# Messaging channel integration spike

Reviewed on 2026-10-24 against provider documentation. These are integration choices, not tested account connections.

## Telegram

Use a BotFather-created bot, with `getUpdates` long polling for a local daemon and `sendMessage` for outbound contact. Long polling avoids requiring a public callback address. The user must [contact the bot first](https://core.telegram.org/bots/tutorial); onboarding should guide the user to press Start, capture the resulting chat ID, then send a confirmation message. The [Bot API](https://core.telegram.org/bots/api) and [FAQ](https://core.telegram.org/bots/faq) describe update offsets and the incompatibility of long polling with an active webhook. The wizard should detect an existing webhook and offer an explicit fix rather than silently replacing it.

## Feishu

Use an **application bot** for a one-to-one companion. A [custom bot](https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot) only posts into its current group and cannot handle the intended private conversation. The [bot overview](https://open.feishu.cn/document/client-docs/bot-v3/bot-overview) covers receive and send capabilities; message APIs require the right application scopes and recipient ID.

### Shared bot: default onboarding prototype

Feishu supports [sharing an application bot by AppLink or QR code](https://open.feishu.cn/document/develop-robots/add-bot-to-external-group?lang=zh-CN). With external one-to-one sharing enabled and first-use review disabled by its owner, an external user can scan, confirm the app information, and open the bot chat. This is the default intended onboarding: Alivyn shows a one-time pairing code in the terminal; the user scans the shared bot QR and sends the code in the chat; the service links that Feishu user to the correct local Alivyn instance. The user creates no Feishu app and handles no App ID, App Secret, or `open_id`.

The repository now contains `alivyn relay serve` and the single user entry point `alivyn channel feishu connect [relay-url]` (plus separate `pair` and `run` commands for recovery). The relay accepts only private text events from its Feishu WebSocket, binds a one-time 56-bit code to one Feishu account, and routes messages to the authenticated local runtime over HTTPS. The code expires in ten minutes. The relay database holds the binding and a hash of the agent token; message bodies use a memory queue of up to 32 items per agent and expire after five minutes. The local runtime acknowledges messages after committing them to SQLite, deduplicates Feishu events, and stores generated replies for retry. The relay sees plaintext in transit and cannot promise end-to-end encryption or delivery while offline. Sending may repeat if the local process crashes after Feishu accepts a message but before the local sent marker is committed.

The project must operate the shared bot and relay for the no-argument user command. Its owner must enable external sharing and complete the required organizational or mainland-China personal identity verification. The user must open the bot chat first; scanning alone does not authorize messages. The QR path still needs an account-level trial before it can be described as available.

For a development relay, the operator sets `ALIVYN_RELAY_FEISHU_APP_ID`, `ALIVYN_RELAY_FEISHU_APP_SECRET`, and `ALIVYN_RELAY_FEISHU_BOT_URL` to the project-owned bot credentials and share link, then runs `alivyn relay serve`. By default the HTTP API listens on `127.0.0.1:8787`; put it behind an HTTPS reverse proxy and point `ALIVYN_RELAY_DATA` to a private durable directory. Set `ALIVYN_RELAY_LISTEN` only when a different local listener is needed. A tester runs `alivyn channel feishu connect https://<relay-host>` after `alivyn init`, scans the displayed bot link and sends the code; the command then stays online to reply. The operator owns the only app configuration. The public shared service address is not embedded yet because the bot has not been provisioned.

### Self-owned bot: fully self-hosted pairing

Personal users are a viable starting point. Feishu's [account guide](https://www.feishu.cn/hc/zh-CN/articles/985882202335-%E7%AC%AC%E4%B8%80%E6%AD%A5-%E5%88%9B%E5%BB%BA%E4%BC%81%E4%B8%9A%E4%B8%8E%E9%82%80%E8%AF%B7%E6%88%90%E5%91%98) says an individual can create a free organization. The [platform changelog](https://open.feishu.cn/changelog?abilityType=Tool&lang=zh-CN) records review-free publishing and immediate permission grants for personal users' self-built apps. A bot used by its creator within the same organization does not need external sharing. [External-user conversations](https://open.feishu.cn/document/develop-robots/add-bot-to-external-group?lang=zh-CN) are a separate capability with extra confirmation and identity requirements.

The optional `alivyn channel feishu add` now uses the official Go SDK's [one-click app registration](https://github.com/larksuite/oapi-sdk-go#one-click-app-registration). It displays a QR code; after the user confirms app creation in Feishu, the returned App ID, secret and scanning user's `open_id` are stored in a local `0600` file. The CLI requests only private-message receive, bot-send and the receive-message event. `alivyn channel feishu run` accepts only private text from that user, stores it quickly, then generates and sends a reply asynchronously. This path still creates an app under the user's account, so it is an opt-in self-hosted path rather than the default. The [WebSocket event mode](https://open.feishu.cn/document/ukTMukTMukTM/uYDNxYjL2QTM24iN0EjN/event-subscription-configure-) needs outbound internet access but no public callback. Whether QR registration also enables and publishes every necessary bot setting must be checked with a real account.

## Next validation

1. Create a Feishu bot under a project-controlled account; test external sharing, its QR code, first-use confirmation, and private-message events with a separate personal account.
2. Deploy the relay behind HTTPS and complete the existing pairing and routing flow with two real accounts and two local runtimes; check bot permission review, queue expiry and re-pairing.
3. Create a fresh personal Feishu organization; test the optional QR app-registration path end to end.
4. Verify the implemented proactive delivery on a real phone, including pause, quiet hours, correction, and restart behavior. Feishu's send-message `uuid` only deduplicates within one hour; attempts older than 55 minutes are recorded as uncertain failures rather than retried.

No bot tokens or application secrets belong in this repository or its issue tracker.

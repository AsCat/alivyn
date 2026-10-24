# Alivyn · 意续

**一个有连续记忆、会在合适时机主动联系你的开源 AI 个体。**

Alivyn 是可自托管的开源项目。它围绕同一个持续存在的 AI 个体设计：记得共同经历，保持稳定的性格，感知时间流逝，并判断什么时候适合主动联系。用户自己配置模型，也决定联系渠道和打扰边界。

> **当前状态：本地预览。** 项目机器人已通过真实手机验证：扫码配对、文字私聊回复、主动发送均已成功。中继目前只运行在项目运营方的 Mac 上；公开 HTTPS 中继和默认接入地址尚未提供，外部账号访问也未验证。实施顺序见[开发路线](docs/roadmap.md)，人物与关系设计见[角色模型](docs/character-model.md)。

[English](README.md) · [目标](docs/vision.md) · [v0.1 规格](docs/spec-v0.1.md) · [架构](docs/architecture.md) · [路线](docs/roadmap.md) · [原型验证](docs/prototype-validation.md)

## 我们要做出的体验

你说周三有面试，后来改到周五。意续记得这次变化，周三保持安静，周五再根据当时的情境和你的联系设置决定是否主动问候。电脑休眠后，它知道自己的时间线出现了间隔，也不会每次唤醒都重复打招呼。

首版将提供终端界面、本地持久运行、用户自配模型、可纠正和删除的记忆，以及 Telegram 与飞书连接。专门的可视化空间和语音来电属于后续阶段。

意续明确是 AI。性格、情绪和连续性是产品的状态建模与交互体验，不声称它具有真实意识或人类情感。

## 试运行本地预览

需要 Go 1.26，或允许 Go 自动下载相应工具链。先用 `demo` 看一段可快进的真实本地运行流程；它使用临时数据库，无需模型密钥，也不会修改你的个体和对话。

```sh
go run ./cmd/alivyn demo
```

`init` 在本地保存个体和模型设置；模型密钥单独保存在用户私有配置目录，文件权限为 `0600`。

```sh
git clone https://github.com/AsCat/alivyn.git
cd alivyn
go run ./cmd/alivyn init
printf '模型密钥: '
read -rs ALIVYN_API_KEY
printf '\n'
printf '%s' "$ALIVYN_API_KEY" | go run ./cmd/alivyn model key import
unset ALIVYN_API_KEY
go run ./cmd/alivyn model check
go run ./cmd/alivyn
```

选择 `deepseek` 会使用当前的 `deepseek-flash` 预设；选择 `custom` 可填写兼容接口地址和模型 ID。预设依据 [DeepSeek 当前模型文档](https://api-docs.deepseek.com/quick_start/pricing/)。远程自定义接口必须使用 HTTPS；明文 HTTP 仅允许连接本机模型服务。密钥优先从 `ALIVYN_API_KEY` 读取；DeepSeek 配置还支持 `DEEPSEEK_API_KEY`；否则读取本机私有的 `model.key`。密钥不写入 `config.json` 或数据库，也不会提交到仓库。在 macOS 上，本地数据保存在用户配置目录；可通过 `ALIVYN_HOME` 指定其他位置。模型提供方会收到生成回复所需的对话上下文。
初始化时可填写用户所在的 IANA 时区，例如 `Asia/Shanghai`；在其他时区的服务器运行时尤其需要设置。旧版配置没有这个字段时，继续使用运行机器的本地时区。

默认命令进入同一段持续对话的 TUI：Enter 发送、PgUp/PgDn 查看历史、Esc 退出。`chat` 是纯文字终端备用入口，两者共用同一份本地历史。`history` 可打印保存的消息，`doctor` 检查 SQLite；`demo tui` 可检查终端渲染。`demo` 会用临时数据库演示改期记忆、周三沉默、周五问候、休眠后恢复和防止重复打招呼。这些命令不会启动后台智能体。

默认语气是**自然**：直接、简洁，亲近感随着真实交流逐渐形成。意续初识时把对方当陌生人；有效交谈会让关系逐步走向初识、熟悉和亲近，反复说“你好”不会让她自动变熟。对方要求保持距离时，她会收敛亲密语气。`alivyn persona show` 查看风格与人物背景；`alivyn persona set natural|romantic|companion` 切换风格，`alivyn persona background set '慢热，遇到陌生人先观察对方。'` 可设置简短人物背景，并影响第一次打招呼。已运行的渠道需要重启才能生效。飞书回复会按收到消息的时间计算一个有上限的短暂间隔；模型已经花较长时间时不会额外等待。意续把当地时间当作背景信息，不会只凭时钟猜测你在做什么。

当前记忆提取器识别明确的面试表述，也识别“我叫阿意”“我喜欢咖啡”等简短、明确的个人陈述。个人记忆保留来源，长对话后仍能调用；后来说“我不喜欢咖啡”会修正旧偏好，说“忘记我喜欢咖啡”或“忘记我的名字”会删除对应记忆并遮盖原始消息。面试记忆保留改期和 `memory forget interview` 的机制。用 `memory list` 查看当前事实，或用 `memory export` 导出包含 `events` 和 `personal` 的 JSON。较长或含糊的说法不会自动变成长期个人记忆；这里也不是完整账号数据导出。

无需模型密钥或聊天账号即可复演联系决策：

```sh
go run ./cmd/alivyn replay scenarios/interview-rescheduled.json
go run ./cmd/alivyn replay scenarios/wake-without-spam.json
go run ./cmd/alivyn replay scenarios/pause-stops-outbound.json
```

面试场景复演使用与 TUI、纯文字对话相同的用户话语提取器。复演仍不会实际发送消息；目前只覆盖有限的面试表达。

## 运行本地主动联系循环

完成初始化后，明确提到未来面试会形成待跟进意图。可在另一个终端或进程管理器下运行：

```sh
go run ./cmd/alivyn contact status
go run ./cmd/alivyn contact policy 22:00 08:00 2
go run ./cmd/alivyn serve
```

`serve once` 只处理一次并退出。`contact pause` 和 `contact resume` 控制所有主动联系；`contact intents` 查看意图状态，`contact audit` 查看发送、跳过、取消、失败的原因，`contact outbox` 查看测试投递。`alivyn serve` 只写入本地 SQLite 测试投递箱；`alivyn channel feishu run` 可以按同一策略向已配对的私聊主动发送。首次投递时，模型会根据当前人格与具体联系缘由写一句简短开场；生成失败则使用固定备用文案。生成后、发送前再次复核打扰策略；文案、重试和结果都会跨重启保存。电脑睡眠时本机循环会暂停；如需持续运行，可部署在始终在线的服务器。

## 飞书扫码接入（开发预览）

默认目标是用户只需运行 `alivyn channel feishu connect`，扫描**项目共享机器人**的二维码，在私聊发送终端显示的一次性配对码；命令会在配对后继续运行并回复消息。用户无需创建飞书应用、申请权限或填写 App ID / App Secret。配对码十分钟过期，同一飞书账号重新发送新配对码会转移到新的本地意续实例。项目机器人已在运营方飞书账号内跑通，但公开 HTTPS 中继和默认地址尚未上线，因此无参数命令仍会提示服务未开放；开发者可用 `alivyn channel feishu connect <中继 HTTPS 地址>` 接入自建中继。配置和服务端运行细节见[渠道技术验证](docs/channel-spike.md)。

私聊消息会经过共享中继到本机，再发往用户选用的模型提供方。中继保存账号绑定和令牌摘要；消息只进入短时内存队列，不保留服务端聊天记录。**本机离线超过五分钟或队列满时可能丢消息；发送成功后本机退出也可能造成重复回复。**本地历史和模型密钥仍在用户设备。飞书链路支持文字私聊回复和向已配对私聊主动发送。`alivyn channel feishu add` 是可选的个人机器人自动创建流程，不是默认使用步骤；已用运营方真实账号验证。

## 实现原则

- **跨渠道只有一个身份：** 用户创建的个体共用一份经历。
- **三个循环：** 回应、内部状态更新、主动表达分别有明确边界。
- **联系决定可追溯：** 每条候选消息都可以重新评估、取消或保持沉默。
- **用户掌控打扰：** 暂停、免打扰、频率限制、记忆纠正、导出和删除优先于生成内容。
- **行为可复演：** 首批[场景](scenarios/README.md)定义运行时必须通过的三条故事。

## 参与开发

当前仍在早期。贡献方式见 [CONTRIBUTING.md](CONTRIBUTING.md)。项目采用 [Apache-2.0](LICENSE) 许可证；安全问题的报告方式见 [SECURITY.md](SECURITY.md)。

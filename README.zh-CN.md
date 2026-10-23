# Alivyn · 意续

**一个有连续记忆、会在合适时机主动联系你的开源 AI 个体。**

Alivyn 是可自托管的开源项目。它围绕同一个持续存在的 AI 个体设计：记得共同经历，保持稳定的性格，感知时间流逝，并判断什么时候适合主动联系。用户自己配置模型，也决定联系渠道和打扰边界。

> **当前状态：本地预览。** 现已能创建一个 AI 个体、配置 DeepSeek 或自定义兼容模型、在持续的本地 TUI 中对话、记住并修正明确提及的面试安排，以及复演主动联系决策。后台自主联系与聊天工具接入仍在开发。实施顺序见[开发路线](docs/roadmap.md)。

[English](README.md) · [目标](docs/vision.md) · [v0.1 规格](docs/spec-v0.1.md) · [架构](docs/architecture.md) · [路线](docs/roadmap.md) · [原型验证](docs/prototype-validation.md)

## 我们要做出的体验

你说周三有面试，后来改到周五。意续记得这次变化，周三保持安静，周五再根据当时的情境和你的联系设置决定是否主动问候。电脑休眠后，它知道自己的时间线出现了间隔，也不会每次唤醒都重复打招呼。

首版将提供终端界面、本地持久运行、用户自配模型、可纠正和删除的记忆，以及 Telegram 与飞书连接。专门的可视化空间和语音来电属于后续阶段。

意续明确是 AI。性格、情绪和连续性是产品的状态建模与交互体验，不声称它具有真实意识或人类情感。

## 试运行本地预览

需要 Go 1.26，或允许 Go 自动下载相应工具链。`init` 在本地保存个体和模型设置，不会询问或保存密钥。

```sh
git clone https://github.com/AsCat/alivyn.git
cd alivyn
go run ./cmd/alivyn init
printf '模型密钥: '
read -rs ALIVYN_API_KEY
printf '\n'
export ALIVYN_API_KEY
go run ./cmd/alivyn model check
go run ./cmd/alivyn
```

选择 `deepseek` 会使用当前的 `deepseek-flash` 预设；选择 `custom` 可填写兼容接口地址和模型 ID。预设依据 [DeepSeek 当前模型文档](https://api-docs.deepseek.com/quick_start/pricing/)。远程自定义接口必须使用 HTTPS；明文 HTTP 仅允许连接本机模型服务。密钥每次从 `ALIVYN_API_KEY` 读取，不写入配置和数据库。在 macOS 上，本地数据保存在用户配置目录；可通过 `ALIVYN_HOME` 指定其他位置。模型提供方会收到生成回复所需的对话上下文。
初始化时可填写用户所在的 IANA 时区，例如 `Asia/Shanghai`；在其他时区的服务器运行时尤其需要设置。旧版配置没有这个字段时，继续使用运行机器的本地时区。

默认命令进入同一段持续对话的 TUI：Enter 发送、PgUp/PgDn 查看历史、Esc 退出。`chat` 是纯文字终端备用入口，两者共用同一份本地历史。`history` 可打印保存的消息，`doctor` 检查 SQLite，`demo` 保留为界面验证。这些命令不会启动后台智能体。

当前记忆提取器识别明确的面试表述，例如“周三下午有面试”“面试改到周五了”“面试取消了”。它记录来源消息、时间和事实来源；仅说日期或上午、下午时，不会把推定时刻当作精确预约。未识别的说法不会产生记忆。用 `go run ./cmd/alivyn memory list` 查看当前事实，或用 `go run ./cmd/alivyn memory export` 导出 JSON。`go run ./cmd/alivyn memory forget interview` 会删除当前面试事实，并遮盖本地对话历史中与面试有关的文字。这里导出的是当前提取记忆，不是完整账号数据。

无需模型密钥或聊天账号即可复演联系决策：

```sh
go run ./cmd/alivyn replay scenarios/interview-rescheduled.json
go run ./cmd/alivyn replay scenarios/wake-without-spam.json
go run ./cmd/alivyn replay scenarios/pause-stops-outbound.json
```

面试场景复演使用与 TUI、纯文字对话相同的用户话语提取器。复演仍不会实际发送消息；目前只覆盖有限的面试表达。

## 实现原则

- **跨渠道只有一个身份：** 用户创建的个体共用一份经历。
- **三个循环：** 回应、内部状态更新、主动表达分别有明确边界。
- **联系决定可追溯：** 每条候选消息都可以重新评估、取消或保持沉默。
- **用户掌控打扰：** 暂停、免打扰、频率限制、记忆纠正、导出和删除优先于生成内容。
- **行为可复演：** 首批[场景](scenarios/README.md)定义运行时必须通过的三条故事。

## 参与开发

当前仍在早期。贡献方式见 [CONTRIBUTING.md](CONTRIBUTING.md)。项目采用 [Apache-2.0](LICENSE) 许可证；安全问题的报告方式见 [SECURITY.md](SECURITY.md)。

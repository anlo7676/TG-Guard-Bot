# TG Guard Bot

基于 **Go 1.26.2 + MySQL 8.4 + Redis 8.6.2** 的 Telegram 智能群管机器人。以需求说明书第六十章的第一版 MVP 为交付范围，遵循“规则优先、AI 辅助、人工可干预”。

本版包含机器人、中文可视化 Web 后台、管理 API、数据库迁移、Docker Compose、测试和 CI 配置。图片／网页 CAPTCHA、OCR／二维码识别、计费及 SaaS 多租户属于后续阶段。

## 已实现

| 模块 | 能力 |
| --- | --- |
| Telegram 接入 | Long Polling、带 Secret 校验的 Webhook、超级群注册、管理员权限检查、成员记录、编辑消息审核 |
| 新人验证 | 自动禁言、32 字符安全 Token、Deep Link 私聊、数学题／按钮验证、3 次尝试、账号绑定、超时踢出／封禁／保持禁言 |
| 恢复机制 | MySQL 持久化验证状态，进程重启继续处理完成／超时任务，失败退避重试 |
| 本地审核 | 全角／零宽字符归一化、UTF-16 Telegram Entity、隐藏链接、URL、TG 引流、联系方式、广告词、博彩、色情推广、币圈招揽、大写和 Emoji |
| Spam | Redis 原子频率计数、归一化文本重复检测、新成员及首条消息加权、群独立累计处罚 |
| AI | Provider 接口、OpenAI Compatible Chat Completions、严格 JSON 校验、超时、并发上限、群限流、24 小时缓存、Token 用量日志 |
| 处罚 | 删除、警告、禁言、踢出、封禁、解除禁言／封禁、仅记录；执行前实时复核目标管理员／白名单身份 |
| 人工复核 | 回复消息 `/check`、`/ai` 或 @机器人；管理员按钮删除／禁言／封禁／误判／白名单，15 分钟有效期 |
| 关键词 | 群级 CRUD、5 种匹配方式、优先级、启停、文本／HTML／MarkdownV2／图片／视频／文件／随机文本、回复原消息、可视化链接按钮 |
| 名单 | 群／全局白名单、黑名单、可信名单，按 Telegram ID 或 username 匹配，支持有效期 |
| 管理 | Telegram 命令、Bearer Token 管理 API、仪表盘统计、群设置、用户查询、审核／处罚／验证／操作日志、误判反馈 |
| 运维 | MySQL 去重收件箱、不同群并行处理、失败重试与死信、JSON 日志、健康检查、优雅停机、单实例锁 |

## 快速启动

1. 使用 BotFather 创建机器人，取得 Bot Token；执行 `/setprivacy` 将 Privacy Mode 设为 Disable。
2. 将机器人加入 **超级群** 并提升为管理员，授予删除消息和限制／封禁成员权限。
3. 复制 `.env.example` 为 `.env`，填写 `BOT_TOKEN`、`ADMIN_API_TOKEN`、数据库及 Redis 密码。`ADMIN_API_TOKEN` 至少 32 个随机字符。不要将 `.env` 提交到版本库。
4. 启动服务。

### Docker Compose

需要安装 Docker Engine／Docker Desktop 和 Compose v2。

```powershell
Copy-Item .env.example .env
# 编辑 .env 后执行
docker compose up -d --build
docker compose logs -f app
```

Compose 固定 `golang:1.26.2-alpine`、`mysql:8.4`、`redis:8.6.2-alpine`。MySQL 8.4 使用该 LTS 系列的镜像更新。MySQL、Redis 不向宿主机暴露端口，HTTP 仅绑定宿主机 `127.0.0.1:8080`。

数据库迁移在应用启动时自动执行，迁移版本保存在 `schema_migrations`。所有连接使用 UTC 和 `utf8mb4`；Redis 启用 AOF。

### 本机运行与更新

```powershell
pwsh -File scripts/start-local.ps1
pwsh -File scripts/open-panel.ps1
# 停止：pwsh -File scripts/start-local.ps1 -Stop
```

启动脚本先构建、再替换进程，核对运行源码指纹。机器人 `/version` 和后台左下角显示当前版本。完整检查报告见 [2026-09-06 验收说明](docs/acceptance-2026-09-06.md)。

### 本机 Go 开发

提前安装 Go 1.26.2，并准备运行中的 MySQL 8.4 和 Redis 8.6.2。在 MySQL 创建 `tgguard` 数据库及对应账号，赋予该数据库内建表、索引和读写权限。修改 `.env` 中的 `MYSQL_DSN`、`REDIS_ADDR` 指向实际服务。

```powershell
Copy-Item .env.example .env
# 编辑 .env 后执行；脚本按 UTF-8 读取 .env，已有进程环境变量优先
.\scripts\run.ps1

# 仅迁移数据库
.\scripts\run.ps1 -MigrateOnly
```

在 Linux/macOS 上，先将配置设置为环境变量，再执行：

```sh
go run ./cmd/tgguard
```

Go 可执行程序本身只读环境变量；自动读取 `.env` 的入口是 PowerShell 脚本和 Compose。

### 配置 AI

打开 `http://127.0.0.1:8080`，使用 `.env` 的 `ADMIN_API_TOKEN` 登录，或运行 `scripts/open-panel.ps1` 一次性登录。私聊 `/start` 或 `/menu` 显示按钮菜单，`/id` 查询自己的数字 ID；在后台「机器人管理员」填写并保存 ID，立即获得跨群机器人命令权限。Telegram 群管理员仍在群设置中任命，Web 登录仍使用部署者凭据。

后台「AI 接口」填写 Base URL（通常包含 `/v1`）、API Key、模型 ID，启用后保存；模型需支持 Chat Completions 和 `response_format=json_object`。输出限制参数可选择 `max_completion_tokens` 或 `max_tokens`。点击「测试已保存的连接」会产生一次实际模型请求。也可用环境变量提供首次默认配置。

再由群管理员发送：

```text
/settings {"ai_enabled":true}
```

也可在后台「群管理」启用目标群的 AI。AI 默认关闭，填写 API Key 不会自动启用各群。未配置或超时的 AI 不会触发基于 AI 的处罚；明确本地高风险规则和 Spam 仍按群策略处理。后台配置立即生效且重启保留，保存后以数据库配置为准。API Key 使用 AES-GCM 加密保存，不在响应或审计中回显；空 Key 保留原值，清除需显式勾选。备份时同时保存 `SETTINGS_ENCRYPTION_KEY`；未设置时从 `ADMIN_API_TOKEN` 派生，此时更换管理凭据会导致旧配置无法解密。

## 常用命令

| 命令 | 用法 |
| --- | --- |
| `/start` | 打开主菜单；私聊使用帮助按钮查看说明；`/start verify_TOKEN` 开始验证 |
| `/id` | 当前用户／群／消息及回复目标 ID |
| 新人验证 | 点击群内入群提示的验证按钮，在私聊完成；无需发送命令 |
| `/groups` | 私聊列出有权限管理的群组 |
| `/settings` | 群内打开本群设置按钮；私聊选择群组 |
| `/settings {"auto_ban":false}` | 覆盖提供的设置项；其他项保持原值 |
| `/stats`、`/rules` | 群统计、本地规则及覆盖值 |
| `/check`、`/ai` | 回复消息后调用 AI，只返回分析 |
| `/warn`、`/mute 1h`、`/ban`、`/kick` | 管理员回复目标用户消息执行处罚 |
| `/unmute`、`/unban` | 回复目标解除限制；也支持 `/unban 用户ID` |
| `/whitelist add 123456` | 加入群白名单；可用 `@username` 或回复用户消息 |
| `/whitelist remove 123456` | 移除群白名单 |
| `/blacklist add 123456` | 加入群黑名单；后续入群／发言触发封禁 |
| `/keywords add contains 官网 \| https://example.com` | 添加文本自动回复 |
| `/keywords del 规则ID` | 删除关键词规则 |

群管理命令均验证当前 Telegram 管理员或 `BOT_SUPER_ADMINS` 身份。命令支持 `@BotUsername` 后缀，不响应其他机器人的命令。命令内容和 @机器人消息同样先经过审核。

## 默认策略

- 验证启用：数学题，180 秒，失败踢出；踢出会执行 ban 后 unban，允许重新加入。
- 管理员／机器人／白名单／可信用户跳过验证；管理员始终免自动处罚。`admin_bypass=false` 可记录管理员风险，仍不会自动处罚管理员。
- 25 类常见广告预设默认命中即删除，不调用 AI；直接动作优先于以下累计评分策略。
- 无直接动作时，本地风险小于 50 直接放行；50–79 在该群启用 AI 时复核；80–100 由程序直接处理。
- AI 置信度 `<0.60` 放行，`0.60–0.79` 警告，`>=0.80` 根据群开关删除和警告；`>=0.95` 且严重违规可禁言。AI 返回的建议不能直接绕过策略执行 Ban。
- 累计评分策略默认首犯删除＋警告，第二次及以后删除＋禁言 1 小时。自动封禁默认关闭；启用后按 `ban_after` 累计次数执行。
- Spam 默认 10 秒超过 5 条、60 秒相同归一化文本达到 3 次。采用固定窗口计数；语义相似文本、图片内容及重复无字幕媒体尚不检测。
- 手工 AI 查询默认所有成员可用，最多每人每群每分钟 3 次；AI 实际调用另有每群每分钟 30 次限制。
- 关键词默认只触发优先级最高的一条，优先级相同时 ID 较小者优先；`keyword_all=true` 最多回复 5 条，避免自动回复刷屏。

私聊主菜单以「我的群组」为入口，选择群后可修改验证、审核、AI、防刷屏及自动处罚开关，并查看该群统计、规则、关键词和名单。每次读取和写入均检查当前群管理员权限。关键词和名单支持私聊编辑；按钮绑定用户和目标群，30 分钟过期，输入提示 10 分钟过期且可用 `/cancel` 取消。完整配置通过 Web 后台或管理 API 获取，群内 `/settings JSON` 仍支持高级参数。`rules` 对象支持按规则名称覆盖 `enabled`、`score`。全局黑名单优先于普通群白名单；Telegram 管理员和 Bot 超管仍受保护。

## 管理 API

HTTP 根地址默认为 `http://127.0.0.1:8080`。

```powershell
$headers = @{ Authorization = "Bearer $env:ADMIN_API_TOKEN" }
Invoke-RestMethod http://127.0.0.1:8080/api/v1/groups -Headers $headers
```

API 支持 Bearer Token 和 HttpOnly Cookie 会话，Cookie 写操作需要 CSRF Token。`actor_id=0` 表示部署者操作；当前不提供独立多管理员 Web 账号或按群 Web 授权。所有 `/api/v1/` 请求均需鉴权、限流；不启用 CORS，不接受跨 Origin 写入。默认仅本机访问，手机远程使用需部署 HTTPS 反向代理。

接口和请求示例见 [docs/api.md](docs/api.md)。部署与故障恢复见 [docs/operations.md](docs/operations.md)，架构与后续范围见 [docs/architecture.md](docs/architecture.md)。

## 测试与构建

```powershell
.\scripts\test.ps1
# 生成 bin/tgguard-next.exe，不覆盖正在运行的程序
```

```sh
go test -count=1 ./...
go vet ./...
go build ./cmd/tgguard
```

默认测试包含纯规则／决策、HTTP 模拟服务、SQL 事务模拟和内存 Redis Lua 测试。真实集成测试需要专用服务：

```powershell
$env:TEST_MYSQL_DSN = 'root:test-password@tcp(127.0.0.1:3306)/tgguard_test?parseTime=true'
$env:TEST_REDIS_ADDR = '127.0.0.1:6379'
go test -count=1 ./...
```

**MySQL 集成测试会清空测试数据库内的项目表，数据库名必须以 `_test` 结尾。** 未设置上述环境变量时对应真实服务测试明确跳过。CI 配置使用实际 MySQL 8.4 和 Redis 8.6.2，运行 `go test -race`、`go vet`、编译和 Docker 构建；本机 Windows 的 race 测试需要兼容的 C 工具链。

本次开发环境已运行的检查及未验证项目见 [docs/verification.md](docs/verification.md)。

## 项目结构

```text
cmd/tgguard/            程序入口和生命周期
internal/bot/          Telegram 事件路由、Polling 和工作池
internal/service/      验证、审核、处罚、人工操作与关键词业务
internal/rules/        标准化、规则评分、最终决策和关键词匹配
internal/domain/       Telegram 对象、领域模型和设置校验
internal/ai/           AI Provider 接口和兼容实现
internal/telegram/     Bot API 客户端、限流和统一错误处理
internal/store/        MySQL Repository、持久队列和内嵌迁移
internal/state/        Redis 缓存、Lua 计数和有主锁
internal/api/          HTTP 管理 API 和 Webhook
internal/i18n/         简体中文／英文核心消息词典
docs/                  API、运维、架构及验收说明
scripts/               Windows UTF-8 运行和测试脚本
```

## 接口资料

- [Telegram Bot API](https://core.telegram.org/bots/api)：群权限、Webhook、消息和回调协议。
- [OpenAI Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs)：JSON mode 仍需程序校验；本项目严格检查必填字段、枚举、范围及截断／拒绝响应。
- [Redis 8.6.2](https://github.com/redis/redis/releases/tag/8.6.2)：目标 Redis 版本。

## 群接入审批（v1.2）

机器人被添加到群后，自动登记为待审批。群主和 Telegram 群管理员不能自行授权。部署者登录网页后台，在「群组列表」点击「批准」「拒绝」或「撤销授权」，可填写原因；结果记录在该群审计日志。未配置机器人超级管理员时，部署者仍可使用后台登录凭据审批。

机器人超级管理员也可私聊使用：

- /approve -100群ID [原因]
- /reject -100群ID [原因]
- /revoke -100群ID [原因]

只有已批准且机器人仍在群内的群，才能使用审核、关键词、验证、处罚和群设置。超级管理员也不能跳过群授权。机器人离群会撤销授权，重新邀请后需要再次审批。拒绝或撤销不会删除原群配置，部署者可重新批准。

**升级说明：已有群也会转为待审批，需要部署者逐一批准。** 进行中的验证进入取消恢复队列，解除其验证限制；恢复失败会重试，可在验证记录查看错误。撤销时停止待执行处罚，清理验证不再踢人。已经发往 Telegram 的在途操作不能撤回。

API：已登录部署者可 PUT /api/v1/groups/{chat}/authorization，JSON 为 status（approved/rejected/revoked）及 reason。普通群管理员不获得网页后台权限；网页只读历史可继续查询，未授权群的配置写入返回 403。

### Telegram 命令交互（v1.2.2）

群聊中的 /rules、/stats、/keywords、/whitelist、/blacklist 返回中文摘要及对应功能按钮。/rules 合并默认规则和群自定义值后显示实际生效状态，不再输出 defaults、overrides 等内部字段。关键词和名单摘要限制条数，完整记录在私聊菜单分页管理。

私聊输入上述命令或 /settings，先选择已授权且有权限的群组，随后直接进入对应功能。群聊生成的链接、群选择翻页及刷新均保留功能目标。旧的群设置链接继续有效。/settings JSON 参数仍作为高级快捷修改方式保留，日常设置可全部通过菜单完成。

群内 `/start` 按发起者身份显示成员指引或管理员操作，并根据本群验证、AI 开关说明实际可用功能。群命令菜单已移除 `/verify`；手动输入旧命令只提示正确的验证入口。

## 本地广告动作与欢迎语（v1.3）

- 网页后台：群管理 → 配置群组。内置规则可选择累计风险分、命中即删除、删除并禁言、封禁并清理发言。原有规则保留评分行为，只有明确选择直接动作的规则才跳过累计阈值。
- 自定义广告规则支持包含、精确、正则。每行格式为「匹配方式|动作|内容」，例如「包含|禁言|稳赚包赔」。动作删除、禁言、封禁都会删除命中消息；禁言使用本群的禁言时长。多条命中时，封禁优先于禁言、删除。最多 50 条，每条 500 字；匹配文本及媒体说明，按消息归一化结果匹配，不区分大小写。
- 内容审核总开关控制广告规则。直接动作不依赖 AI、累计次数或自动处罚开关；管理员、超级管理员、白名单及可信用户仍受保护。命中记录和实际处罚分别写入审核、处罚日志。
- 私聊：/rules → 选群 → 点内置规则设置动作，或「编辑广告匹配词库」。词库输入会替换本群全部自定义规则；前缀「停用」保留但禁用该行，输入「清空」清空。长词库请在网页编辑。
- 欢迎语：/settings → 选群 → 入群欢迎语，或网页群配置。支持 {name}、{username}、{user_id}、{group}、{timeout}，纯文本、最多 1000 字。开启验证时自动附加验证说明和按钮；关闭欢迎语不会关闭验证。未开启验证时可独立欢迎新成员，同次入群重复事件不重复发送，离开后重新入群可再次欢迎。
- /ban 回复目标用户消息：封禁同时发送 revoke_messages=true，交由 Telegram 清理该用户在本群的全部消息，不限于机器人本地已记录的消息。依据：https://core.telegram.org/bots/api#banchatmember 。仍需 Telegram 管理权限，接口失败进入既有处罚重试与日志流程。
- 普通群成员菜单移除 /id。ID 查询保留在私聊与管理员菜单；普通成员手动输入时提示到私聊查询。

验收覆盖：直接规则动作与保护、包含/精确/正则及多命中优先级、私聊配置、广告删除/禁言/封禁与重试、欢迎语变量/开关/重复入群、封禁清理参数。真实 MySQL/Redis 测试使用独立测试库和模拟 Telegram，不对真实群成员执行处罚。

`/help` 已从全部命令菜单及命令处理入口移除；使用说明保留在私聊主菜单的「使用帮助」按钮。

## 常见广告默认规则（v1.4）

内置 25 类广告预设：保本暴利、投资带单、博彩开户、色情招揽、刷单返佣、贷款中介、额度套现、虚拟币交易、空投钱包诱导、外部群引流、群发推广、账号买卖、接码养号、证件文凭代办、发票代开、个人资料交易、刷粉刷量、代理节点销售、破解软件销售、购物返利、高薪招募诱导、英文投资诈骗、英文赠币诱导、跑分招募、低价代充。默认启用，匹配明确广告组合后直接删除，不需要 AI。原有 11 类通用风险指标继续累计评分。

后台「群管理 → 配置群组 → 默认本地规则」显示每类规则、示例、开关、分值与动作，可改为禁言或封禁。既有群组和新群组均使用默认值，已有单条覆盖配置优先，自定义词库保持不变；自定义输入框为空不代表没有内置规则。群组必须已批准、开启消息审核，机器人须具备对应管理权限，管理员和白名单保护继续生效。

这些是有限的本地文本特征规则，覆盖常见广告类型，不能识别所有新变种或图片内文字。正常讨论若引用广告组合也可能命中，可按群用途逐条停用或改为累计评分，并补充自定义匹配规则。

## 警告说明（v1.4.1）

群内警告优先使用用户名，否则使用昵称并链接对应 Telegram 用户；仅在两者均缺失时显示数字 ID。警告显示本地规则、刷屏检测、AI 复核或人工操作来源，以及本次处理、自动处罚累计次数和本群实际禁言／封禁阈值。计数来自本群已完成的自动处罚，不按天清零，人工警告不增加该计数。累计升级只适用于累计策略达到违规处罚条件的消息；低置信度警告及固定的单条规则动作不因次数升级，严重 AI 判定可按策略提前禁言。

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
| `/start`、`/help` | 使用说明；私聊 `/start verify_TOKEN` 开始验证 |
| `/id` | 当前用户／群／消息及回复目标 ID |
| `/verify` | 当前群内本人的验证状态 |
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
- 本地风险小于 50 直接放行；50–79 在该群启用 AI 时复核；80–100 由程序直接处理。
- AI 置信度 `<0.60` 放行，`0.60–0.79` 警告，`>=0.80` 根据群开关删除和警告；`>=0.95` 且严重违规可禁言。AI 返回的建议不能直接绕过策略执行 Ban。
- 默认首犯删除＋警告，第二次及以后删除＋禁言 1 小时。自动封禁默认关闭；启用后按 `ban_after` 累计次数执行。
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

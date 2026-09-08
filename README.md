# TG Guard Bot

v1.12.0 增加私聊自助验证：验证超时后保持禁言的成员，可从主菜单或 `/verify` 重新验证并恢复发言。保留广告重发禁言、30 分 AI 复核及 MySQL、Redis 与配置的备份。见 [备份与恢复](docs/backup-and-recovery.md) 和 [网页升级与 DC 查询](docs/web-upgrade-and-dc.md)。

基于 **Go 1.26.7 + MySQL 8.4 + Redis 8.6.2** 的 Telegram 智能群管机器人。以需求说明书第六十章的第一版 MVP 为交付范围，遵循“规则优先、AI 辅助、人工可干预”。

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
| 名单 | 群／全局白名单、黑名单、可信名单，仅绑定稳定的 Telegram 数字 ID，支持有效期；用户名仅作备注 |
| 管理 | Telegram 命令、Bearer Token 管理 API、仪表盘统计、群设置、用户查询、审核／处罚／验证／操作日志、误判反馈 |
| 运维 | MySQL 去重收件箱、不同群并行处理、失败重试与死信、JSON 日志、健康检查、优雅停机、单实例锁 |

## 一行命令安装（Linux 服务器）

在 Ubuntu / Debian 服务器执行，无需提前下载项目或安装 Docker（命令入口需要 `curl` 和 `sudo`；root 用户可省略 `sudo`）：

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/anlo7676/TG-Guard-Bot/main/install.sh)"
```

脚本自动检查依赖，按需通过 [Docker 官方软件源](https://docs.docker.com/engine/install/ubuntu/#install-using-the-apt-repository) 安装 Docker 和 Compose，下载项目到 `/opt/tg-guard`，再提示输入 **Bot Token**。数据库、Redis、密码、加密密钥及后台登录入口自动配置。

**执行后进入中文管理菜单，选择“安装 / 启动”开始安装，选择“更新到最新版本”执行更新。** 保留 `.env` 和 Docker 数据卷，仓库有本地修改或无法快进时停止更新。其他 Linux 发行版需先自行安装 Docker、Compose、git、curl、openssl。远程后台访问仍使用下文的 SSH 端口转发；不会直接开放管理后台到公网。

## 下载源码后部署（Windows / Linux / macOS）

只需安装 Docker（Windows 使用 Docker Desktop），无需单独安装 Go、MySQL 或 Redis。Linux 还需要系统常见工具 `bash`、`openssl`、`curl`；Windows 使用 PowerShell 7。

下载项目后，在项目目录执行：

**Linux / macOS：**

```sh
git clone https://github.com/anlo7676/TG-Guard-Bot.git
cd TG-Guard-Bot
bash scripts/deploy.sh
```

**Windows：**

```powershell
git clone https://github.com/anlo7676/TG-Guard-Bot.git
cd TG-Guard-Bot
pwsh -File scripts/deploy.ps1
```

也可直接从仓库下载 ZIP 并解压，无需配置 Git SSH。

首次只按提示输入 **Bot Token**。脚本生成数据库密码、后台凭据和独立加密密钥，保存到 `.env`，然后下载预编译程序并启动全部服务，等待健康检查通过。Windows 自动打开后台；Linux 输出有效期 1 分钟的一次性登录地址。首次拉取镜像和程序需要几分钟，并需要网络可访问镜像仓库和 Telegram。

进入后台后：

1. 在「机器人管理员」设置自己的 Telegram 数字 ID（私聊机器人 `/id` 获取）。AI 接口可稍后按需填写，本地规则无需 AI。
2. 将机器人加入超级群并设为管理员，授予删除消息和限制成员权限；在 BotFather 关闭 Privacy Mode。
3. 在网页群组列表批准该群，即可开始使用。

后台只监听本机 `127.0.0.1:8080`。部署到远程服务器时，在自己电脑另开终端运行 `ssh -L 8080:127.0.0.1:8080 用户@服务器`，再打开脚本给出的登录地址。票据失效后可重新执行部署脚本获取，或使用 `.env` 中的 `ADMIN_API_TOKEN` 登录。

**更新：** `git pull --ff-only` 后再次执行同一部署命令。已有 `.env` 不覆盖、密码不重置、数据卷保留。请备份 `.env` 和数据库；原先手工部署的配置不完整时，脚本会提示缺失字段，不擅自改写。已有本机 Go 进程时，继续使用下面的本机更新方式；不要用同一个 Token 同时启动两个机器人实例。

**查看日志：** `docker compose logs --tail 100 app`。**停止：** `docker compose stop`。启动失败可修正配置后重试。

Compose 使用 Go 1.26.7、MySQL 8.4、Redis 8.6.2，数据库迁移自动执行。MySQL 和 Redis 不向宿主机暴露端口，数据保存在 Docker 命名卷中。

### 本机运行与更新

```powershell
pwsh -File scripts/start-local.ps1
pwsh -File scripts/open-panel.ps1
# 停止：pwsh -File scripts/start-local.ps1 -Stop
```

启动脚本先构建、再替换进程，核对运行源码指纹。机器人 `/version` 和后台左下角显示当前版本。完整检查报告见 [2026-09-06 验收说明](docs/acceptance-2026-09-06.md)。

### 本机 Go 开发

提前安装 Go 1.26.7，并准备运行中的 MySQL 8.4 和 Redis 8.6.2。在 MySQL 创建 `tgguard` 数据库及对应账号，赋予该数据库内建表、索引和读写权限。修改 `.env` 中的 `MYSQL_DSN`、`REDIS_ADDR` 指向实际服务。

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

打开 `http://127.0.0.1:8080`，使用 `.env` 的 `ADMIN_API_TOKEN` 登录，或运行 `scripts/open-panel.ps1` 一次性登录。私聊 `/start` 或 `/menu` 显示按钮菜单，`/id` 查询自己的数字 ID；在后台「机器人管理员」填写并保存 ID，立即获得跨群机器人命令权限。Telegram 群管理员仍在群设置中任命，Web 登录仍使用后台管理员凭据。

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
| `/check`、`/ai` | 回复消息后调用 AI；判广告则删除并警告，重复相同内容升级禁言 |
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
- 私聊「自助验证 / 解除验证禁言」或 `/verify` 列出本人当前入群的待验证记录。超时后保持禁言的记录可重新出题，每人每群每小时最多重开 3 次；成功后恢复群默认发言权限。踢出、封禁、黑名单、机器人处罚或已观察到的 Telegram 管理员权限接管不允许自助恢复。再次超时仍保持禁言；群内不提供 `/verify` 命令。
- 管理员／机器人／白名单／可信用户跳过验证；管理员始终免自动处罚。`admin_bypass=false` 可记录管理员风险，仍不会自动处罚管理员。
- 32 类常见广告预设默认删除并警告；同一用户重发相同内容升级禁言，不同广告累计第 4 次起持续禁言，交由管理员决定封禁或解禁。直接动作仍优先于累计评分策略。
- AI 默认触发分数为 30（包含 30）；开启群 AI 和全局接口后，达到阈值即尝试 AI 复核，高分及刷屏消息也会复核。明确本地动作和高分直接处理策略仍保留，AI 失败不能抹去已有本地违规证据。
- AI 置信度 `<0.60` 放行，`0.60–0.79` 警告，`>=0.80` 根据群开关删除和警告；`>=0.95` 且严重违规可禁言。AI 返回的建议不能直接绕过策略执行 Ban。
- 累计评分策略默认首犯删除＋警告，第二次及以后删除＋禁言 1 小时。自动封禁默认关闭；启用后按 `ban_after` 累计次数执行。
- Spam 默认 10 秒超过 5 条、60 秒相同归一化文本达到 3 次。采用固定窗口计数；语义相似文本、图片内容及重复无字幕媒体尚不检测。
- 手工 AI 查询默认所有成员可用，判广告后删除并警告（仍保护管理员、机器人和白名单）；同一原消息不重复处罚。最多每人每群每分钟 3 次；AI 实际调用另有每群每分钟 30 次限制。
- 重复广告记录从升级后的成功删除＋警告开始建立，按群组、用户和归一化文本持久保存；同一用户在本群重发才会触发，禁言时长使用本群设置。管理员标记原记录误判后清除对应证据；媒体画面和语义相似内容不在此匹配范围内。
- 关键词默认只触发优先级最高的一条，优先级相同时 ID 较小者优先；`keyword_all=true` 最多回复 5 条，避免自动回复刷屏。

私聊主菜单以「我的群组」为入口，选择群后可修改验证、审核、AI、防刷屏及自动处罚开关，并查看该群统计、规则、关键词和名单。每次读取和写入均检查当前群管理员权限。关键词和名单支持私聊编辑；按钮绑定用户和目标群，30 分钟过期，输入提示 10 分钟过期且可用 `/cancel` 取消。完整配置通过 Web 后台或管理 API 获取，群内 `/settings JSON` 仍支持高级参数。`rules` 对象支持按规则名称覆盖 `enabled`、`score`。全局黑名单优先于普通群白名单；Telegram 管理员和 Bot 超管仍受保护。

## 管理 API

HTTP 根地址默认为 `http://127.0.0.1:8080`。

```powershell
$headers = @{ Authorization = "Bearer $env:ADMIN_API_TOKEN" }
Invoke-RestMethod http://127.0.0.1:8080/api/v1/groups -Headers $headers
```

API 支持 Bearer Token 和 HttpOnly Cookie 会话，Cookie 写操作需要 CSRF Token。`actor_id=0` 表示后台管理员操作；当前不提供独立多管理员 Web 账号或按群 Web 授权。所有 `/api/v1/` 请求均需鉴权、限流；不启用 CORS，不接受跨 Origin 写入。默认仅本机访问，手机远程使用需部署 HTTPS 反向代理。

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

## 当前群管理行为

- **接入审批**：机器人入群后待审批，由后台管理员在网页群组列表批准，或机器人超级管理员私聊使用 `/approve 群ID`。群主不能自行授权；离群后再加入需要重新审批。
- **群设置**：网页「群管理 → 配置群组」管理审核、规则、验证和欢迎语；私聊 `/settings` 或 `/rules` 先选择群组。关键词新增和编辑采用“关键词 → 回复内容”两步输入，同义词支持 `官网|网站|网址`。
- **本地广告规则**：32 类广告预设与 11 类通用风险指标，可逐条开关、调整动作，另可添加包含、精确或正则规则。默认广告动作无需 AI：前三次删除并警告，第 4 次起持续禁言，交由管理员 `/ban 用户ID` 或 `/unmute 用户ID` 处理。次数取本群该成员已完成的自动处罚，不按天清零；显式禁言、封禁规则按所选动作执行。
- **转发审核**：正文、转发来源及外部引用片段分别匹配，同一规则去重计分。普通群内回复不继承原消息；“举报”等前缀不赋予广告转发免审资格。本地规则不能识别所有变体、反诈语境或图片内文字。
- **规则试判**：网页使用已保存的群配置试判，可填写来源和引用；不调用 AI、不执行处罚、不增加违规次数。实际审核仍受群授权、审核开关及管理员、白名单等保护策略控制。
- **验证和欢迎**：公开验证链接只有对应新人可答题。验证成功后发送欢迎语，未开验证时直接欢迎；欢迎语包含可点击用户提及，默认约 5 分钟自动删除，接口失败持久化重试。
- **人工处罚**：`/ban` 同时请求 Telegram 清理目标用户在本群的消息；已创建的旧自动处罚任务会尊重后续通过机器人完成的人工处理。普通成员菜单不提供 `/id`，`/help` 已移除，使用指引在私聊菜单。

升级历史及每次修复说明见 [版本记录](CHANGELOG.md)，部署、换机器人和故障恢复见 [运维说明](docs/operations.md)。

## 本地文件管理

`.gitignore` 排除环境配置、密钥文件、二进制、缓存、日志、备份、编辑器配置和会话附件；`.env.example` 保留用于部署。数据库迁移、测试和 `go.sum` 必须提交。

`bin/` 保存运行文件、待发布构建及回滚文件，`tmp/` 保存构建缓存、PID 和运行日志。服务运行期间不要整目录清空；本地数据库备份统一放在 `backups/`。Docker 构建上下文也排除了本地配置、附件和产物。

## 预编译发布

默认 Dockerfile 从 GitHub Release 下载当前固定版本的 Linux amd64 / arm64 程序，校验 SHA-256 后装入 Alpine 运行镜像。服务器无需下载 Go 工具链或编译源码；MySQL、Redis 和原数据卷不变。下载失败会停止构建，不会自动退回耗时的源码编译。

本地执行 `pwsh -File scripts/build-release.ps1` 可交叉编译两种架构，产物位于 Git 忽略的 `dist/`。推送版本标签后，Release 工作流使用 Go 1.26.7 测试、构建并上传程序和校验文件。二进制保存在 Release 附件中，不进入 Git 源码历史。

源码构建入口保留为 `docker build -f Dockerfile.source -t tgguard:source .`。维护者发布新版时须同步应用版本和 Dockerfile 的 RELEASE_VERSION，再推送对应版本标签；普通用户重复执行一行安装命令即可更新。

## 使用服务器 IP 打开后台

默认仅本机访问。需要 IP 直连时，在 `/opt/tg-guard/.env` 添加或修改 `PANEL_BIND=0.0.0.0`，可选设置 `PANEL_HOST=服务器公网IP`，然后执行 `bash scripts/deploy.sh`。浏览器打开 `http://服务器公网IP:8080`，使用新生成的登录链接或 `.env` 中的 `ADMIN_API_TOKEN` 登录。云安全组和服务器防火墙需允许你的 IP 访问 TCP 8080。

公网 HTTP 不加密，长期使用建议配置 HTTPS；MySQL 和 Redis 仍不公开端口。后台「机器人管理员」中的地址仅用于机器人菜单展示，不会修改端口监听。

## 服务器管理菜单

重复执行一行安装命令即可打开中文菜单；已安装后也可直接执行 `sudo bash /opt/tg-guard/scripts/manage.sh`。

- 选择 **2**：开启 IP 访问 / 修改地址，输入公网 IPv4 或域名即可保存并应用。
- 选择 **3**：切回仅本机访问。
- 选择 **4**：生成新的后台登录链接，不重启或构建服务。
- 选择 **5 / 6 / 7**：更新、查看状态、查看最近日志。

菜单只修改后台访问设置，保留 Token、数据库密码和数据卷。IP 访问仍需安全组和防火墙放行；不会自动修改系统防火墙。

### v1.7.8 审查修复

外部频道身份的消息也进行本地规则和 AI 审核；违规时通过统一处罚流程删除并警告，保存频道身份和审核记录。不会将频道 ID 当成用户去禁言、封禁；本群匿名管理员和关联频道自动转发保持保护，普通转发广告仍审核。

已安装项目打开管理菜单不再强制联网更新；菜单更新使用当前安装目录。后台访问修改失败会还原 `.env` 并尝试恢复原服务，恢复失败时明确提示运行状态无法确认。上述流程已增加模拟和独立数据库回归测试。

历史处罚参与累计违规计数，审核记录关联人工反馈，清理时保留这些关联关系，不直接删除处罚表重置历史计数。

### v1.7.9 可靠性修复

- 同一验证答案的消息或按钮回调重试不会重复扣除尝试次数。
- 外部频道身份接入本地防刷屏，警告遵守自动删除开关和 AI 决策，不额外删除仅需警告的消息。
- 就绪检查包含 Telegram 接收状态和队列积压；连续请求失败、长时间失联或待处理消息积压超过 5 分钟时返回不可用。死信数量单独展示，便于管理员排查。
- Release 必须先通过完整 CI，包括真实 MySQL、Redis、竞态测试和源码镜像构建；发布后验证预编译程序镜像。
- MySQL 实例锁和迁移锁按数据库隔离，不同机器人的独立数据库可同时运行，同一数据库仍只允许一个实例。

数据保留默认 `RETENTION_DAYS=90`，每小时分批清理：已完成事件只保留去重信息，过期审核消息原文被清除，过期 AI 用量明细被删除。**清除的原文无法恢复，请按需备份；设置为 `0` 可关闭清理。** 处罚累计次数、审核结论、人工反馈关联的原文、未完成处罚及待处理/死信事件保留；审计元数据仍会增长。相关查询增加索引，Docker 每个服务的输出日志最多保留 3 个 10 MB 文件。

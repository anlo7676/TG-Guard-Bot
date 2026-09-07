# 部署与运维

首次部署推荐使用 [README 一键部署](../README.md#一键部署推荐)：只输入 Bot Token，其他凭据和依赖由脚本生成及启动。本文保留手工运维与故障恢复细节。

## 发布 Webhook

本地开发默认 Polling。生产使用带有效证书的 HTTPS 反向代理，将 `/telegram/webhook` 转发到应用；填写：

```dotenv
BOT_MODE=webhook
WEBHOOK_URL=https://bot.example.com/telegram/webhook
WEBHOOK_SECRET=replace_with_at_least_32_random_url_safe_characters
```

允许的 Secret 字符为 `A-Z a-z 0-9 _ -`，长度 32–256。应用启动注册 Webhook，保留 Telegram 待处理 Update。切回 Polling 时删除 Webhook，同样不丢弃待处理事件。Webhook 采用单连接投递来减少事件乱序，收到事件只执行持久化，业务在工作池运行。

管理 API 应限制访问来源；Compose 的默认 HTTP 端口只在宿主机回环地址可见。Webhook 和管理 API 可在代理层分别配置路径访问规则。不要将 `ADMIN_API_TOKEN` 放进公开页面或 URL。

## 验证首次联调

1. `GET /health/ready` 返回 200，应用日志显示正确 Bot username。
2. 将机器人设为测试超级群管理员，确认删除消息、限制成员权限。
3. 使用普通测试账号加入群，确认立即禁言并出现 Deep Link。
4. 用另一个账号打开验证链接，确认被拒绝。
5. 正确账号完成数学题，确认恢复群默认权限；再加入新测试账号不验证，确认 180 秒后踢出且可重新加入。
6. 验证过程中重启应用，确认成功恢复／超时任务仍可完成。
7. 发送普通技术讨论、带隐藏 Entity 的广告、重复文本，核对审核与处罚记录。
8. 启用 AI，回复消息 `/check`；普通用户点击处罚按钮应拒绝，管理员可操作。
9. 用管理员／白名单账号发送风险样本，确认没有自动处罚。
10. 新增关键词后检查优先级、开关和群隔离。

## 观察与排错

```sh
docker compose ps
docker compose logs --tail=200 app
docker compose logs --tail=100 mysql
docker compose logs --tail=100 redis
```

- `telegram error 400/403`：检查机器人管理权限、是否超级群、目标是否已退群，以及群或私聊是否仍可访问。
- `telegram error 429`：客户端依据 `retry_after` 等待，超出任务时限转为队列重试。
- `AI HTTP 4xx/5xx`：检查供应商 URL、Key、模型及 JSON mode / max_completion_tokens 支持；密钥和响应原文不会写日志。
- `AI response refused or incomplete`：拒绝或截断结果被安全丢弃；手动审核不处罚，自动审核仅保留本地规则决策。
- `resource is busy`：同一用户验证与处罚发生竞争，任务稍后自动重试。
- `another TG Guard instance is running`：本版只允许运行一个实例，内部工作池提供并行。
- `instance lock lost`：数据库连接或互斥锁丢失，进程停止以避免双实例执行。

## 死信与失败恢复

`GET /api/v1/queue/dead` 显示失败 5 次的 Update。先检查对应错误，修复权限／配置／网络，再通过受控数据库连接重放指定 Update：

```sql
UPDATE update_inbox
SET status='pending', attempts=0, available_at=UTC_TIMESTAMP(6), lease_until=NULL
WHERE update_id=待重放的ID AND status='dead';
```

审查原始事件时间和目标身份后再重放，避免把很久以前的管理员命令应用到当前状态。既有审核决策和完成的处罚步骤会复用；不要删除 `moderation_logs` 或 `punishments` 来强制重放。

验证恢复状态和错误可直接查看：

```sql
SELECT chat_id,user_id,status,recovery_attempts,last_error,next_attempt_at
FROM verification_sessions
WHERE status IN ('completing','expiring');
```

## 备份和数据保留

MySQL 卷保存群配置、成员、验证、审核和处罚数据；Redis 卷保存 AOF。使用数据库备份工具定期备份 MySQL，测试恢复流程，并妥善保管 `.env`。`docker compose down` 不删除数据卷；只有明确希望清空环境时才使用删除卷选项。

当前不自动删除审计数据。部署方可按自己的保留期归档已完成 `update_inbox` 原始负载、审核和 AI 日志，保留必要去重元数据；不要清除正在处理的队列和验证记录。直接删除历史收件箱会缩短去重能力，重放旧 Update 时需人工核验。

## 本地构建产物

```powershell
.\scripts\test.ps1
# 可执行文件：bin/tgguard.exe
```

Linux 可使用 `CGO_ENABLED=0 go build -trimpath -o bin/tgguard ./cmd/tgguard`。升级数据库结构时新增迁移文件，已在生产执行的迁移不可修改；迁移前先备份。MySQL DDL 无整体事务回滚，新增迁移应设计为可安全恢复的操作。

## 本机发布与版本核验

使用 `pwsh -File scripts/start-local.ps1` 统一构建和启动，`-Stop` 停止本项目进程。`scripts/test.ps1` 构建 next 文件，不覆盖运行文件。旧二进制保存在 `bin/tgguard-previous.exe`，数据库需要另行备份；不能将二进制备份误认为数据库备份。

`/health/live` 和 Web 后台展示运行源码指纹，机器人 `/version` 也可核对。升级后请重新发送 `/menu`，旧 Telegram 消息不会自动更新，旧版回调会提示刷新菜单。完整交付与限制见 acceptance-2026-09-06.md。

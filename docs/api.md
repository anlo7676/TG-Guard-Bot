# 管理 API

所有管理路由前缀为 `/api/v1`，接受 `Authorization: Bearer <ADMIN_API_TOKEN>` 或 Web 登录会话（写操作需要 `X-CSRF-Token`）。此凭据具有全局管理权限，不能嵌入公开前端。以 HTTPS 反向代理发布远程接口。

写入请求使用 `Content-Type: application/json`，最大 64 KiB；未知字段和尾随第二个 JSON 值返回 400。凭据错误返回 401，跨 Origin 写入返回 403，资源不存在返回 404，超出每客户端 IP 每分钟 120 次返回 429。服务不信任 `X-Forwarded-For`；反向代理下默认按代理 IP 共享限制。

## 路由

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/dashboard` | UTC 当日消息审核、删除、AI 调用／Token、死信及群／用户数量 |
| GET | `/groups` | 群列表 |
| GET / PUT | `/groups/{chat}/settings` | 查看／覆盖群设置，未提供字段保持当前值 |
| GET / POST | `/groups/{chat}/keywords` | 查询／新增关键词 |
| PUT / DELETE | `/groups/{chat}/keywords/{id}` | 替换／删除关键词，必须属于该群 |
| GET / POST / DELETE | `/groups/{chat}/lists` | 查询／增加／移除名单 |
| GET | `/groups/{chat}/users?q=文本` | 按用户 ID、username、昵称查询已记录成员 |
| GET | `/groups/{chat}/logs` | 审核记录、规则和 AI 结果 |
| GET | `/groups/{chat}/punishments` | 处罚计划、执行步骤、状态和错误 |
| GET | `/groups/{chat}/verifications` | 最近 100 条验证记录，不包含 Token 或答案 |
| GET | `/groups/{chat}/audits` | 群设置、名单、关键词等管理变更审计 |
| POST | `/groups/{chat}/feedback` | 记录误判反馈 |
| GET | `/queue/dead` | 重试耗尽的 Update 元数据，不返回原始消息负载 |

`chat` 必须为负数 Telegram 超级群 ID；只有名单接口允许 `chat=0` 表示全局名单。通用列表上限 100，可用 `?before=上一页最小ID` 翻页；群列表用 `chat_id`，用户列表用 `user_id`，死信用 `update_id`。关键词列表返回该群全部规则，验证列表仅最近 100 条。列表中的 MySQL JSON 列以 JSON 字符串返回，前端按需解析；群设置和关键词是结构化 JSON。

不存在独立 `/rules` 写接口：使用群设置的 `rules` 对象修改规则；AI Provider 连接信息通过 `/system` 配置并加密持久化。

## 系统设置和 Web 会话

- `GET /api/v1/system`：机器人身份和脱敏设置，Key 仅返回 `key_configured`。
- `PUT /api/v1/system`：完整保存 `super_admins`（数字 ID 数组）、`panel_url`、`ai`。AI 字段为 `enabled`、`base_url`、`model`、`api_key`、`timeout_seconds`、`max_tokens`、`token_parameter`。空 Key 保留，顶层 `clear_key=true` 显式清除，保存立即生效。
- `POST /api/v1/system/test-ai`：测试已保存连接，不使用缓存，会产生实际模型用量。
- `POST /auth/login`：JSON `{"token":"管理凭据"}`，成功设置 HttpOnly、SameSite=Strict Cookie，并返回 `csrf`，会话有效 8 小时。
- `GET /auth/session`：返回当前会话的 CSRF Token。
- `POST /auth/logout`：要求 Cookie 和 `X-CSRF-Token`，撤销会话。
- `POST /api/v1/panel-ticket`：已鉴权部署者获取 60 秒一次性票据；`POST /auth/ticket` 用 `{"ticket":"票据"}` 消费并建立会话。

首页和静态资源公开可读，业务数据需要鉴权。登录每 IP 每 5 分钟最多 10 次；写操作拒绝跨 Origin。前端不在 localStorage 保存管理凭据。HTTPS 反向代理需保留 Host，并设置匹配的 HTTPS `panel_url`。

## 更新群设置

`PUT /api/v1/groups/-1001234567890/settings`

```json
{
  "verification_enabled": true,
  "verification_timeout": 180,
  "verification_type": "math",
  "verification_fail_action": "kick",
  "ai_enabled": true,
  "ai_threshold": 50,
  "direct_threshold": 80,
  "ai_warn_confidence": 0.6,
  "ai_delete_confidence": 0.8,
  "ai_mute_confidence": 0.95,
  "auto_delete": true,
  "auto_warn": true,
  "auto_mute": true,
  "auto_ban": false,
  "mute_seconds": 3600,
  "mute_after": 2,
  "ban_after": 3,
  "review_access": "all",
  "rules": {
    "url": {"enabled": true, "score": 20},
    "telegram_link": {"enabled": true, "score": 40}
  }
}
```

`review_access` 可为 `all`、`admin`、`trusted`（管理员＋白名单／可信用户）。`verification_type` 可为 `math`、`button`；失败操作可为 `kick`、`ban`、`mute`。`language` 支持 `zh_CN`、`en_US`。所有时间戳存储和 API 展示均为 UTC；验证／禁言期限使用秒。

规则名称：`url`、`telegram_link`、`contact`、`mention`、`advertising`、`gambling`、`porn`、`crypto`、`caps`、`emoji`、`many_links`。未覆盖项使用程序默认评分。

## 新增／替换关键词

`POST /api/v1/groups/-1001234567890/keywords`

```json
{
  "keyword": "官网",
  "match_type": "contains",
  "reply_type": "text",
  "content": "我们的官网是 https://example.com",
  "priority": 100,
  "enabled": true,
  "reply": true
}
```

成功返回 `{"id":123}`。PUT 使用同一数据结构，为完整替换操作。`match_type` 可为 `exact`、`contains`、`starts_with`、`ends_with`、`regex`；正则使用 Go RE2，提交时校验。

`reply_type`：`text`、`HTML`、`MarkdownV2`、`photo`、`video`、`document`、`random`。媒体的 `content` 是 Telegram file_id 或可公开访问的 URL；`random` 使用每行一个候选文本，并按消息 ID 选择，重试保持选择稳定。

## 名单

`POST /api/v1/groups/-1001234567890/lists`，全局使用 `/groups/0/lists`：

```json
{
  "user_id": 123456,
  "username": "",
  "kind": "white",
  "reason": "已人工确认",
  "expires_at": null
}
```

`kind` 为 `white`、`black`、`trusted`。username 可带 `@`，服务会去掉前缀。建议使用稳定的 Telegram ID，username 可被用户更改或重新分配。删除时使用 DELETE 并提供相同的 `user_id`、`username`、`kind`。

## 误判反馈

`POST /api/v1/groups/-1001234567890/feedback`

```json
{"log_id":123,"note":"正常技术讨论，人工确认误判"}
```

反馈只记录证据，不自动解除处罚或修改全局策略。需要解除禁言／封禁时，由群管理员使用 `/unmute` 或 `/unban`。

## 健康检查和 Webhook

- `GET /health/live`：进程存活，不访问数据库。
- `GET /health/ready`：MySQL／Redis 可连接，失败返回 503；不代表 Telegram 或 AI 的外部连通性。
- `POST /telegram/webhook`：只在 Webhook 模式注册。要求 `X-Telegram-Bot-Api-Secret-Token`，最大 1 MiB，只有写入 MySQL 收件箱后才返回成功。

# Telegram 智能群管机器人项目需求说明书

## 一、项目名称

Telegram 智能群管与 AI 内容审核机器人

英文暂定名称：

TG Guard Bot

---

## 二、项目定位

本项目开发一套基于 Telegram Bot API 的智能群管理机器人，用于帮助 Telegram 群组自动完成新人验证、垃圾消息过滤、广告识别、AI 内容审核、关键词自动回复、违规处罚、黑白名单管理以及群管理配置等工作。

机器人主要面向 Telegram 大群、社区群、项目群、交易群、交流群等需要自动化管理的场景。

系统需要兼顾：

1. 自动化程度高
2. 误判率低
3. 多群独立配置
4. AI 审核能力
5. 高并发处理能力
6. 管理员操作方便
7. 后续可扩展 SaaS 商业化
8. 支持 Web 管理后台
9. 支持不同群使用不同审核策略
10. 保留完整审核和处罚日志

项目原则是：

“规则优先，AI 辅助，人工可干预。”

普通明确广告尽可能通过本地规则进行判断，模糊内容再调用 AI，从而降低 AI API 成本和审核延迟。

---

# 三、推荐技术架构

后端语言：

Python 3.12+

Telegram 框架：

aiogram 3.x

Web API：

FastAPI

数据库：

PostgreSQL

ORM：

SQLAlchemy 2.x

数据库迁移：

Alembic

缓存：

Redis

异步任务：

第一阶段可直接使用 asyncio。

后期任务量增加后，可以增加：

Celery / Dramatiq / ARQ

AI：

支持 OpenAI API 及 OpenAI Compatible API。

系统设计时不要与单一 AI 服务商强绑定。

部署：

Docker + Docker Compose

生产环境：

Nginx / Caddy + HTTPS + Webhook

开发环境：

Telegram Long Polling

---

# 四、系统整体架构

系统主要分为以下几层：

Telegram Bot 层

负责接收 Telegram Update，并执行 Telegram API 操作。

包括：

* 新成员入群
* 用户退群
* 普通消息
* 回复消息
* Bot 命令
* Inline Button
* Callback Query
* Chat Member Update
* 私聊消息

Telegram Handler 不直接承担复杂业务逻辑。

Handler 只负责：

接收事件 → 调用 Service → 根据返回结果执行操作。

---

业务 Service 层

负责具体业务。

主要 Service 包括：

VerificationService

负责新人验证。

ModerationService

负责消息审核流程。

AIRiskService

负责 AI 广告和违规内容判断。

KeywordService

负责关键词自动回复。

PunishmentService

负责警告、禁言、删除、踢出、封禁。

GroupService

负责群配置。

UserService

负责用户状态。

WhitelistService

负责白名单。

BlacklistService

负责黑名单。

AuditService

负责记录审核日志。

---

规则引擎层

负责本地快速内容判断。

例如：

* URL 检测
* Telegram 链接检测
* @username 检测
* 电话号码检测
* 钱包地址检测
* 广告词检测
* 招聘词检测
* 博彩词检测
* 色情推广检测
* 金融推广检测
* 重复消息检测
* 大量 Emoji
* 大量大写字符
* 大量链接
* 频繁发言
* 联系方式引流
* QR Code 内容
* 可疑用户名
* 可疑新用户

每条规则可以返回风险分。

最终得到 message risk score。

---

数据层

PostgreSQL：

保存长期数据。

Redis：

保存短期、高频状态。

例如：

* 验证 Token
* 验证状态
* 消息频率
* 用户发言计数
* 临时风控数据
* 群配置缓存
* 关键词缓存
* AI 审核缓存
* 限流状态

---

# 五、Telegram Bot 必要权限

机器人加入群后必须成为管理员。

机器人至少需要：

Delete Messages

用于删除广告和违规消息。

Ban Users

用于封禁或者踢出用户。

Restrict Members

用于新人验证前禁言以及违规禁言。

Invite Users

根据后续入群功能需要开启。

机器人还需要关闭 Privacy Mode。

通过 BotFather：

/setprivacy

选择机器人。

设置：

Disable

这样机器人才能接收普通群消息。

---

# 六、新人入群验证系统

## 6.1 功能目标

任何新用户进入开启验证功能的群后，默认不能立即发言。

用户必须完成私聊机器人验证。

通过验证以后机器人自动解除禁言。

如果超过规定时间没有完成验证，则根据群设置：

踢出用户

或者

保持禁言。

推荐默认：

验证失败或者超时 → 踢出群。

---

# 七、新人验证完整流程

用户加入 Telegram 群。

↓

机器人收到 chat_member 或 new_chat_members Update。

↓

查询当前群设置。

判断：

verification_enabled = true？

如果 false：

不处理。

如果 true：

继续。

↓

检查用户是否：

管理员

白名单

可信用户

机器人

如果属于以上类型：

跳过验证。

否则：

↓

机器人立即限制用户权限。

例如：

can_send_messages = false

↓

生成一次性验证 Session。

例如：

verify_token：

随机、安全、不可预测。

Redis：

verify:{token}

内容：

user_id

chat_id

created_at

expires_at

status

↓

机器人在群中发送欢迎信息：

欢迎 @username 加入本群。

请在 3 分钟内点击下方按钮完成安全验证。

按钮：

点击进行验证

链接：

https://t.me/BOT_USERNAME?start=verify_TOKEN

↓

用户点击。

Telegram 打开机器人私聊。

↓

用户点击 Start。

机器人收到：

/start verify_TOKEN

↓

机器人验证：

Token 是否存在

Token 是否过期

User ID 是否匹配

Group ID 是否有效

用户是否仍在群内

↓

如果合法：

展示验证码。

---

# 八、人机验证方式

系统需要预留多个验证 Provider。

第一版建议实现以下模式。

## 模式一：按钮验证

机器人显示：

请选择“我是人类”

下面出现多个按钮。

例如：

机器人

我是人类

跳过

用户点击正确按钮通过。

优点：

简单。

缺点：

容易被脚本模拟。

---

## 模式二：数学题

例如：

请输入：

7 + 5 = ?

用户输入：

12

正确通过。

---

## 模式三：图片验证码

机器人生成简单 CAPTCHA。

例如：

A7K3

用户输入。

---

## 模式四：Web CAPTCHA

后续可以使用 Telegram Mini App 或独立网页接 Cloudflare Turnstile。

安全级别最高。

第一阶段不一定需要。

---

# 九、验证成功

用户验证成功后：

1. 修改 Verification 状态：

verified

2. 调用 Telegram：

restrictChatMember

恢复发送消息权限。

3. Redis 删除验证 Session。

4. 保存数据库验证记录。

5. 群内可选提示：

@username 已完成验证。

6. 自动删除之前的验证提示。

---

# 十、验证超时

后台启动验证超时处理。

例如：

默认：

180 秒。

用户未完成验证。

执行：

banChatMember

然后：

unbanChatMember

效果：

将用户踢出群，但允许以后重新加入。

也可以配置：

永久 Ban。

---

# 十一、广告审核系统

这是系统核心模块。

每条普通群消息进入 Moderation Pipeline。

整体流程：

Message

↓

消息标准化

↓

群配置检查

↓

用户白名单检查

↓

内容规则引擎

↓

风险评分

↓

是否调用 AI

↓

最终判断

↓

处罚系统

↓

审核日志

---

# 十二、消息标准化

Telegram 消息可能包含：

text

caption

photo

video

document

sticker

forward

reply

entity

系统统一转换为：

ModerationMessage

包含：

chat_id

message_id

user_id

username

display_name

text

caption

urls

mentions

entities

reply_message

forward_source

media_type

created_at

---

# 十三、白名单系统

以下用户默认可以跳过部分审核：

Telegram 群管理员

机器人管理员

群白名单用户

全局白名单用户

可信用户

白名单 Telegram ID

白名单 username

管理员可配置：

管理员是否完全免审。

推荐：

Telegram 管理员跳过自动处罚，但仍可以记录风险。

---

# 十四、广告规则引擎

规则引擎输出：

RiskScore。

例如总分：

0 - 100。

每个 Rule：

检测消息。

返回：

matched

score

reason

metadata

例如：

URLRule：

检测到外部 URL。

风险：

+20

TelegramInviteRule：

检测：

t.me/xxxx

telegram.me/xxxx

joinchat

风险：

+40

ContactRule：

检测：

WhatsApp

微信

QQ

手机号

邮箱

风险：

+25

UsernamePromotionRule：

例如：

联系 @xxxx

风险：

+20

AdvertisingKeywordRule：

例如：

代理

推广

包赔

稳赚

日赚

兼职

接单

博彩

投注

棋牌

换U

空投

带单

风险：

+20～50

CryptoRule：

钱包地址

USDT

TRC20

ERC20

充值

兑换

场外

风险根据语境增加。

---

# 十五、风险等级

建议：

0～19

低风险。

直接放行。

20～49

中低风险。

根据群设置决定：

放行 / AI。

50～79

高风险。

调用 AI。

80～100

极高风险。

可以：

立即删除

或者

AI 快速复核。

默认建议：

规则非常明确时直接处理。

其他情况交给 AI。

---

# 十六、AI 内容审核

AI 负责判断无法通过简单规则确定的消息。

例如：

“兄弟几个最近做个项目，需要人，有兴趣私聊。”

本地规则很难确认。

AI 可以结合语义分析。

---

# 十七、AI 输入

不要直接让 AI 读取所有 Telegram 原始对象。

构造精简 JSON。

例如：

{
"text": "消息内容",
"has_url": true,
"urls": [],
"mentions": [],
"user_status": "new",
"risk_score": 55,
"matched_rules": [
"contact_method",
"promotion_keyword"
]
}

---

# 十八、AI 输出格式

强制要求 AI 返回 JSON。

例如：

{
"is_ad": true,
"confidence": 0.96,
"category": "financial_promotion",
"severity": "high",
"reason": "包含收益诱导和私聊联系方式",
"recommended_action": "delete"
}

字段：

is_ad

boolean

是否属于广告。

confidence

0～1

可信度。

category

广告分类。

例如：

normal

promotion

crypto

gambling

porn

recruitment

scam

traffic_diversion

external_group

financial

unknown

severity：

low

medium

high

critical

reason：

简短原因。

recommended_action：

allow

warn

delete

mute

ban

注意：

AI 只能给建议。

最终操作必须经过程序规则判断。

禁止：

AI 直接操作 Telegram。

---

# 十九、AI 判断策略

例如：

confidence < 0.60

放行但记录日志。

0.60～0.79

警告或进入观察状态。

0.80～0.94

删除。

> = 0.95 且严重违规

删除 + 禁言 / Ban。

所有阈值必须允许每个群单独配置。

---

# 二十、回复消息 @机器人进行 AI 审核

核心功能：

群成员可以回复一条消息，然后 @机器人，让机器人判断该消息是否为广告。

例如原消息：

有人需要 USDT 吗？便宜出，有需要联系 @xxxxx

用户回复：

@GuardBot 是广告吗

机器人检测：

当前 message 是否 reply_to_message。

如果没有：

提示：

请回复需要审核的消息后再 @机器人。

如果存在：

提取：

reply_to_message

↓

调用 AIReviewService

↓

返回：

审核结果。

例如：

AI 审核结果

判定：疑似广告

风险：96%

类型：加密货币推广

原因：消息包含交易招揽以及外部联系方式。

建议：删除。

---

# 二十一、谁可以使用 AI 审核

支持群设置：

所有人

仅管理员

管理员 + 可信用户

推荐默认：

所有群成员可以查询。

但只有管理员可以执行：

删除

封禁

禁言。

避免普通用户恶意操作。

---

# 二十二、AI 审核按钮

机器人返回结果下面可提供：

删除

禁言

封禁

误判

白名单

按钮。

权限验证：

点击按钮时检查 Telegram 用户是否管理员。

---

# 二十三、关键词自动回复

机器人支持群级关键词。

例如：

用户：

官网是什么？

触发：

官网

机器人：

我们的官网是 xxxx。

---

# 二十四、关键词匹配模式

支持：

exact

完全匹配。

contains

包含关键词。

starts_with

开头匹配。

ends_with

结尾匹配。

regex

正则表达式。

---

# 二十五、关键词回复类型

支持：

文本

Markdown

HTML

图片

视频

文件

链接按钮

多按钮

随机文本

Reply 当前消息

直接发群消息。

---

# 二十六、关键词优先级

keyword_rules：

priority

数值越高越优先。

如果多个规则同时匹配：

管理员可以配置：

只触发最高优先级。

或者：

触发全部。

推荐默认：

只触发最高优先级。

---

# 二十七、防刷屏系统

需要检测用户短时间大量发言。

Redis：

rate:user:{chat_id}:{user_id}

例如：

10 秒：

5 条消息。

30 秒：

10 条消息。

超过阈值：

警告。

再次超过：

禁言。

再犯：

Ban。

---

# 二十八、重复消息检测

Redis 保存用户最近 N 条消息 Hash。

如果用户连续发送：

同样内容

高度类似内容

重复链接

重复 Emoji

判断 Spam。

例如：

1 分钟重复 3 次：

删除。

重复 5 次：

禁言。

---

# 二十九、新用户风控

新加入用户的风险权重可以提高。

例如：

加入群 < 10 分钟。

风险：

+10

加入群后第一条消息包含 URL：

+30

第一条消息包含联系方式：

+30

第一条消息包含 Telegram 群链接：

+50

这样可以快速识别入群广告号。

---

# 三十、处罚系统

支持以下 Action：

ALLOW

允许。

DELETE

删除消息。

WARN

发送警告。

MUTE

禁言。

KICK

踢出。

BAN

永久封禁。

SHADOW_LOG

只记录，不执行处罚。

---

# 三十一、禁言时长

支持：

1 分钟

5 分钟

10 分钟

30 分钟

1 小时

6 小时

12 小时

1 天

3 天

7 天

永久。

---

# 三十二、违规次数系统

保存：

user_group_stats

例如：

warning_count

deleted_count

mute_count

ban_count

ad_count

spam_count

risk_score_total

last_violation_at

---

# 三十三、累计处罚策略

例如：

第一次广告：

删除 + 警告。

第二次：

删除 + 禁言 1 小时。

第三次：

Ban。

每个群可以修改策略。

---

# 三十四、群设置

每个群必须拥有独立设置。

group_settings：

verification_enabled

verification_timeout

verification_type

verification_fail_action

moderation_enabled

ai_moderation_enabled

ai_threshold

ad_detection_enabled

spam_detection_enabled

keyword_reply_enabled

delete_ad

warn_ad

auto_ban

new_member_protection

admin_bypass

log_channel

language

timezone

等等。

---

# 三十五、机器人管理员命令

建议支持：

/start

机器人说明。

/help

帮助。

/settings

群设置。

/verify

验证状态。

/stats

群统计。

/whitelist

白名单。

/blacklist

黑名单。

/keywords

关键词。

/rules

规则。

/warn

警告用户。

/mute

禁言。

/unmute

解除禁言。

/ban

封禁。

/unban

解除封禁。

/id

查看用户、群、消息 ID。

/check

AI 审核。

---

# 三十六、管理员快捷操作

管理员回复用户消息：

/ban

机器人封禁该用户。

回复：

/mute 1h

禁言 1 小时。

回复：

/warn

警告。

回复：

/ai

AI 判断。

---

# 三十七、管理员权限系统

需要区分：

Telegram Owner

Telegram Admin

Bot Super Admin

Bot Group Admin

Trusted Moderator

Super Admin：

机器人项目拥有者。

权限最高。

可以：

查看所有群

管理所有配置

设置全局规则

设置全局黑名单

管理 AI

查看系统日志。

---

# 三十八、数据库设计

核心表包括：

users

字段：

id

telegram_user_id

username

first_name

last_name

is_bot

global_status

created_at

updated_at

---

groups

id

telegram_chat_id

title

username

owner_id

status

member_count

created_at

updated_at

---

group_members

id

group_id

user_id

role

joined_at

verified_at

left_at

status

trust_score

warning_count

---

group_settings

id

group_id

verification_enabled

verification_timeout

moderation_enabled

ai_enabled

ai_threshold

spam_enabled

keyword_enabled

auto_delete

auto_mute

auto_ban

settings_json

---

verification_sessions

id

token

group_id

user_id

status

challenge_type

attempts

expires_at

verified_at

created_at

---

keyword_rules

id

group_id

keyword

match_type

reply_type

reply_content

priority

enabled

created_by

created_at

updated_at

---

whitelists

id

group_id

user_id

username

type

reason

created_by

created_at

---

blacklists

id

group_id

user_id

username

scope

reason

created_by

expires_at

created_at

---

moderation_logs

id

group_id

user_id

message_id

message_text

risk_score

matched_rules

ai_used

ai_result

decision

action

created_at

---

punishments

id

group_id

user_id

type

reason

duration

source

moderation_log_id

expires_at

created_by

created_at

---

ai_usage_logs

id

group_id

model

input_tokens

output_tokens

cost

latency

result

created_at

---

# 三十九、Redis Key 设计

例如：

验证：

verify:{token}

群配置：

group:settings:{chat_id}

关键词：

group:keywords:{chat_id}

用户限流：

rate:{chat_id}:{user_id}

重复消息：

spam:messages:{chat_id}:{user_id}

用户临时风险：

risk:{chat_id}:{user_id}

AI 缓存：

ai:moderation:{message_hash}

管理员缓存：

group:admins:{chat_id}

---

# 四十、AI 缓存

相同文本不应该不断调用 AI。

消息进行 Normalize。

然后：

SHA256。

生成：

message_hash。

Redis：

ai:moderation:{hash}

TTL：

例如：

24 小时。

如果同样内容再次出现：

直接使用缓存结果。

这样可以明显降低 AI 成本。

---

# 四十一、消息审核 Pipeline

最终推荐设计：

Telegram Message

↓

MessageNormalizer

↓

ContextLoader

↓

WhitelistFilter

↓

SpamDetector

↓

RuleEngine

↓

RiskScorer

↓

AI Decision Gate

↓

AI Moderation

↓

DecisionEngine

↓

PunishmentService

↓

AuditLogger

---

# 四十二、Decision Engine

整个项目最重要的思想之一：

规则和 AI 不直接执行处罚。

所有结果进入 DecisionEngine。

例如输入：

risk_score = 78

ai_ad = true

ai_confidence = 0.93

user_warning_count = 1

user_is_new = true

group_auto_ban = false

DecisionEngine 输出：

action = DELETE

secondary_action = MUTE

duration = 3600

reason = advertising

然后：

PunishmentService 执行。

---

# 四十三、Web 管理后台

后续建议增加 Web 管理平台。

前端：

Vue 3 / React / Next.js 均可。

如果希望开发效率高：

推荐 Vue 3 + Element Plus。

后台功能：

Dashboard

群数量

用户数量

今日消息

今日删除消息

广告数量

AI 调用量

AI 成本

Ban 数量。

---

# 四十四、群管理页面

显示：

群名称

成员数

验证状态

审核状态

AI 状态

今日消息

今日违规

设置。

---

# 四十五、审核记录页面

管理员可以查看：

消息

用户

时间

风险分

命中规则

AI 判断

最终处理。

支持：

标记误判。

如果误判：

加入 AI 反馈数据集。

---

# 四十六、用户管理页面

搜索 Telegram：

ID

username

昵称。

查看：

加入群时间

验证记录

广告记录

警告

禁言

Ban

风险等级。

---

# 四十七、关键词管理

管理员可以：

新增

修改

删除

开启

关闭

测试关键词。

---

# 四十八、规则管理

可以设置：

URL 是否允许

Telegram 链接是否允许

联系方式是否允许

招聘广告是否允许

币圈内容是否允许

博彩是否允许

色情推广是否允许。

每项规则设置：

enabled

score

action。

---

# 四十九、AI 设置

后台可以配置：

Provider

API Base URL

API Key

Model

Temperature

Timeout

最大 Token

是否启用 AI

AI 风险阈值。

API Key 必须加密存储。

禁止前端直接读取完整 API Key。

---

# 五十、日志群

每个群可以设置一个 Log Channel / Log Group。

发生：

删除

禁言

Ban

AI 判断

验证失败

管理员操作

时自动发送日志。

例如：

广告已删除

用户：@example

ID：123456

风险：92

AI：广告

原因：包含 Telegram 外部群链接及联系方式。

操作：删除 + 禁言 1h

---

# 五十一、操作审计

所有管理员操作都记录。

例如：

admin_id

action

target_user_id

group_id

old_value

new_value

timestamp

用于防止管理员滥用权限。

---

# 五十二、国际化

系统设计支持：

简体中文

繁体中文

英语

其他语言。

不要把所有 Bot 文本直接写死在 Handler。

使用：

i18n/

例如：

zh_CN.json

en_US.json

---

# 五十三、安全设计

验证 Token：

使用 secrets.token_urlsafe。

禁止：

可预测 ID。

所有 Callback Data：

需要检查：

用户身份

群身份

权限

有效期。

Web API：

JWT / Session。

后台需要：

CSRF

Rate Limit

HTTPS

密码 Hash。

推荐：

Argon2 / bcrypt。

AI API Key：

加密保存。

---

# 五十四、Rate Limit

Bot API 调用需要统一限流。

避免因为群消息过多导致 Telegram Flood Control。

对：

delete

ban

restrict

send_message

进行统一 Telegram API 调度。

---

# 五十五、异常处理

Telegram 常见异常：

用户已经退群

机器人无管理员权限

消息已经删除

消息不存在

用户无法被限制

API Flood Wait

网络错误。

必须统一异常处理。

不能因为一条消息异常导致整个 Update Consumer 停止。

---

# 五十六、日志系统

使用：

structlog 或标准 logging。

推荐 JSON 日志。

字段：

timestamp

level

module

chat_id

user_id

message_id

action

request_id

exception。

---

# 五十七、性能目标

第一阶段目标：

支持：

100+ Telegram 群。

数万用户。

每秒几十到数百条消息。

系统采用全异步架构。

避免：

同步 HTTP 请求

同步数据库调用

同步 AI 调用阻塞 Event Loop。

---

# 五十八、项目目录建议

tg_guard_bot/

app/

bot/

handlers/

start.py

join.py

verification.py

messages.py

moderation.py

keywords.py

admin.py

callback.py

middlewares/

database.py

redis.py

rate_limit.py

logging.py

filters/

admin.py

reply.py

mention.py

keyboards/

verification.py

moderation.py

admin.py

services/

verification_service.py

moderation_service.py

ai_service.py

keyword_service.py

punishment_service.py

group_service.py

user_service.py

whitelist_service.py

blacklist_service.py

audit_service.py

rules/

base.py

url_rule.py

telegram_link_rule.py

contact_rule.py

keyword_rule.py

crypto_rule.py

gambling_rule.py

spam_rule.py

duplicate_rule.py

scoring.py

models/

user.py

group.py

group_member.py

group_setting.py

verification.py

keyword.py

moderation.py

punishment.py

repository/

user_repository.py

group_repository.py

keyword_repository.py

moderation_repository.py

api/

routers/

groups.py

users.py

keywords.py

moderation.py

settings.py

auth.py

schemas/

core/

config.py

database.py

redis.py

logging.py

security.py

exceptions.py

i18n/

tasks/

tests/

migrations/

Dockerfile

docker-compose.yml

.env.example

pyproject.toml

README.md

---

# 五十九、开发阶段划分

## 第一阶段：项目基础

完成：

项目初始化

aiogram

FastAPI

PostgreSQL

Redis

SQLAlchemy

Alembic

Docker

配置系统

日志系统。

---

## 第二阶段：群管理基础

完成：

机器人进群

群注册

管理员同步

用户入群

成员记录

权限检测。

---

## 第三阶段：新人验证

完成：

入群禁言

Deep Link

私聊验证

验证码

验证成功解除限制

验证超时踢出

验证日志。

---

## 第四阶段：广告规则引擎

完成：

Message Normalizer

Rule Engine

URL 检测

TG 链接

联系方式

广告关键词

风险评分

删除

警告

禁言。

---

## 第五阶段：AI 审核

完成：

AI Provider 抽象

OpenAI Compatible API

Structured JSON

AI 缓存

Decision Engine

AI Usage Log。

---

## 第六阶段：回复消息 AI 审核

完成：

reply + @bot

AI 判断

审核结果

管理员操作按钮。

---

## 第七阶段：关键词系统

完成：

关键词 CRUD

匹配模式

优先级

Redis Cache

自动回复。

---

## 第八阶段：Spam 系统

完成：

频率限制

重复消息

相似文本

新用户风控

累积处罚。

---

## 第九阶段：Web 管理后台

完成：

登录

Dashboard

群管理

用户管理

审核日志

关键词

规则

AI 设置。

---

## 第十阶段：商业化能力

如果未来 SaaS 化：

增加：

套餐

订阅

群数量限制

AI Token 配额

消息审核配额

Premium 功能

支付

API

Webhook

团队账号。

---

# 六十、第一版 MVP 范围

第一版不要一次实现所有高级功能。

推荐 MVP：

1. Bot 入群和管理员权限检测。

2. 新用户进入后自动禁言。

3. 私聊机器人完成验证。

4. 验证成功自动解除禁言。

5. 验证超时自动踢出。

6. 普通消息广告规则检测。

7. Telegram 链接、URL、联系方式检测。

8. 广告关键词系统。

9. 消息风险评分。

10. 高风险消息 AI 判断。

11. 自动删除广告。

12. 自动警告用户。

13. 自动禁言用户。

14. 回复某条消息并 @机器人进行 AI 判断。

15. 群关键词自动回复。

16. 白名单。

17. 黑名单。

18. 群独立设置。

19. 审核日志。

20. PostgreSQL + Redis。

做到以上功能以后，已经是一套完整可用的 Telegram AI 群管机器人。

---

# 六十一、项目核心原则

整个代码必须遵守：

Handler 只处理 Telegram Event。

业务放在 Service。

数据库操作放 Repository。

消息判断放 Rule Engine。

处罚统一放 PunishmentService。

最终决定统一放 DecisionEngine。

AI 只负责分析，不直接执行操作。

Redis 负责高频临时状态。

PostgreSQL 负责持久化数据。

所有敏感操作保留日志。

所有群设置必须做到互相独立。

---

# 六十二、最终产品效果

用户加入群：

进入群

→ 自动禁言

→ 点击验证

→ 私聊 Bot

→ 完成人机验证

→ 自动解除禁言。

普通用户发消息：

消息

→ 本地规则

→ 风险评分

→ 必要时 AI

→ 正常消息放行。

广告消息：

消息

→ 风险检测

→ AI 判断

→ 删除

→ 警告 / 禁言 / Ban

→ 保存审核日志。

人工发现可疑消息：

回复消息

→ @机器人

→ AI 分析

→ 返回广告概率、分类、原因

→ 管理员可以一键删除、禁言或封禁。

用户咨询：

“官网”

↓

命中关键词。

↓

机器人自动回复官网信息。

管理员：

通过 Telegram 命令或 Web 后台管理：

验证规则

广告规则

AI

关键词

白名单

黑名单

处罚

审核历史

用户。

最终形成：

“新人验证 + 自动审核 + AI 判断 + Spam 防护 + 自动处罚 + 关键词客服 + Web 管理后台”

一体化 Telegram 智能群管理平台。

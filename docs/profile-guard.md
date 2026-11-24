# 简介与头像广告拦截

在 Web 管理面板的「群策略」中开启「简介与头像广告拦截」，填写小写域名（如 `heiying.co`）。此策略独立于消息审核和累计封禁开关，命中即加入本群黑名单并封禁；处罚仍经过管理员、白名单、可信用户保护与持久化恢复流程。

- 入群、首次发言检查；后续发言使用最多 5 分钟的资料缓存。资料读取失败缓存 1 分钟，获取失败不会判定为广告。
- 简介匹配完整域名及其子域名，兼容大小写、全角字符、零宽字符；不会匹配 `notheiying.co`、`heiying.co.evil.com` 等不同域名。
- 开启头像匹配后，仅从简介域名命中的非受保护用户学习头像。样本按群隔离，使用解码后像素 SHA-256，比对当前可见头像；没有头像不构成匹配。不进行相似人脸/头像推断，裁剪、改色或重新压缩导致像素变化可能无法命中。
- 普通退出、验证超时和一般移除记录不会自动成为头像样本。移除域名会停用从该域名学习的头像样本。
- Telegram 可能不返回用户简介或头像。Bot API 无法枚举群全部成员，只能清理机器人已记录的用户，遗漏用户在后续入群、发言时检查。
- 封禁记录与黑名单原因包含 `profile_domain:` 或 `profile_avatar:`。撤销需同时解除 Telegram 封禁、删除群黑名单；仍命中策略的账号再次入群/发言会再次被拦截，可按需添加白名单并移除对应黑名单。
- 头像样本纳入数据库备份。增加样本表后，旧版本 Web 备份因表结构不同不能直接恢复到新版；跨版本恢复应使用对应版本与服务器备份流程。

## 存量检查接口

使用现有后台管理员认证调用 `POST /api/v1/groups/{chat}/profile-check`：

```json
{"user_id":123456789,"apply":false,"learn":false}
```

默认只检查并返回是否命中、简介/头像可读状态、实时群成员状态和保护状态。`learn:true` 仅保存简介命中的头像样本；`apply:true` 重新读取资料，只有仍在群内且命中的非受保护用户才写入黑名单并执行封禁。样本学习和执行处罚需显式指定，未授权群拒绝操作。每次请求只处理一人，不启动第二个 Telegram 轮询进程。

批处理工具：`scripts/profile-cleanup.py`。通过环境变量 `ADMIN_API_TOKEN` 读取凭据，输出 JSONL 报告（不含 Token 或完整简介）。先 `seed` 建立样本，再 `preview` 检查候选，最后 `apply --report preview.jsonl` 重新核验并处理预览中仍在群的命中者。`--scope left` 可只检查有离群记录的用户；默认检查所有已记录用户。报告包含成员 ID，应保存在仅管理员可读目录。

```sh
python3 scripts/profile-cleanup.py --chat=-1001234567890 --mode seed --scope left --output seed.jsonl
python3 scripts/profile-cleanup.py --chat=-1001234567890 --mode preview --output preview.jsonl
python3 scripts/profile-cleanup.py --chat=-1001234567890 --mode apply --report preview.jsonl --output applied.jsonl
```

工具使用本机 `http://127.0.0.1:8080`，不接受远程明文传输凭据。执行前应保留数据库备份，命中清单由操作人核对后执行。读取失败会记录为错误，不能计入“已清理”。

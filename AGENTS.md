# 项目约定

- 使用中文沟通，以 UTF-8 编码读取和修改文件。
- 技术栈固定为 Go 1.26.2、MySQL 8.4、Redis 8.6.2。
- Handler 负责事件路由；业务放在 service，数据库操作放在 store，本地分析和最终决策放在 rules，Telegram 处罚统一经过 Service.Punish。
- 修改后先审查，运行相关测试和 go vet，再生成本地 Git 提交。当前用户要求先本地，不推送；只有用户后续提供推送要求时再推送。
- 提交信息不包含 AI 署名、Co-Authored-By、Generated with 或类似标记。
- 不提交 .env、真实 Token、API Key、数据库密码和生成的二进制文件。
- 集成测试只能使用名称以 _test 结尾的独立数据库；测试会清空该测试数据库中的项目表。

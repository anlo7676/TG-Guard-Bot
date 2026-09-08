# 备份与恢复

Linux 管理菜单「9. 立即备份」生成配置、MySQL 和 Redis 归档；「10. 启用每日自动备份」安装 systemd 定时器。需要在已启动的 Git 项目目录中执行，自动备份需要 root/sudo。Windows 可以通过 SSH 在 Linux 服务器使用这些功能。

也可运行 `bash scripts/backup.sh` 和 `sudo bash scripts/backup.sh --install`。定时任务为每天 UTC 03:30（北京时间 11:30）加最多 5 分钟随机延迟，关机错过后补执行。启用定时器不会立即备份；请先执行一次手动备份并确认成功。使用 `systemctl list-timers tg-guard-backup.timer` 查看下次时间，`journalctl -u tg-guard-backup.service` 查看结果。停用使用 `systemctl disable --now tg-guard-backup.timer`。

归档位于 `backups/tg-guard-时间-进程号.tar.gz`，目录 0700，文件默认 0600，包含：

- `config.env`：配置与加密密钥；必须与数据库一起保留，切勿上传公开仓库。
- `mysql.sql.gz`：业务数据的事务一致性逻辑备份。
- `redis.rdb`：通过 Redis 复制协议生成并检查完整性的快照。
- `revision.txt`、`SHA256SUMS`：代码提交和各文件校验值。

任务会排除与项目更新、其他备份同时运行的情况。所有步骤成功后才发布最终归档；之后清理超过 30 天的本脚本归档。失败保留已有备份，systemd 标记失败。备份没有内置第三方上传或通知服务，请将归档定期复制到独立设备/存储并监控任务失败；同机备份无法抵御磁盘损坏。

## 恢复演练

先在私有临时目录解压并运行 `sha256sum -c SHA256SUMS` 和 `gzip -t mysql.sql.gz`。使用同一 MySQL 8.4 实例中的独立 `_test` 数据库或隔离实例导入 `mysql.sql.gz`，核对表数量及关键配置。不要对生产数据库运行清表式测试；只删除明确创建的测试数据库。Redis 可以使用 `redis-check-rdb` 校验，不需要覆盖线上数据。

## 灾难恢复

先停止应用和升级/备份任务，保留现有数据另作副本，准备与 `revision.txt` 对应的程序版本。将 `config.env` 恢复为 `.env`（0600），在隔离环境完成 MySQL 导入。不要向有业务数据的库直接导入覆盖。

MySQL 与 Redis 的快照是顺序生成，无法保证跨系统同一时间点。Redis 含会话和临时状态，恢复旧快照可能恢复旧登录会话；应在上线前清理过期会话并核对验证、处罚及队列。Redis 开启 AOF 时，直接替换 dump.rdb 不会覆盖现有 AOF，需使用新的隔离数据卷验证恢复流程。确认待执行任务不会产生过期处罚或重复通知后再连接 Telegram；恢复并不自动执行。

备份不包含 Telegram 历史消息、程序镜像、Caddy 证书、外部文件或云配置；这些由相应系统单独保管。

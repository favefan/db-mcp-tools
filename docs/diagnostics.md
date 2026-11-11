# 日志与远程诊断

设置 → 通用 → 诊断日志、访问日志页和托盘右键菜单均可“导出诊断包”。它是一个 `dbmcp-diagnostics-<时间>.json` 文件，可直接交给维护者分析，无需复制整个数据目录。导出前尽量重现问题，并记下发生时间和操作。

客户端通过当前 Admin 连接采集实际 Server 的快照，远程部署仍使用既有 SSH 隧道。Server 无法连接、凭据不可用或版本不支持时，仍保存本地日志，并在 `server_status`、`issues` 中注明缺失阶段。实际 Server 主机也可执行：

```sh
dbmcp --data-dir <实际数据目录> diagnostics --out <新文件路径>.json
```

CLI 在停机时也能收集已有日志，不会打开或迁移业务 SQLite。CLI 只新建文件，桌面导出拒绝写入应用配置和已连接本机 Server 的数据目录。无需把管理员 Token 放在命令行或发给维护者。

## 存储位置

| 组件         | Windows                                            | macOS                              | Linux                                                                              |
| ------------ | -------------------------------------------------- | ---------------------------------- | ---------------------------------------------------------------------------------- |
| 客户端       | `%LOCALAPPDATA%\local.dbmcp.tools\logs`            | `~/Library/Logs/local.dbmcp.tools` | `$XDG_DATA_HOME/local.dbmcp.tools/logs` 或 `~/.local/share/local.dbmcp.tools/logs` |
| CLI / Bridge | `%LOCALAPPDATA%\DBMCP\logs`                        | `~/Library/Logs/DBMCP`             | `$XDG_STATE_HOME/dbmcp/logs` 或 `~/.local/state/dbmcp/logs`                        |
| Server       | `<实际数据目录>\logs`                              | `<实际数据目录>/logs`              | `<实际数据目录>/logs`                                                              |
| 访问审计     | `<实际数据目录>\state.sqlite` 的 `audit_events` 表 | 同左                               | 同左                                                                               |

Windows 默认个人数据目录为 `%LOCALAPPDATA%\DBMCP\server`，未指定目录的服务使用 `%PROGRAMDATA%\DBMCP\server`。服务账户与桌面账户可能不同；管理接口采集的是 Server 进程自己的日志目录。

运行日志采用 JSON Lines。Go 文件为 `server-<PID>.jsonl`、`cli-<PID>.jsonl`、`bridge-<PID>.jsonl`，桌面文件为 `desktop-<PID>.log`。每个进程单文件 2 MiB，最多三个轮转文件；启动时清理超过七天或超过最近 32 个非活动文件的记录，活动进程持有锁以免被其他实例清理。访问审计仍默认保留 30 天。旧 `server.log` 只作后台启动的兼容 stderr 捕获，不进入新诊断包。

Windows 1.0.4 安装目录迁移另写入客户端日志目录的 `installer-migration.log`，使用 UTF-8 JSON Lines，记录 UTC 时间、版本、源／目标目录、拒绝原因、旧卸载器退出码和完成阶段；超过 1 MiB 后下次写入重新开始。一键导出的 `desktop_logs` 同时包含此文件，可区分目录冲突、旧卸载器缺失、清理失败和成功迁移。

## 诊断内容与定位

- 版本、源码修订、OS / 架构、时间、PID、WebView 版本、实际数据和日志目录。
- 主窗口隐藏、重建、轻量模式计时 / 取消 / 释放事件；导出时附实际轻量状态、托盘勾选与剩余计时，区分自动偏好与即时切换。
- Server 配置加载、状态打开、监听、TLS、就绪、维护和关闭阶段；CLI / 服务管理操作及进程结果。
- 客户端管理请求、Bridge 调用、HTTP 状态和耗时；`trace_id` 贯穿客户端 → Server。数据库操作还有 `request_id`、实体 / 实例 / 凭据 ID、策略版本和请求摘要。
- 数据库错误的发生阶段、类型链、网络错误 / errno、驱动 SQLSTATE 或错误号。原始驱动和 Parser 报错可能含行值或 SQL 字面量，因此只保留安全错误信息、指纹和已知原因码。
- 最近最多 1000 条审计摘要、实体启用与凭据撤销 / 到期状态、登记状态、连接端点和不含替换值的策略快照，解释权限、审批及 Mask 配置。
- 前端未捕获异常、Promise 拒绝和 React 渲染失败的指纹、文件与行列；Rust / Go panic 的位置和栈，不含 panic 参数值。客户端构建保留隐藏 source map，交付目录按资产哈希保存映射，便于还原生产位置。

不导出密码、Token、Authorization、密钥、凭据文件、SQLite 副本、原始 SQL / 参数、结果行或 Mask 替换值。采集只读允许的普通日志文件，不跟随符号链接。每目录最多 16 个最新文件、合计 8 MiB，每行最多 64 KiB；截断、损坏行和读取失败会注明。配置快照最多各取 100 个实例、实体和登记。

访问日志的“导出该请求”另行导出当前请求详情，包含管理员主动查看的 SQL、参数和关联事件。请求详情在 Server 状态库中加密保存并保留 30 天，详情接口仅允许管理员访问；它不属于诊断包的采集范围，不改变上述诊断内容限制。执行结果只保留摘要，未持久化原始结果行。

维护者先看版本、时间和采集状态，再用 `trace_id` 串起请求，以阶段和错误码区分配置、认证、策略、连接、执行和持久化问题；`request_id` 继续关联审批、恢复和审计。缺失、截断或已经轮转的证据需要在实际主机重新复现后导出。诊断只保存文件，不会自动上传或联系外部服务。

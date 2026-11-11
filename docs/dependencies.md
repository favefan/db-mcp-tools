# 依赖与实现限制

完整版本以 server/go.mod、client/src-tauri/Cargo.lock 和 pnpm-lock.yaml 为准。源码采用 MIT，第三方许可与来源见 [third-party](../third-party/README.md)。

## 主要依赖

| 能力         | 选择                                        | 用途与约束                                             |
| ------------ | ------------------------------------------- | ------------------------------------------------------ |
| SQL AST      | Bytebase Omni，固定提交 e500bc714251        | 各方言解析与字段来源分析；未知节点拒绝，不靠关键词放行 |
| MCP          | 官方 Go SDK                                 | Streamable HTTP 与 STDIO Bridge；每个请求独立认证      |
| 代理来源     | realclientip-go                             | 只对显式可信代理解析转发链，真实 peer 先校验           |
| SQLite       | modernc.org/sqlite、goose                   | WAL、迁移、条件认领与在线备份；数据目录独占锁          |
| PostgreSQL   | pgx stdlib                                  | database/sql 驱动；基础扩展元数据与非执行 Explain      |
| MySQL        | go-sql-driver/mysql                         | 参数绑定、连接池与目录发现                             |
| SQL Server   | go-mssqldb                                  | 命名参数、OUTPUT 与多数据库访问                        |
| DM8          | gitee.com/chunanyong/dm，可选 dm 构建标签   | 原生 database/sql 接口，默认构建省略                   |
| CLI / 服务   | Cobra、kardianos/service                    | CLI、后台进程与平台系统服务                            |
| 配置与文件锁 | go-toml/v2、toml_edit、flock                | Go 部署配置、外部 TOML 注释保留与互斥                  |
| 桌面         | Tauri 2、keyring、reqwest                   | 凭据、本机集成和 Admin 白名单代理，不打开业务 SQLite   |
| 前端         | React、TanStack Query、React Hook Form、Zod | 页面、服务状态与表单                                   |
| 交互组件     | Radix、Headless Tree、Lucide                | 弹窗 / Tabs、对象树多选与键盘交互、图标                |
| 版本比较     | semver                                      | 最低 Server 版本与随包升级判断                         |
| 日志         | slog、lumberjack、tauri-plugin-log          | 按 PID 分文件、滚动与诊断导出                          |
| 打包         | archiver、Tauri NSIS 工具                   | 独立包、Windows 安装器和校验清单                       |

## SQL 与数据库边界

支持常用 SELECT / DML、读 CTE、别名、JOIN、子查询及字段来源检查；写 CTE、多语句、未知来源和不支持语法在统一管线明确拒绝或按写入策略处理。跨数据库联查、任意存储过程、任意 SQL 改写和完整跨方言血缘分析不属于当前范围。

实例按精确数据库分池，共享实例并发上限。数据库发现、对象范围、SQL 检查与执行前重检使用同一授权。Mask 由用户显式配置，实例强制规则与实体追加规则经过统一结果出口。

PostgreSQL Explain 只返回非执行计划与估算；其他方言不开放 Explain 工具。PostGIS / TimescaleDB 仅提供基础识别与展示，不扩展专用工具集。对象树按库读取完整目录，不提供虚拟滚动与树内搜索；目录很大时优先选择整库或模式。

DM 驱动不能由客户端强制验证 TLS，只接受显式关闭 TLS 的本机 / 受保护隧道连接；不要将其当作可验证的 TLS 支持。该模块未提供可确认的独立分发授权，默认公开包不包含 DM，显式 --dm=native 生成本地用途的 dm-local 包。项目 MIT 不覆盖上游驱动许可。

## 平台与配置

桌面需要对应 OS 的原生依赖、图形会话和系统凭据存储，安装步骤见 [开发指南](development.md)。Server 可交叉编译，桌面必须在目标 OS 原生构建。GitHub CI 仅使用 Windows x64。

外部 MCP 配置使用预览指纹、受限备份和同目录原子保存；TOML 保留无关注释，JSON 保留无关值并按两空格排版。其他进程不共享项目文件锁，因此采用乐观冲突检查，不宣称能锁住外部编辑器。移除只处理语义哈希一致的自身条目。

备份包含 SQLite 一致性快照、配套密钥、凭据与哈希清单；恢复要求停机。配置导入不含秘密，只创建停用的新对象，不覆盖现有状态。详情见 [架构](architecture.md) 和 [使用指南](user-guide.md)。

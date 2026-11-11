# 技术选择与参考来源

优先使用成熟依赖和系统 API，以小范围适配形成个人工具；不复制参考项目的完整实现，不引入第二套业务状态或通用框架。完整依赖版本以锁文件为准。

## 参考项目

- [CC Switch](https://github.com/farion1231/cc-switch)：参考桌面卡片、设置、托盘、轻量模式及配置集成的用户流程；本项目保留独立 Server、权限与审批管线，不依赖该项目运行。
- [Bytebase Omni](https://github.com/bytebase/omni)：复用 PostgreSQL、MySQL、SQL Server、Oracle 方言 AST；固定提交，通过薄适配提取表与列来源。Oracle 解析器只用于 DM 常用语法适配，不代表支持全部 DM 语法。

## 实现选择

Go 使用标准库 net/http、database/sql、slog，配合官方 MCP SDK、Cobra、goose 与成熟服务库。SQLite 持久化，不引入 ORM、Redis、消息队列、外部 IAM 或 DI 容器。

桌面使用 Tauri 2、React、Radix 和 Headless Tree。对象树复用异步缓存、多选、ARIA 和键盘能力；业务策略与数据库执行仍在 Go 中。React Hook Form / Zod 管理表单，TanStack Query 管理服务状态，semver 负责版本语义。

外部 TOML 使用 toml_edit 保留注释；文件更新采用备份、预览指纹和原子替换。配置格式参考 [Codex MCP 文档](https://developers.openai.com/codex/mcp)、[Codex 配置参考](https://developers.openai.com/codex/config-reference) 和 [Claude Code MCP 文档](https://code.claude.com/docs/en/mcp)。

DM 使用原生 database/sql 驱动，参考 [DM 原生接口指南](https://eco.dameng.com/document/dm/zh-cn/app-dev/go_dm.html)。[ODBC 指南](https://eco.dameng.com/document/dm/zh-cn/app-dev/go_odbc.html) 仅作为替代路线资料，当前不同时实现两套驱动管理。正常使用 Go Modules，不向 GOROOT 复制源码或关闭模块校验；许可和 TLS 限制见 [依赖说明](dependencies.md)。

## 来源与许可

新增依赖说明用途、许可及平台影响。提取上游代码时附固定提交、修改说明与原始许可，不把设计参考等同于代码复制。分发包收集实际依赖清单、许可文本和精确源码地址；补充文本由 third-party/notices.json 记录来源和哈希，不以本项目 MIT 覆盖上游许可。

# 项目文档

DB MCP Tools 的当前使用、开发和设计说明。Server / CLI / Bridge 位于 server/，桌面客户端位于 client/。

| 文档                                    | 内容                                               |
| --------------------------------------- | -------------------------------------------------- |
| [使用指南](user-guide.md)               | 安装、首次连接、Agent 接入、审批、Mask、备份与恢复 |
| [开发指南](development.md)              | 工具链、平台依赖、三条启动路径、格式、测试与打包   |
| [产品范围](product.md)                  | 功能、默认行为和安全边界                           |
| [架构](architecture.md)                 | 状态归属、调用管线、身份、权限、审批和接口         |
| [客户端与打包](client-and-packaging.md) | 桌面交互、本机服务、安装包与配置集成               |
| [依赖与限制](dependencies.md)           | 主要依赖、SQL 范围、驱动与分发限制                 |
| [技术选择](research.md)                 | 参考来源与复用理由                                 |
| [日志与诊断](diagnostics.md)            | 日志位置、导出与故障定位                           |
| [OpenAPI](openapi.json)                 | 从 Go 模型生成的管理接口契约                       |

贡献流程见 [CONTRIBUTING](../CONTRIBUTING.md)，敏感信息与漏洞报告见 [SECURITY](../SECURITY.md)。

版本说明见 [RELEASE](../RELEASE.md)，变更历史见 [CHANGELOG](../CHANGELOG.md)。

业务状态、SQLite、权限、审批和 Mask 统一由 Go Server 管理；Rust 负责桌面与本机集成。main 为集成主线，使用贡献者自己的 Git 身份。GitHub 工作流仅运行 Windows x64 检查，桌面在目标 OS 原生构建。

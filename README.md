# DB MCP Tools

面向个人开发者的数据库 MCP 管理器。独立 Go Server 提供 CLI / STDIO Bridge；Tauri / React 桌面客户端管理实例、实体、权限、审批、Mask、日志和本机 Server。

业务状态只属于 Server，桌面关闭后 Server 可继续运行。支持 PostgreSQL、MySQL、SQL Server 和可选 DM8；默认构建不含许可尚未确认的 DM 驱动，含 DM 的本机构建不作为公开分发包。

## 平台与构建

Server 支持 Windows、macOS 和 Linux；桌面使用 Tauri 的原生平台工具链。GitHub 工作流仅限 Windows x64，PR / main 基础检查与手动未签名打包分开，不自动发布。平台依赖和驱动限制见开发指南。

## 开始开发

所有命令从仓库根目录执行。按工作范围选择环境，详见 [开发指南](docs/development.md)。

| 路径                 | 必需工具                                                        | 入口                                                                         |
| -------------------- | --------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| 前端预览（模拟数据） | Node 24.22.2、pnpm 10.27.0                                      | 安装依赖后运行 pnpm --dir client preview:ui                                  |
| Server / CLI         | Go 1.27.3                                                       | go -C server build -o bin/dbmcp.exe ./cmd/dbmcp，输出为 server/bin/dbmcp.exe |
| 完整桌面             | Node / pnpm、Rust 1.96.1、原生编译依赖；开发本机 Server 另需 Go | 按指南初始化隔离开发目录，再 pnpm --dir client dev:desktop                   |

Linux / macOS 将 Server 输出名改为 bin/dbmcp。首次开发不要使用已安装产品的数据目录；初始化、连接、停止与清理步骤见指南。普通开发不需要容器，真实数据库集成测试才需要临时容器。

## 文档与贡献

- [v1.0.9 发布说明](RELEASE.md) · [变更历史](CHANGELOG.md)
- [安装与使用](docs/user-guide.md)
- [开发环境、三条启动路径与检查](docs/development.md)
- [贡献流程](CONTRIBUTING.md) · [安全问题](SECURITY.md)
- [设计与架构入口](docs/README.md) · [Server](server/README.md) · [客户端](client/README.md)
- [第三方许可](third-party/README.md)

## 本地打包

```sh
pnpm package:server --platform=windows --arch=amd64
pnpm package:client --server=none --target=x86_64-pc-windows-msvc
pnpm package:client --server=bundled --target=x86_64-pc-windows-msvc
```

产物与 SHA256SUMS 位于 artifacts/releases/<当前版本>/，当前为 1.0.9。桌面必须在目标 OS 原生构建，Server 可交叉编译。--dm=native 仅用于明确标识的本地 DM 构建。详细前置条件和测试入口见开发指南。

源码采用 [MIT](LICENSE)，第三方许可不被覆盖。

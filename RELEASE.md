# DB MCP Tools v1.0.9

本次更新整理公开协作和开发入口，并隔离开发版与正式版的桌面配置及凭据。

## 主要变化

- 提供前端预览、独立 Server、完整桌面三条开发路径，补齐 Windows / macOS / Linux 的环境说明。
- 新增 `dev:desktop` 入口，使用独立开发配置、单实例身份与 debug keyring，避免覆盖正式版连接档案和管理员凭据。
- 统一 Node 24 环境检查，API 契约生成后自动执行仓库格式化。
- 数据库测试使用动态宿主端口，检查容器清理结果；Windows 服务与提权测试通过参数选择程序，不再依赖历史构建缓存。
- 新增仅 Windows x64 的 GitHub 基础检查和手动未签名打包工作流，不自动发布 Release。
- 精简项目文档，补充贡献、安全和第三方依赖说明。

## 下载与选择

在 GitHub Release 附件中按用途选择 Windows x64 包：

| 文件                                                        | 用途                                           |
| ----------------------------------------------------------- | ---------------------------------------------- |
| `db-mcp-tools-client-bundled-1.0.9-windows-amd64.setup.exe` | 首次使用，安装桌面客户端并携带本机 Server      |
| `db-mcp-tools-client-none-1.0.9-windows-amd64.setup.exe`    | 仅安装桌面，连接已有 Server 或自行选择独立程序 |
| `dbmcp-server-1.0.9-windows-amd64.zip`                      | 独立运行 Server、CLI 和 STDIO Bridge           |
| `SHA256SUMS`                                                | 分发文件的 SHA256 校验清单                     |

安装包未签名。默认包包含 PostgreSQL、MySQL 和 SQL Server 驱动；DM8 通过显式本机构建启用，不包含在这些公开分发包中。macOS / Linux 的本机构建步骤见 [开发指南](docs/development.md)。

本地构建输出位于 `artifacts/releases/1.0.9/`。这些文件由构建生成，不随源码 clone 下载；维护者通过 GitHub Release 附件分发，自动工作流的产物位于对应 Actions 运行页面。

## 升级提示

使用安装器更新已有客户端并保留数据。携带 Server 的客户端可升级受管本机 Server；使用独立或远程 Server 时，按 [使用指南](docs/user-guide.md) 备份并更新对应程序。

已有实例、实体、策略和连接密码保留。本次不增加 SQLite schema 迁移。开发者请改用 `pnpm --dir client dev:desktop`；首次需要为独立开发档案选择开发 Server 和数据目录，开发版不沿用正式版管理员凭据。

完整变更历史见 [CHANGELOG](CHANGELOG.md)，安装与配置见 [使用指南](docs/user-guide.md)。

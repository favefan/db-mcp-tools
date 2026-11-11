# DB MCP Tools v1.0.11

本次更新简化配置管理与桌面交互，统一本机托管的 MCP Server 生命周期。

## 主要变化

- “环境”改称“配置”，设置入口为“配置与 MCP Server”；侧栏底部菜单支持快速切换。
- 当前配置使用主题强调色标记，连接方式改为紧凑选项，修正折叠侧栏与表单对齐。
- 精简说明文字，必要提示移至圆圈感叹号悬浮提示；操作通知统一放在右下角。
- 本机托管默认启动、接管和停止对应 MCP Server，保留手动启动、停止和重启；HTTP 配置只连接，包括 127.0.0.1。
- 移除桌面系统服务操作区；仅检测到有新版程序时显示“升级 MCP Server”。
- 取消定时、启动和切换时的自动备份，保留手动备份和升级、恢复、结构迁移前的安全快照。

## 下载与选择

在 GitHub Release 附件中按用途选择 Windows x64 包：

| 文件                                                         | 用途                                               |
| ------------------------------------------------------------ | -------------------------------------------------- |
| `db-mcp-tools-client-bundled-1.0.11-windows-amd64.setup.exe` | 首次使用，安装桌面客户端并携带本机 MCP Server      |
| `db-mcp-tools-client-none-1.0.11-windows-amd64.setup.exe`    | 仅安装桌面，连接已有 MCP Server 或自行选择独立程序 |
| `dbmcp-server-1.0.11-windows-amd64.zip`                      | 独立运行 MCP Server、CLI 和 STDIO Bridge           |
| `SHA256SUMS`                                                 | 分发文件的 SHA256 校验清单                         |

安装包未签名。默认包包含 PostgreSQL、MySQL 和 SQL Server 驱动；DM8 通过显式本机构建启用，不包含在这些公开分发包中。macOS / Linux 的本机构建步骤见 [开发指南](docs/development.md)。

本地构建输出位于 `artifacts/releases/1.0.11/`。这些文件由构建生成，不随源码 clone 下载；维护者通过 GitHub Release 附件分发，自动工作流的产物位于对应 Actions 运行页面。

## 升级提示

使用安装器更新已有客户端并保留数据。携带 MCP Server 的客户端可升级受管本机 MCP Server；使用独立或远程 MCP Server 时，按 [使用指南](docs/user-guide.md) 备份并更新对应程序。

已有实例、实体、策略和连接密码保留，不增加 SQLite schema 迁移。配置和凭据保持原位。本机托管要求 MCP Server 至少 1.0.11，旧启停开关不再使用；如需保持 MCP Server 独立运行，请使用已有 MCP Server 的 HTTP 配置。普通启动不自动升级，可在设置中显式升级所选程序。

完整变更历史见 [CHANGELOG](CHANGELOG.md)，安装与配置见 [使用指南](docs/user-guide.md)。

## 维护者发布流程

1. 将版本同步到根与客户端 package.json、Tauri 配置、Cargo.toml / Cargo.lock 和 Go 的 Version；更新本文件标题、发布说明及 CHANGELOG 中对应版本条目。
2. 将变更合入并推送 main，再从该提交创建并推送稳定版本标签。标签工作流会独立执行源码、契约和运行管理检查，再构建并校验安装包：

   ```sh
   git tag v1.0.11
   git push origin v1.0.11
   ```

3. `Publish Windows release` 工作流核对标签、版本和文档，构建独立 MCP Server、纯客户端和携带 MCP Server 三种默认无 DM 的 Windows x64 包，校验后上传附件并发布 GitHub Release。发布说明使用标签提交中的 RELEASE.md。

标签采用 `v主版本.次版本.修订版本`，例如 `v1.0.11`；当前流程不发布预发行版本。仅构建和校验成功后才进入 Release 写入任务，使用仓库自带的 GITHUB_TOKEN，无需额外配置 PAT。既有同名 Release 不会被覆盖；若上传失败留下草稿，删除该草稿后可在 Actions 重跑失败的任务。

Release 附件还包含两个客户端构建清单及 source-maps.zip；发布用 SHA256SUMS 覆盖全部六个附件，可在下载到同一目录后核对。源码映射按原 source-maps/ 目录结构压缩，独立于安装包。

手动 `Windows unsigned packages` 工作流仍只上传 Actions Artifact，不创建 Release。

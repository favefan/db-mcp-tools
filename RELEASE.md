# DB MCP Tools v1.0.9

本次更新整理公开协作和开发入口，并隔离开发版与正式版的桌面配置及凭据。

## 主要变化

- 提供前端预览、独立 Server、完整桌面三条开发路径，补齐 Windows / macOS / Linux 的环境说明。
- 新增 `dev:desktop` 入口，使用独立开发配置、单实例身份与 debug keyring，避免覆盖正式版连接档案和管理员凭据。
- 统一 Node 24 环境检查，API 契约生成后自动执行仓库格式化。
- 数据库测试使用动态宿主端口，检查容器清理结果；Windows 服务与提权测试通过参数选择程序，不再依赖历史构建缓存。
- 新增仅 Windows x64 的 GitHub 基础检查、手动未签名打包及版本标签触发的 Release 发布工作流。
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

## 维护者发布流程

1. 将版本同步到根与客户端 package.json、Tauri 配置、Cargo.toml / Cargo.lock 和 Go 的 Version；更新本文件标题、发布说明及 CHANGELOG 中对应版本条目。
2. 将变更合入 main，确认基础检查成功，再从该提交创建并推送稳定版本标签：

   ```sh
   git tag v1.0.9
   git push origin v1.0.9
   ```

3. `Publish Windows release` 工作流核对标签、版本和文档，构建独立 Server、纯客户端和携带 Server 三种默认无 DM 的 Windows x64 包，校验后上传附件并发布 GitHub Release。发布说明使用标签提交中的 RELEASE.md。

标签采用 `v主版本.次版本.修订版本`，例如 `v1.0.9`；当前流程不发布预发行版本。仅构建和校验成功后才进入 Release 写入任务，使用仓库自带的 GITHUB_TOKEN，无需额外配置 PAT。既有同名 Release 不会被覆盖；若上传失败留下草稿，删除该草稿后可在 Actions 重跑失败的任务。

Release 附件还包含两个客户端构建清单及 source-maps.zip；发布用 SHA256SUMS 覆盖全部六个附件，可在下载到同一目录后核对。源码映射按原 source-maps/ 目录结构压缩，独立于安装包。

手动 `Windows unsigned packages` 工作流仍只上传 Actions Artifact，不创建 Release。

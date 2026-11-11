# DB MCP Tools v1.0.10

本次更新引入命名环境（Profile）与唯一当前会话，统一连接切换、本机 Server 启停和客户端偏好。

## 主要变化

- 侧栏突出唯一当前环境；本机托管与已有 Server 是互斥选择。
- 每个环境独立保存管理员凭据、主题、通知、关闭行为和自动轻量模式；系统登录自启动共用。
- 可勾选 Server 随客户端启停：进入环境启动，切走或真正退出时仅停止本次启动且身份匹配的后台实例；托盘与轻量模式继续运行。
- 切换前阻止进行中操作，清理旧会话缓存、通知和订阅，拒绝旧响应；失败显示断开状态。
- 新本机环境分配独立目录及可用回环端口，保留已有部署地址；旧连接迁移为 `default` 环境。

## 下载与选择

在 GitHub Release 附件中按用途选择 Windows x64 包：

| 文件                                                         | 用途                                           |
| ------------------------------------------------------------ | ---------------------------------------------- |
| `db-mcp-tools-client-bundled-1.0.10-windows-amd64.setup.exe` | 首次使用，安装桌面客户端并携带本机 Server      |
| `db-mcp-tools-client-none-1.0.10-windows-amd64.setup.exe`    | 仅安装桌面，连接已有 Server 或自行选择独立程序 |
| `dbmcp-server-1.0.10-windows-amd64.zip`                      | 独立运行 Server、CLI 和 STDIO Bridge           |
| `SHA256SUMS`                                                 | 分发文件的 SHA256 校验清单                     |

安装包未签名。默认包包含 PostgreSQL、MySQL 和 SQL Server 驱动；DM8 通过显式本机构建启用，不包含在这些公开分发包中。macOS / Linux 的本机构建步骤见 [开发指南](docs/development.md)。

本地构建输出位于 `artifacts/releases/1.0.10/`。这些文件由构建生成，不随源码 clone 下载；维护者通过 GitHub Release 附件分发，自动工作流的产物位于对应 Actions 运行页面。

## 升级提示

使用安装器更新已有客户端并保留数据。携带 Server 的客户端可升级受管本机 Server；使用独立或远程 Server 时，按 [使用指南](docs/user-guide.md) 备份并更新对应程序。

已有实例、实体、策略和连接密码保留，不增加 SQLite schema 迁移。旧连接与偏好迁移到 `default` 环境，默认不启用随客户端启停；原配置保留。该启停选项要求本机程序至少 1.0.10，不能用于系统服务；外部已运行实例需先手动停止或关闭此选项。开发版仍使用独立配置与 debug keyring。

完整变更历史见 [CHANGELOG](CHANGELOG.md)，安装与配置见 [使用指南](docs/user-guide.md)。

## 维护者发布流程

1. 将版本同步到根与客户端 package.json、Tauri 配置、Cargo.toml / Cargo.lock 和 Go 的 Version；更新本文件标题、发布说明及 CHANGELOG 中对应版本条目。
2. 将变更合入 main，确认基础检查成功，再从该提交创建并推送稳定版本标签：

   ```sh
   git tag v1.0.10
   git push origin v1.0.10
   ```

3. `Publish Windows release` 工作流核对标签、版本和文档，构建独立 Server、纯客户端和携带 Server 三种默认无 DM 的 Windows x64 包，校验后上传附件并发布 GitHub Release。发布说明使用标签提交中的 RELEASE.md。

标签采用 `v主版本.次版本.修订版本`，例如 `v1.0.10`；当前流程不发布预发行版本。仅构建和校验成功后才进入 Release 写入任务，使用仓库自带的 GITHUB_TOKEN，无需额外配置 PAT。既有同名 Release 不会被覆盖；若上传失败留下草稿，删除该草稿后可在 Actions 重跑失败的任务。

Release 附件还包含两个客户端构建清单及 source-maps.zip；发布用 SHA256SUMS 覆盖全部六个附件，可在下载到同一目录后核对。源码映射按原 source-maps/ 目录结构压缩，独立于安装包。

手动 `Windows unsigned packages` 工作流仍只上传 Actions Artifact，不创建 Release。

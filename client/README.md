# Client

Tauri 2 + React + TypeScript 桌面客户端。当前已有实例、实体、审批、日志、设置五项导航，实例七标签、策略和 Mask 编辑、本机后台进程管理与深浅主题。

前端位于 `src/`，Tauri 壳与本机集成位于 `src-tauri/`。客户端通过 Admin API 管理 Server，通过本机 Go CLI 执行进程和系统服务操作；不复制 Server 的数据库、权限、审批、Mask 和 SQLite 逻辑。

已实现纯客户端与携带 Server 两种构建模式，详见 [客户端与打包设计](../docs/client-and-packaging.md)。

```sh
pnpm --dir client generate:api
pnpm --dir client typecheck
pnpm --dir client lint
pnpm --dir client dev:desktop
pnpm --dir client tauri build --debug --no-bundle
cargo clippy --manifest-path client/src-tauri/Cargo.toml --all-targets -- -D warnings
```

从仓库根目录执行。开发前用 Node 24 LTS 和锁定的 pnpm 安装依赖；Tauri 还需要各平台原生编译依赖，安装步骤和隔离开发 Server 流程见 [开发指南](../docs/development.md)。dev:desktop 使用开发专属配置目录、单实例身份与 debug keyring，不覆盖正式版档案；直接运行 tauri dev 不具备配置目录隔离。Admin 请求由 Rust 白名单代理转发，管理员凭据保存到 OS keyring；WebView 不保存 Token。已接入服务迁移与升级、配置安装 / 移除、程序 / 目录选择、客户端自启。

从仓库根目录使用 `pnpm package:client --server=none|bundled --target=<三元组>`。纯客户端不会编译或包含 Go；携带包另需 `--dm=native` 才包含本机 DM 原生构建。Windows NSIS 工具使用固定官方版本和哈希，缓存于 src-tauri/target/.tauri/；构建脚本从官方来源下载并核对哈希。

业务页面统一使用 `components/ui.tsx` 的 Button、DataTable、DataList／DataListItem、Card、ViewTabs、Field、Modal、Notice 和 Badge。Button 通过现有 Radix Slot 支持文件选择等语义元素；主／次／文字／图标四种变体集中管理尺寸、焦点和危险色。DataTable 必须传入表头并统一空行，按列比较的数据使用表格，标题／说明／操作组合使用列表。Notice 统一警告／错误的间距、长文本换行和操作入口，放在关联内容区；页面标题与窗口控制之前不插入业务提示。页面不再复制按钮类、列表行和表格外壳；边距与颜色使用 styles.css 中的设计变量。

窗口控制与侧栏导航保留专用交互，实例页使用 Radix Tabs，对象树由 Headless Tree 管理。树的内联变量只表达层级深度，缩进仍由统一 CSS 间距计算。

主页面统一使用 `components/page-header.tsx` 的 Page：窗口根容器与标题栏固定，只有 `.page-content` 滚动并预留滚动条位置。弹窗由 modal-body 或活动 tab-content 负责滚动，禁止给外层窗口叠加滚动。操作、表格与展开内容用公共 Stack 的 16px gap 组合；Stack 负责子项外间距，Button／DataTable 不额外猜测上下文添加 margin。Button 的 success／danger／warning 色调集中管理，禁用状态统一为灰色。本机控制按钮依据所选程序与目录的 Go 状态结果，状态未确认时禁止启停。

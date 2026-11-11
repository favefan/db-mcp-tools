# 开发、格式化、Git 与验证规范

## 环境与首次运行

以下命令均从仓库根目录执行。固定版本：Node 24.22.2、pnpm 10.27.0、Go 1.27.3、Rust 1.96.1。版本由 .node-version、packageManager、go.mod 和 rust-toolchain.toml 固定。

pnpm 通过仓库 .npmrc 严格检查 Node 主版本，不支持 Node 25。只开发 Server 可仅安装 Go；只预览前端无需 Go、Rust 或数据库。安装 Node 后可用 npm install --global pnpm@10.27.0 安装 pnpm（由贡献者主动执行，仓库脚本不改全局环境），或使用自己已有的 pnpm / Corepack 管理方式。安装 Rust 时使用 [rustup](https://rustup.rs/)，在仓库中运行 rustup show 会读取 rust-toolchain.toml 并安装固定工具链及 rustfmt / clippy。

官方下载：[Node](https://nodejs.org/en/download) · [Go](https://go.dev/dl/) · [Tauri 平台前置条件](https://v2.tauri.app/start/prerequisites/)。命令找不到时先确认 PATH；不要把作者机器绝对路径加入脚本。首次依赖下载需要访问 npm、crates.io、Go 模块源和可选的 Gitee DM 模块源。

### 桌面原生依赖

| 平台        | 安装与运行条件                                                                                                                                                                                    |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Windows x64 | Visual Studio 2022 Build Tools 的“使用 C++ 的桌面开发”、MSVC x64 / Windows SDK；Rust 使用 MSVC 工具链；WebView2 Runtime                                                                           |
| macOS       | Xcode Command Line Tools（xcode-select --install）、Rust 对应宿主架构；凭据使用系统 Keychain                                                                                                      |
| Linux       | Ubuntu 24.04 参考：build-essential、pkg-config、libwebkit2gtk-4.1-dev、libssl-dev、libdbus-1-dev、librsvg2-dev、libayatana-appindicator3-dev、patchelf；其他发行版按 Tauri 官方前置条件安装等效包 |

Linux 运行完整桌面需要图形会话、D-Bus 和可用且已解锁的 Secret Service（例如 GNOME Keyring）；仅安装头文件不能保证凭据读写可用。Server 无需这些桌面依赖。打包另需目标平台工具，见后文。

### 路径一：前端预览

```sh
pnpm install --frozen-lockfile
pnpm --dir client preview:ui
```

打开 http://127.0.0.1:1420，可看到模拟实例、审批与日志。此模式使用内存夹具，不连接真实 Server，不代表数据库或 Rust IPC 验证；终端 Ctrl+C 停止。若 1420 被占用，停止占用该端口的本项目开发进程或选择其他预览端口；不要终止不属于本次测试的进程。

### 路径二：Server / CLI

Windows PowerShell：

```powershell
go -C server build -o bin/dbmcp.exe ./cmd/dbmcp
./server/bin/dbmcp.exe --data-dir ./.tmp/dev-server init
./server/bin/dbmcp.exe --data-dir ./.tmp/dev-server serve
```

macOS / Linux：

```sh
go -C server build -o bin/dbmcp ./cmd/dbmcp
./server/bin/dbmcp --data-dir ./.tmp/dev-server init
./server/bin/dbmcp --data-dir ./.tmp/dev-server serve
```

init 输出 server_id 等初始化结果，serve 保持前台运行。另开终端，在同一仓库目录执行（Unix 去掉 .exe）：

```powershell
./server/bin/dbmcp.exe --data-dir ./.tmp/dev-server api /admin/v1/health
./server/bin/dbmcp.exe --data-dir ./.tmp/dev-server instances list
```

应返回同一 Server 的健康信息和空实例列表。CLI 自动读取私密凭据，不要直接输出 admin.token。默认 Admin 为 127.0.0.1:19876、MCP 为 127.0.0.1:19877；已被占用时，在初始化生成的 .tmp/dev-server/deployment.toml 中为本次测试选择空闲端口，再启动，不连接或停止已有产品服务。

在 serve 终端 Ctrl+C 停止后才清理本次测试 .tmp/dev-server。若改用 run start 后台模式，用相同程序和数据目录 run stop 并确认 run status 为停止，再删除本次测试目录；不要删除整个 .tmp 或用户默认数据目录。Server 功能说明见 [server/README](../server/README.md)。

### 路径三：完整桌面

先安装桌面原生依赖和 pnpm 依赖，按路径二构建程序并初始化开发目录，但先不要执行 serve；若已启动，请先停止该开发 Server，再启动桌面：

```sh
pnpm --dir client dev:desktop
```

首次会编译 Rust 并打开开发窗口。此入口通过已提交的 tauri.dev.conf.json 使用独立 identifier / 产品名，隔离桌面配置与单实例；debug 构建使用独立的 keyring 服务名，不覆盖正式版管理员凭据。不要直接运行未带配置的 tauri dev，以免共享正式版配置目录。开发连接必须仍选择本次测试 Server 与数据目录，不安装服务、不改写真实 Agent 配置。

窗口中进入 Server → 本机托管，选择当前仓库 server/bin/dbmcp[.exe] 与 .tmp/dev-server 的绝对路径，点击“初始化目录”同步当前目录的管理端口（保留已有配置与身份），再点击“启动并连接”。连接后确认健康状态及空实例列表。本机模式由启动结果同步自定义端口；不要在未同步地址时直接“重新连接”，否则可能仍使用表单中的默认地址。不要安装系统服务或接入真实 Agent 做首次开发验证。

停止时先退出桌面开发进程，再停止路径二的 Server；只清理本次测试开发数据和配置。完整桌面需要真实图形会话。

### 常见问题

- pnpm 安装版本不符：按 packageManager 指定版本运行，不删除锁文件解决冲突。
- Tauri 编译失败：先核对平台依赖和 MSVC / SDK，不通过关闭静态检查规避。
- 桌面无法连接：核对所选程序、数据目录、端口和 Server 身份；不要把原始 admin.token 当文本 Token。
- Linux 凭据保存失败：检查 Secret Service 和会话解锁状态，不回退到明文凭据文件。
- 数据库未装：普通 Go 测试无需数据库；只有容器测试入口会启动临时数据库。

## 1. 协作与 Git

使用自己的 Git 身份，不复制作者的姓名和邮箱。main 为集成主线，主题分支合入后删除。提交、合并和远端操作遵循贡献流程与当前任务授权；不自动丢弃未提交文件，不重写他人历史，不修改全局 Git 配置。

实现前阅读相关架构和调用链，修改后提交必要源码、文档与锁文件。常规贡献见 [CONTRIBUTING](../CONTRIBUTING.md)，历史实施授权不作为其他贡献者的默认授权。

## 2. 文本与换行

`.gitattributes` 是仓库最终规则：默认文本在索引和工作区统一 LF；`.bat` / `.cmd` 才使用 CRLF，二进制明确不转换。`.editorconfig` 统一 UTF-8 无 BOM、末尾换行和缩进，Prettier 强制 LF，Rustfmt 使用 Unix 换行。

不要为了换行、个人编辑器设置或对齐习惯批量重写无关文件。导入旧文本时必要的换行归一化单独提交；跨平台 checkout 不应制造整文件差异。检查可用 `git ls-files --eol` 和 `git diff --check`。

用户机器上的 MCP 配置文件是外部文件，优先保留原有换行与其他内容；仓库规范不意味着替用户格式化整个配置。

## 3. 固定格式工具与风格

| 语言 / 文件                  | 唯一格式工具                          | 风格及配套检查                                                                      |
| ---------------------------- | ------------------------------------- | ----------------------------------------------------------------------------------- |
| Go                           | Go 工具链内的 `gofmt`                 | 标准 Go 风格，不维护自定义排版；`go vet` 做基础静态检查                             |
| Rust                         | Rust 工具链内的 `cargo fmt` / rustfmt | 根 `rustfmt.toml`，100 列、Unix 换行；`cargo clippy` 检查实际壳代码                 |
| TS / TSX / JS / JSX / MJS    | Prettier                              | 根 `.prettierrc.json`，2 空格、双引号、分号、100 列；TS strict，ESLint 最小推荐规则 |
| CSS / JSON / YAML / Markdown | 同一 Prettier                         | 不为每类文件另引一套工具；Markdown 保留作者段落换行                                 |
| SQLite 迁移 SQL              | Prettier + `prettier-plugin-sql`      | `server/migrations/**/*.sql` 指定 SQLite 方言，关键字大写                           |
| TOML                         | Taplo                                 | `.taplo.toml`，不自动重排键，100 列                                                 |

当前已锁定：pnpm 10.27.0、Prettier 3.9.10、prettier-plugin-sql 0.20.0、`@taplo/cli` npm 包 0.7.0（实际输出 Taplo 0.9.0）。提交 `pnpm-lock.yaml`，不随日常开发自动升级格式工具。

仓库脚本尽量使用 Node MJS，减少 PowerShell / Bash 双套逻辑。若确需引入新的脚本语言，在同一变更配置其标准格式工具；不要为了一个简单构建步骤引入 Python 或自写格式器。目标数据库 SQL 样本与 SQLite 迁移分开；加入其他 SQL 方言文件时同时添加正确的 Prettier overrides。

Go 版本由 go.mod 固定，Rust 由 rust-toolchain.toml 固定，Node 由 .node-version 与 package engines 约定；安装方式见下方。

### 已可执行的命令

```sh
pnpm install --frozen-lockfile
pnpm format
pnpm format:check
git diff --check
```

### 按改动选择的检查

以下命令已存在，按受影响范围执行；全量格式检查需要 Go、Rust 和 Node 工具链。

```sh
gofmt -w server/cmd server/internal
go -C server vet ./...
go -C server test ./...
cargo fmt --manifest-path client/src-tauri/Cargo.toml
cargo clippy --manifest-path client/src-tauri/Cargo.toml --all-targets -- -D warnings
pnpm --dir client typecheck
pnpm --dir client lint
```

`pnpm format` 和 check 模式统一调用 gofmt、cargo fmt、Prettier、Taplo；check 不修改文件，任一工具失败均返回非零退出码。

ESLint 使用 使用 TypeScript / React 推荐配置和 React Hooks 规则，排版交给 Prettier，不启用重复的格式风格规则。Rust 不无理由允许全局 clippy 警告；Go 不预先引入大包 lint 规则迫使简单代码变复杂。

## 4. 依赖与实现风格

先看成熟库和系统 API 能否满足，再写自己的实现。默认 Go 标准库 `net/http`、`database/sql`、`slog` 配合官方 MCP SDK；CLI / migration / service 使用成熟库。无需 ORM、DI 容器、通用插件框架、复杂配置层和全局事件总线。

按能力需求添加客户端依赖，组件直接用 shadcn / Radix，表单和服务状态分别交给 React Hook Form / TanStack Query。数据业务只在 Go 实现，Rust 保持薄，避免一份策略跨三种语言维护。

新依赖记录用途、许可、版本和对平台构建的影响。锁定 Go / Cargo / pnpm 依赖；只在实际提取参考代码时增加其源码归属和许可说明，不虚构已有代码来源。Go sum 校验保持启用，不照抄旧文档关闭 Modules、修改 GOROOT/src 或全局改代理的步骤。

## 5. 测试和验证节奏

| 时机                                | 适度执行                                             |
| ----------------------------------- | ---------------------------------------------------- |
| 普通 UI / 文档 / 简单 CRUD 修改     | 相关格式、类型或编译检查；必要时实际走一条受影响流程 |
| 修改身份 / 策略 / SQL / Mask / 审批 | 针对改变的业务边界补集中回归并运行受影响测试         |
| 完成一条完整用户流程                | 一次端到端联调，确认上下游契约，不扩大到无关平台     |
| 形成发布候选                        | 检查数据库、目标 OS、两种客户端包和恢复场景          |

不为每个按钮、组件、setter、简单转发函数写镜像测试，不追逐覆盖率。不要小改一次就重复全库集成、全平台构建、截图巡检。格式和测试通过后继续交付，只有新变更、失败或未解决问题才扩大检查。

值得保留的回归是：凭据隔离和撤销；硬拒绝不能被审批绕过；写 CTE / 多语句；敏感列在条件和别名中出现；强制 Mask；审批内容绑定、超时和并发 resume；写入结果未知不重试；外部配置无关内容保留；在线 SQLite 备份一致性。

### 数据库容器测试与镜像构建

所有目标数据库（PostgreSQL、MySQL、SQL Server、DM8，以及 PostGIS / TimescaleDB 扩展）的集成测试和验证统一创建临时数据库容器，不依赖用户提供长期测试连接，也不在宿主机安装数据库或连接现有业务实例完成验证。Server 自身的嵌入式 SQLite 状态库使用临时文件或内存库测试，结束后同样清理。

容器工具根据当前开发平台、已安装工具、运行状态和镜像架构实际选择，不固定唯一工具，也不要求同时安装多套工具：

| 开发平台 | 可按实际情况选择的工具                                              |
| -------- | ------------------------------------------------------------------- |
| Windows  | WSLC（适用于 Linux 的 Windows 子系统容器 CLI 工具）、Docker、Podman |
| macOS    | Apple Container、Docker、Podman                                     |
| Linux    | Docker、Podman                                                      |

优先使用已有可用的数据库镜像，记录镜像来源、tag / digest、数据库版本和运行架构。**没有可用的对应数据库镜像时，允许跳过该数据库的测试和验证**，记录 `skipped` 和具体原因，继续其他数据库及功能开发；不把跳过写成通过，也不因缺少镜像阻塞整个阶段。镜像可用但产品测试失败时记录失败，不用“缺少镜像”掩盖实际问题。当前平台没有可用容器工具时，同样记录环境原因，不改用宿主机数据库。

**任何可用的镜像构建方式都可以用于构建自定义专用测试镜像**，不限于 Docker。可使用 Dockerfile / Containerfile 或工具支持的其他构建输入与流程，按实际可用的构建器执行；容器运行工具和镜像构建工具不必相同，只需保证生成的镜像能被所选运行工具加载并启动。自建镜像是按需选项，不是跳过缺失镜像测试前的强制任务。

实际需要构建时，把必要构建文件、初始化 SQL 和使用说明放在 `server/tests/containers/<数据库>/`，固定基础镜像及数据库安装来源，遵守实际许可。优先工具原生命令和少量脚本适配，不为支持多种容器工具自建编排框架。新增构建文件或脚本时遵守本仓库格式化约定。

每轮集中验证按“选择可用工具与镜像 → 创建隔离容器及临时数据 → 初始化并执行本次测试场景 → 保存简短结果 → 清理”完成。**开发、测试和验证容器用后即焚**：

- 为本次测试资源使用明确的名称或标记，只操作本次测试创建的容器、卷、网络和文件。
- 容器数据不复用为下一轮环境；本次测试结束清理容器、卷、网络、挂载临时目录和本次测试构建的专用测试镜像。
- 在正常完成、失败或中断的清理路径中执行回收，不默认保留容器排错；只保留必要且不含凭据的日志和结果。
- 不运行影响其他项目的全局 prune，不删除用户已有数据库、镜像或无关构建缓存。

数据库验证结果统一记录为通过、失败或 `skipped`，附原因及实际镜像 / 引擎版本。四库仍是产品支持目标，但镜像不可用导致的跳过不要求人为补建一个长期环境，也不妨碍完成其他已授权工作。

## 构建与集成测试

UI 测试使用 Playwright 或 Tauri 自动化工具，不使用 computer use。Windows 可给本次测试隔离 WebView2 设置临时 CDP 调试端口，再由 Playwright 连接真实 Tauri 页面；生产包不默认开启调试端口。界面测试记录应区分真实 IPC、文件对话框的自动化适配和浏览器演示数据，不把模拟接口写成真实 Server 通过。开始菜单、安装器和资源哈希通过安装测试与系统文件接口验证。截图和 trace 放入忽略的 `output/playwright/`。NSIS 修复钩子由 Prettier 管理的 MJS 模板生成，不手工维护另一套脚本格式。

Windows 安装迁移回归使用 `node scripts/test-windows-installer.mjs --previous-script=<旧版生成的 installer.nsi> --report=<结果.json>`。在重新构建覆盖生成脚本前先备份旧脚本；其资源输入仍需可读取。测试编译独立身份的 NSIS 并静默安装／迁移／卸载，不启动 GUI、不覆盖实际产品的注册表／快捷方式／档案。当前脚本默认取本机 x64 release 生成目录，也可用 `--current-script` 指定。原模板来源和固定哈希由 `package-windows-installer.mjs` 管理。

使用 `pnpm package:server --platform=windows|linux|darwin --arch=amd64|arm64 [--dm=native]` 和 `pnpm package:client --server=none|bundled --target=<Rust 三元组> [--dm=native]`。客户端要求目标 OS 的原生环境；纯客户端不调用 Go，也不把 binaries 目录残留当资源打包。默认不含未确认分发许可的 DM；本机 `dm-local` 版本明确记录限制。源码本身采用 MIT，第三方许可不被覆盖。

构建使用 archiver 8.0.0（MIT）生成独立包；依赖清单来自实际 Go 依赖、Cargo 元数据和 pnpm 生产依赖，附上游许可文本、补充许可来源 / 哈希及精确源码地址。Windows NSIS 3.13 / Tauri utilities 0.5.3 对齐锁定 CLI 2.12.4，官方资产 API下载后校验固定哈希。

Tauri 2.12.4 给安装程序内的主程序写入包类型标记，完成后恢复未修改的编译输出（已读取锁定版本 tauri-bundler 的实现）。客户端清单 `unbundled_app_sha256` 对应该恢复后的编译输出，不能直接与安装后的程序比较；分发校验使用 `installer_files` 与 `SHA256SUMS`。资源清单按包内实际资源逐文件核验。

真实 Agent 集成测试使用 `node scripts/test-agent.mjs`，需先登录其官方 CLI。Codex 设置 `DBMCP_TEST_CODEX_JS`；Claude 设置 `DBMCP_TEST_AGENT=claude` 与 `DBMCP_TEST_CLAUDE`。可用 `DBMCP_TEST_SERVER` 指向已打包程序、`DBMCP_TEST_RUNTIME` 选择容器 CLI。Windows SCM 测试在人工授权的管理员终端执行 `node scripts/test-windows-service.mjs --original=<原程序> --candidate=<不同构建的升级程序>`；脚本只管理唯一名称的本次测试资源，并核验卸载后保留数据再清理。

## 测试命令与 CI

基础检查：pnpm format:check、pnpm --dir client typecheck、pnpm --dir client lint、pnpm --dir client build、go -C server test ./...、go -C server vet ./...、cargo test --manifest-path client/src-tauri/Cargo.toml --locked、cargo clippy --manifest-path client/src-tauri/Cargo.toml --locked --all-targets -- -D warnings。API 变更运行 pnpm generate:api 并检查 docs/openapi.json 与 client/src/lib/contract.ts。

运行管理冒烟先构建 server/bin/dbmcp[.exe]，再运行 pnpm smoke:runtime；此入口启动独立临时 Server，不连接目标数据库。

数据库入口：pnpm test:databases --runtime=docker --drivers=postgres（也可显式选择 podman / wslc 与多个逗号分隔数据库）。默认运行时为 Docker，不自动安装工具；Apple Container 尚无脚本适配，请勿将文档允许使用误解为此脚本已支持。端口由容器运行时分配并读取，结果保存在 artifacts/database-validation-*.json。全跳过返回退出码 2，失败 / 中断 / 清理失败返回 1，至少一项通过且无失败返回 0；部分跳过仍需阅读报告。固定容器密码只用于回环隔离测试。

SCM 测试：管理员终端运行 node scripts/test-windows-service.mjs --original=<原程序> --candidate=<不同构建的候选程序>。两个输入可来自自行保留的旧版 / 新版默认 Server 构建，无需 DM；脚本核对哈希不同并记录输入哈希。UAC 测试：在非提权用户终端运行 node scripts/test-windows-elevation.mjs --server=<本次测试程序>，人工处理产品原生 UAC。两者均不属于 PR 自动检查。

安装器迁移的 previous-script 必须来自保存完整资源输入的旧版本原生构建目录；单独下载旧 exe 或拷贝 installer.nsi 不足以复现。缺少旧构建时不运行迁移测试。详见上述 构建与集成测试。

.github 工作流仅使用 Windows x64。普通 PR 不需要 Secrets、管理员权限、目标数据库或 Agent 登录；手动打包只上传 Actions Artifact，版本标签触发核对、构建、校验和 Release 发布。发布任务通过 GITHUB_TOKEN 获得 contents: write，构建任务保持只读；发布步骤见 [RELEASE](../RELEASE.md#维护者发布流程)。版本门禁可用 `node --test scripts/release-version.test.mjs` 检查。Windows 托管 runner 不假设可用 Linux 数据库容器。分支保护和安全报告渠道由仓库所有者在 GitHub 配置。

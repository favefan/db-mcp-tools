# Development, formatting, Git, and validation

## Setup and first run

Run commands from the repository root. Pinned tools: Node 24.22.2, pnpm 10.27.0, Go 1.27.3, and Rust 1.96.1. Versions are defined by `.node-version`, `packageManager`, `go.mod`, and `rust-toolchain.toml`.

The repository's `.npmrc` strictly checks the Node major version; Node 25 is unsupported. MCP Server-only development requires Go only. Frontend preview needs no Go, Rust, or database. After installing Node, install pnpm with `npm install --global pnpm@10.27.0` or use an existing pnpm / Corepack setup. Repository scripts do not change global tools. Install Rust through [rustup](https://rustup.rs/); `rustup show` in the repository reads the pinned toolchain and installs rustfmt / clippy.

Official downloads: [Node](https://nodejs.org/en/download) · [Go](https://go.dev/dl/) · [Tauri prerequisites](https://v2.tauri.app/start/prerequisites/). Check PATH if a command is unavailable; do not put a maintainer's absolute paths in scripts. Initial dependency downloads require access to npm, crates.io, Go module sources, and optionally the Gitee DM module source.

### Native desktop prerequisites

| Platform    | Requirements                                                                                                                                                                                                                                             |
| ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Windows x64 | Visual Studio 2022 Build Tools with Desktop development with C++, MSVC x64 / Windows SDK, Rust MSVC toolchain, and WebView2 Runtime                                                                                                                      |
| macOS       | Xcode Command Line Tools (`xcode-select --install`), native Rust target, and system Keychain                                                                                                                                                             |
| Linux       | Ubuntu 24.04 reference: `build-essential`, `pkg-config`, `libwebkit2gtk-4.1-dev`, `libssl-dev`, `libdbus-1-dev`, `librsvg2-dev`, `libayatana-appindicator3-dev`, `patchelf`; use equivalent packages from the Tauri prerequisites on other distributions |

The full Linux desktop needs a graphical session, D-Bus, and an available, unlocked Secret Service such as GNOME Keyring. Header packages alone do not establish working credential storage. The MCP Server needs none of these desktop dependencies. Packaging also needs native platform tools.

### Path 1: frontend preview

```sh
pnpm install --frozen-lockfile
pnpm --dir client preview:ui
```

Open `http://127.0.0.1:1420` to see mock instances, approvals, and logs. This mode uses in-memory fixtures, not real MCP Server connections, databases, or Rust IPC. Stop with Ctrl+C. If the port is occupied, stop only this project's relevant development process or select another preview port; do not terminate unrelated processes.

### Path 2: MCP Server / CLI

Windows PowerShell:

```powershell
go -C server build -o bin/dbmcp.exe ./cmd/dbmcp
./server/bin/dbmcp.exe --data-dir ./.tmp/dev-server init
./server/bin/dbmcp.exe --data-dir ./.tmp/dev-server serve
```

macOS / Linux:

```sh
go -C server build -o bin/dbmcp ./cmd/dbmcp
./server/bin/dbmcp --data-dir ./.tmp/dev-server init
./server/bin/dbmcp --data-dir ./.tmp/dev-server serve
```

`init` prints initialization metadata including `server_id`; `serve` stays in the foreground. Open another terminal in the same repository and run, omitting `.exe` on Unix:

```powershell
./server/bin/dbmcp.exe --data-dir ./.tmp/dev-server api /admin/v1/health
./server/bin/dbmcp.exe --data-dir ./.tmp/dev-server instances list
```

These should return the same MCP Server's health and an empty instance list. The CLI reads private credentials automatically; do not print `admin.token`. Default Admin and MCP ports are `127.0.0.1:19876` and `127.0.0.1:19877`. If occupied, select unused ports in the generated `.tmp/dev-server/deployment.toml` before startup. Do not connect to or stop an installed product instance.

Stop `serve` with Ctrl+C before removing `.tmp/dev-server`. For `run start` background mode, use the same executable and directory with `run stop`, then confirm `run status` is stopped before cleanup. Do not delete the entire `.tmp` directory or default user data. See [MCP Server](../server/README.md).

### Path 3: full desktop

Install native desktop and pnpm dependencies. Build and initialize the isolated MCP Server as in Path 2, but do not run `serve`. Stop it first if already running, then launch:

```sh
pnpm --dir client dev:desktop
```

The first run compiles Rust and opens a development window. The committed `tauri.dev.conf.json` uses a separate identifier / product name for configuration and single-instance isolation. Debug builds use a separate keyring service so installed administrator credentials are not overwritten. Do not run unconfigured `tauri dev` for development. Select only this run's test executable and data directory; do not install services or edit real agent configuration.

Open Configurations & MCP Server and choose local management. Select absolute paths to `server/bin/dbmcp[.exe]` and `.tmp/dev-server`. Initialize the directory to synchronize its management port while preserving existing configuration and identity, then start and connect. Check health and the empty instance list. Local startup synchronizes custom ports; do not reconnect using stale default addresses.

Exit the development desktop, stop any remaining test MCP Server, and clean up only this run's data and configuration. Full desktop development requires a real graphical session.

### Troubleshooting

- pnpm version mismatch: use the pinned `packageManager` version; do not delete lockfiles to resolve conflicts.
- Tauri compilation: check native dependencies, MSVC, and SDK before changing checks.
- Desktop connection: verify executable, directory, ports, and MCP Server identity. Raw `admin.token` text is not an encoded token.
- Linux credentials: verify Secret Service and session unlock; do not fall back to plaintext credential files.
- No database installed: ordinary Go tests need none; container test entry points create temporary databases.

## 1. Collaboration and Git

Use your own Git identity, not the author's. `main` is the integration branch; delete topic branches after merge. Current task authorization and contribution guidance govern commits, merges, and remote operations. Preserve uncommitted files, others' history, and global Git configuration.

Read relevant architecture and call chains before editing. Commit required sources, documentation, and lockfiles. See [Contributing](../CONTRIBUTING.md). Historical implementation authorization is not a default for other contributors.

## 2. Text and line endings

`.gitattributes` is authoritative: text uses LF in both index and worktree; only `.bat` / `.cmd` use CRLF. Binary files explicitly disable conversion. `.editorconfig` specifies UTF-8 without BOM, final newlines, and indentation; Prettier and rustfmt enforce LF.

Do not rewrite unrelated files for line endings or personal formatting. Isolate necessary normalization of imported text. A cross-platform checkout should not produce whole-file diffs. Check with `git ls-files --eol` and `git diff --check`.

External agent configuration belongs to the user. Preserve original line endings and unrelated content where possible; repository rules do not authorize reformatting user files.

## 3. Formatting tools

| Language / files             | Tool                             | Configuration                                                                                                             |
| ---------------------------- | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| Go                           | `gofmt`                          | Standard Go style; `go vet` for static checks                                                                             |
| Rust                         | `cargo fmt` / rustfmt            | Root `rustfmt.toml`, 100 columns, Unix newlines; clippy for shell code                                                    |
| TS / TSX / JS / JSX / MJS    | Prettier                         | Root `.prettierrc.json`, 2 spaces, double quotes, semicolons, 100 columns; strict TS and minimal recommended ESLint rules |
| CSS / JSON / YAML / Markdown | Same Prettier                    | No separate formatter per file type; preserve authored prose wrapping                                                     |
| SQLite migration SQL         | Prettier + `prettier-plugin-sql` | SQLite dialect and uppercase keywords for `server/migrations/**/*.sql`                                                    |
| TOML                         | Taplo                            | `.taplo.toml`, no automatic key reordering, 100 columns                                                                   |

Locked versions: pnpm 10.27.0, Prettier 3.9.10, prettier-plugin-sql 0.20.0, and `@taplo/cli` 0.7.0 (reports Taplo 0.9.0). Commit `pnpm-lock.yaml` and avoid incidental formatter upgrades.

Prefer Node MJS for repository scripts rather than duplicate PowerShell / Bash implementations. Configure formatting whenever adding another script language; do not introduce Python or a custom formatter for a simple build step. Target database SQL samples remain separate from SQLite migrations; add suitable Prettier overrides for additional dialects.

Toolchain versions are pinned in `go.mod`, `rust-toolchain.toml`, `.node-version`, and package engines.

```sh
pnpm install --frozen-lockfile
pnpm format
pnpm format:check
git diff --check
```

Choose checks for the affected scope:

```sh
gofmt -w server/cmd server/internal
go -C server vet ./...
go -C server test ./...
cargo fmt --manifest-path client/src-tauri/Cargo.toml
cargo clippy --manifest-path client/src-tauri/Cargo.toml --all-targets -- -D warnings
pnpm --dir client typecheck
pnpm --dir client lint
```

`pnpm format` and its check mode invoke gofmt, cargo fmt, Prettier, and Taplo. Check mode does not edit files and fails if any tool fails. ESLint uses recommended TypeScript / React and React Hooks rules; Prettier owns layout. Do not allow global clippy warnings without reason or introduce broad Go lint dependencies that complicate simple code.

## 4. Dependencies and implementation

Investigate mature libraries and system APIs first. Prefer Go `net/http`, `database/sql`, and `slog` with the official MCP SDK, Cobra, goose, and an established service library. No ORM, DI container, generic plugin framework, or global event bus is needed.

Add dependencies for actual capabilities. Use shadcn / Radix components, React Hook Form / Zod for forms, and TanStack Query for remote state. Business logic stays in Go; Rust remains a thin integration layer.

Document new dependencies' purpose, license, version, and platform effects. Lock Go, Cargo, and pnpm dependencies. Record source attribution and licenses when extracting code, without inventing copied-code provenance for design references. Keep Go module verification enabled; do not disable modules, modify GOROOT, or globally alter proxies based on outdated instructions.

## 5. Validation scope

| Change                                  | Appropriate validation                                                                 |
| --------------------------------------- | -------------------------------------------------------------------------------------- |
| Ordinary UI, documentation, simple CRUD | Relevant formatting, type, or compilation checks; walk an affected flow when necessary |
| Identity, policy, SQL, Mask, approvals  | Focused regressions for changed business boundaries                                    |
| Complete user flow                      | One integration pass through upstream and downstream contracts                         |
| Release candidate                       | Relevant databases, target OS, both client packages, and recovery behavior             |

Do not test every button, setter, or forwarding method by mirroring implementation or chasing coverage. Do not repeat full integration matrices and screenshot sweeps after small edits. After checks pass, continue delivery; broaden checks only for new changes, failures, or unresolved concerns.

Keep regressions for credential isolation / revocation, hard denials, write CTEs / multiple statements, sensitive predicates and aliases, mandatory Mask, approval binding / expiry / concurrent resumption, unknown write outcomes, external configuration preservation, and consistent online SQLite backup.

### Temporary database containers

All target database integration tests, including PostgreSQL, MySQL, SQL Server, DM8, PostGIS, and TimescaleDB, create temporary containers. Do not use host-installed databases or existing business instances. Embedded SQLite tests use temporary files or memory and clean up afterward.

Choose a runtime based on the development platform, installed tools, current availability, and image architecture. Multiple runtimes are not required.

| Platform | Options                                                          |
| -------- | ---------------------------------------------------------------- |
| Windows  | WSLC (Windows Subsystem for Linux container CLI), Docker, Podman |
| macOS    | Apple Container, Docker, Podman                                  |
| Linux    | Docker, Podman                                                   |

Prefer available database images and record source, tag / digest, database version, and architecture. **If no suitable image is available, skip that database with a specific reason and continue other work.** A skip is not a pass. An available image with a failing product test is a failure, not a missing-image skip. If no runtime is available, report that limitation rather than using host databases.

**Any available image build method may produce dedicated test images**, including Dockerfiles, Containerfiles, or another builder's supported input. The builder and runtime may differ if the runtime can load the image. Building a custom image is optional, not a prerequisite for skipping.

When needed, put build files, initialization SQL, and instructions under `server/tests/containers/<database>/`. Pin base images and database sources, observe licenses, and prefer native runtime commands with small adapters rather than a new orchestration framework. Configure formatting for added files.

Use this flow: select runtime and image → create isolated containers and data → initialize and test → save concise results → clean up. **Dispose of test resources after each run:**

- Give resources explicit names or labels and operate only on resources created for the run.
- Remove containers, volumes, networks, mounted temporary data, and dedicated test images; do not reuse their data for another run.
- Clean up on success, failure, and interruption. Keep only necessary records without credentials; do not leave resources running by default for debugging.
- Do not run global prune operations or delete existing databases, unrelated images, or unrelated build caches.

Report pass, failure, or `skipped` with reasons and actual image / engine versions. The four databases remain product targets, but missing images do not require maintaining a permanent custom environment or block other authorized work.

## Builds and integration tests

Use Playwright or Tauri automation for UI tests, not computer use. On Windows, an isolated test WebView2 can use a temporary CDP port for Playwright to connect to the real Tauri page; production builds do not enable it by default. Distinguish real IPC, file-dialog automation adapters, and browser demo data. Mock results are not real MCP Server validation. Check Start menu entries, installers, and resource hashes with installation tests and system file APIs. Put screenshots / traces in ignored `output/playwright/`. NSIS repair hooks are generated from Prettier-managed MJS templates.

For Windows installer migration, run `node scripts/test-windows-installer.mjs --previous-script=<old-installer.nsi> --report=<result.json>`. Preserve the old generated script and all readable resource inputs before rebuilding. The test compiles an independently identified NSIS installer and performs silent install / migration / uninstall without opening a GUI or replacing real product registry entries, shortcuts, or profiles. It defaults to the local x64 release generation directory; `--current-script` overrides it. `package-windows-installer.mjs` pins template provenance and hashes. An old installer executable or copied script alone cannot reproduce migration; skip this test if the required old build inputs are unavailable.

For bundled installation upgrade regressions, add `--previous-server=<older-dbmcp.exe>`. The test creates isolated running and stopped managed deployments, independent HTTP / external deployments, and a directly launched bundled process. It checks upgrades, credentials, profile preservation, repeat installs without extra snapshots, failure / retry, and cleanup. Legacy local CLI compatibility can be checked with `DBMCP_TEST_SERVER` and `DBMCP_TEST_LEGACY_SERVER` using `cargo test --manifest-path client/src-tauri/Cargo.toml --locked older_server_local_start_attach_and_stop_without_upgrade -- --ignored`.

Package with `pnpm package:server --platform=windows|linux|darwin --arch=amd64|arm64 [--dm=native]` or `pnpm package:client --server=none|bundled --target=<Rust-triple> [--dm=native]`. Desktop builds require the native target OS. Pure clients neither invoke Go nor package leftover `binaries/` resources. Default packages omit DM due to unconfirmed redistribution terms; local `dm-local` packages state their restrictions. Project MIT does not replace dependency licenses.

Standalone archives use archiver 8.0.0 (MIT). Inventories come from actual Go dependencies, Cargo metadata, and pnpm production dependencies, with upstream license texts, supplemental provenance / hashes, and exact source URLs. Windows NSIS 3.13 and Tauri utilities 0.5.3 match CLI 2.12.4; official assets are downloaded through the API and verified against pinned hashes.

Tauri 2.12.4 patches the installer executable with bundle-type metadata and restores the unmodified build output afterward, as confirmed in the pinned bundler implementation. Manifest `unbundled_app_sha256` describes the restored output, not the installed executable. Verify distribution with `installer_files` and `SHA256SUMS`; resource manifests verify individual packaged resources.

Real agent integration uses `node scripts/test-agent.mjs` after logging into the official CLI. Set `DBMCP_TEST_CODEX_JS` for Codex; set `DBMCP_TEST_AGENT=claude` and `DBMCP_TEST_CLAUDE` for Claude. `DBMCP_TEST_SERVER` selects a packaged executable; `DBMCP_TEST_RUNTIME` selects the container runtime.

## Test commands and CI

Client and MCP Server versions are maintained separately. The root package, client package, Tauri configuration, Cargo package, and Cargo lock use the client release version. `server/internal/model/model.go` retains its version for client-only changes; when MCP Server code changes, update it to the accompanying client release version. Package manifests record both component versions, and standalone archives use the MCP Server version. A client version bump does not raise the API compatibility floor; raise that floor only for required API changes.

Basic checks are `pnpm format:check`, frontend typecheck / lint / build, `go -C server test ./...`, `go -C server vet ./...`, `cargo test --manifest-path client/src-tauri/Cargo.toml --locked`, and clippy. For API changes, run `pnpm generate:api` and review `docs/openapi.json` and `client/src/lib/contract.ts`.

Build `server/bin/dbmcp[.exe]` before `pnpm smoke:runtime`. This uses isolated temporary MCP Server processes and no target database.

Database tests: `pnpm test:databases --runtime=docker --drivers=postgres`, with explicit `podman` / `wslc` and comma-separated drivers also supported. Docker is the default and no runtime is installed automatically. Apple Container has no script adapter yet. Runtime-assigned host ports are read dynamically. Results go to `artifacts/database-validation-*.json`. Exit code 2 means all skipped, 1 means failure / interruption / cleanup failure, and 0 means at least one pass and no failures. Read partial-skip reports. Fixed passwords are only for isolated loopback test containers.

Run SCM tests in an authorized administrator terminal: `node scripts/test-windows-service.mjs --original=<old-executable> --candidate=<different-build>`. The inputs can be retained old / new default builds without DM; the script verifies different hashes, manages uniquely named test resources, checks data retention on uninstall, and cleans up.

Run UAC tests from a non-elevated terminal: `node scripts/test-windows-elevation.mjs --server=<test-executable>`, handling the native UAC prompt manually. SCM and UAC tests are not part of automatic PR checks.

GitHub workflows run on Windows x64 only. Ordinary PRs need no secrets, administrator rights, target databases, or agent login. PRs and version tags share source, contract, and isolated runtime checks. Ordinary `main` pushes do not trigger publication. Version tags independently trigger validation, builds, package verification, and GitHub Release publication. Optional manual packaging uploads Actions artifacts only. Publication uses `GITHUB_TOKEN` with `contents: write` while builds remain read-only. Check the version guard with `node --test scripts/release-version.test.mjs`. Hosted Windows runners do not assume Linux database containers are available. Repository owners configure branch protection and security reporting channels on GitHub.

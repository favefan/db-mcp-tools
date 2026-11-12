# Desktop behavior, packaging, and lifecycle

## 1. Desktop layout

CC Switch informs the gray sidebar, light main area, orange primary actions, thin borders, horizontal cards, quiet status indicators, and compact forms. Its provider business model is not reused.

| Area           | Design                                                                                                                 |
| -------------- | ---------------------------------------------------------------------------------------------------------------------- |
| Window         | Initial 1120×760, minimum 960×640; no system title bar; 52px toolbar for dragging and window controls                  |
| Sidebar        | 200px expanded, 64px collapsed, full height, one brand mark, settings entry and approval count                         |
| Sidebar footer | One active configuration, connection status, and quick switching; process controls live in settings                    |
| Page header    | Icon, title, short count, and one primary action such as adding an instance                                            |
| Lists          | Horizontal cards with name, engine, endpoint, status, recent request, and a few actions; secondary actions use a menu  |
| Status         | Connected, disconnected, disabled, or pending with both text and icons as well as semantic colors                      |
| Details        | Object context in the header, tabs beneath, advanced settings grouped rather than exposed on the home page             |
| Notifications  | Concise Sonner toasts; connection errors offer retry / settings; system approval notifications open pending approvals  |
| Empty state    | One clear next step; disconnected views identify the active configuration without presenting stale caches as live data |
| Theme          | System, light, or dark using shared tokens                                                                             |

Spacing uses 4 / 8 / 12 / 16 / 24 / 32px increments: 24px main horizontal padding, 8px list gaps, 12×16px card padding, 32px controls, 13px body and 12px secondary text. Control corners are 5px; groups and dialogs use 8px. Lists grow naturally. Instance editors keep stable dimensions, position, and footer actions, with internal tab scrolling. Confirmation dialogs expand from a stable top position rather than recentering as content changes.

Page fixes headers and window controls across the five main pages. The root window does not scroll; content does. Content, dialogs, and active tabs reserve `scrollbar-gutter: stable` so expanded restore forms and backup rows do not shift layout. Stack owns 16px gaps between actions, tables, and expanded content, avoiding local margin patches.

Local controls use Go `run status` for the selected executable and directory, refreshed on initial load, target changes, lifecycle operations, and while the page is active. HTTP connection state is not local process state. Running disables start; stopped disables stop / restart; unknown or failed status disables all three. Enabled start is green, stop red, restart yellow, and disabled controls gray. Top-level or nested status JSON updates the same target cache.

Settings categories are General, Configurations & MCP Server, Agent integration, Instance templates, and Data management. The sidebar footer opens a nearby quick-switch menu. Its settings entry opens Configurations & MCP Server, whose full list highlights the active profile with the accent color. Connection options use compact radio controls; deletion is separated on the left from cancel / save.

Close behavior, theme, notifications, and automatic lightweight mode belong to the active profile and save atomically in `profiles.json`. Login startup is shared. Switching away or truly exiting stops the active locally managed MCP Server; existing HTTP-connected MCP Servers continue running.

Automatic lightweight mode initially hides a window closed to the tray, then destroys its window and WebView after three minutes while retaining Rust's Admin connection and the native tray. Reopening cancels the timer; closing again restarts it. Disabling the preference cancels pending release. The tray's lightweight-mode checkbox is immediate: enabling releases the window; disabling recreates and opens it. It reflects current state without changing the automatic preference. Reopening restores the same configuration, not unsaved forms.

The official single-instance plugin lets a repeated Start menu launch restore the main window. The tray offers opening the window, lightweight mode, diagnostic export, and exit; double-click opens the window. NSIS creates a `DB MCP Tools` Start menu folder and repairs missing entries on upgrade, while explicit `/NS` still skips shortcuts.

Rust handles new approval notifications during lightweight mode, respecting notification preferences and deduplicating against requests already seen by the frontend. Notifications direct users to reopen the window through the tray.

Diagnostics are separate from business audit and remain exportable while disconnected. See [Diagnostics](diagnostics.md).

For browser preview, run `pnpm --dir client preview:ui` and open `http://127.0.0.1:1420/`. In-memory fixtures support browsing and editing without connecting to databases or writing agent configuration. The adapter loads only in explicit Vite development mode and never participates in production authorization.

shadcn / Radix supply dialogs, sheets, tabs, selects, switches, dropdowns, popovers, and tooltips. Lucide supplies icons, React Hook Form / Zod forms, TanStack Query remote state, and CodeMirror SQL display / simulation. There is no charting or drag-and-drop framework or custom base component system.

```text
┌────────────────┬──────────────────────────────────────────┐
│ DB MCP Tools   │ Database instances        + Add instance │
│                ├──────────────────────────────────────────┤
│ Instances      │ [PG] Development       Connected  [...]  │
│ Entities       │      127.0.0.1:5432    Read allowed      │
│ Approvals  2   │                                          │
│ Logs           │ [MS] SQL Server Test   Disconnected [...]│
│ Settings       │      192.168.1.20:1433                   │
│                │                                          │
│ default   ●    │                                          │
└────────────────┴──────────────────────────────────────────┘
```

## 2. Main interactions

### First launch

First launch creates a disconnected `default` profile. The editor chooses local management or an existing MCP Server. Bundled packages discover the local executable; pure clients can select a separately installed one.

A new local profile with an empty data path receives an isolated directory and available loopback ports. Existing deployments retain addresses. Startup verifies `server_id` and API version. Instance creation follows connection → test → scope / policy → save. Mask starts empty; add rules manually or explicitly copy a template.

### Instances and entities

Instances have Connection, Scope, Permissions, Mask, Entities, Records, and Advanced tabs. The catalog tree selects databases / schemas / tables / columns. Advanced holds pooling, timeouts, extensions, and diagnostics. Stored passwords show six fixed dots, never submitted as a password; only new input replaces them. New connections default to TLS off.

Access logs use the shared table. Clicking a row or its detail action opens a fixed-width right drawer with non-modal Radix Dialog content. Filters, cursor, and list position remain; only drawer content scrolls, and title / actions stay fixed. Adjacent navigation and per-request export are available. Pages, dialogs, and drawers initially focus their container rather than an arbitrary control, while explicit and Tab focus remain.

Entity details show effective resources, operations, approval mode, and Mask per instance, plus credential state and recent requests. Disabling an entity immediately affects subsequent requests and pending execution without removing history.

### Approvals

Cards show entity, instance, operation, target tables, time, and expiry countdown. Details show SQL, expandable parameters, and risk hints. The main action is allow once, alongside deny and remember rule. Remembered rules show actual scope; there is no blanket permanent grant for all agent actions.

Entity registrations and SQL approvals have separate type labels. API decision success means the decision was saved, not that database execution succeeded. Execution status is shown separately.

### Mask and simulation

Select a source field → choose fixed replacement / partial retention / NULL / regex / denied access → enter a sample → preview → save. Display instance and entity rule provenance and ordering.

Simulation accepts entity, instance, and SQL, returning allow / deny / pending and matched reasons through actual MCP Server analysis without executing SQL. Do not duplicate policy decisions in TypeScript.

## 3. Communication and local integration

React uses typed API wrappers. Rust uses `reqwest` for Admin, holds keyring credentials, and forwards state events, avoiding WebView CORS configuration and persistent administrator tokens.

Rust handles windows, tray, notifications, Admin connections, file dialogs, MCP configuration installation, and Go CLI invocation. Arguments are structured rather than interpolated into shell commands. The frontend cannot submit arbitrary system commands. Desktop system service entry points are not exposed.

The client stores named profiles, keyring references, and preferences only. One profile is active at a time. Local management owns lifecycle; existing MCP Servers connected over HTTP, including loopback, remain independent.

## 4. Distribution packages

Bundling the MCP Server is an explicit build choice with separate installers.

| Package               | Contents                                     | Purpose                                                                    |
| --------------------- | -------------------------------------------- | -------------------------------------------------------------------------- |
| Standalone MCP Server | `dbmcp`, documentation, third-party licenses | Headless MCP Server / CLI / Bridge / optional CLI service management       |
| Client only           | Tauri client, no Go executable               | Existing MCP Server connections or a separately installed local executable |
| Bundled MCP Server    | Same client plus platform-matched `dbmcp`    | Local management immediately after installation                            |

```sh
pnpm package:client --server=none --target=x86_64-pc-windows-msvc
pnpm package:client --server=bundled --target=x86_64-pc-windows-msvc
pnpm package:server --platform=windows --arch=amd64
```

Node scripts validate arguments first. Bundled mode compiles Go as `binaries/dbmcp-<target-triple>[.exe]` and generates a Tauri override. Pure mode neither compiles Go nor includes `externalBin` resources. Base configuration excludes the MCP Server so stale test binaries cannot enter a pure client.

| Tauri target                                             | Go target                                  |
| -------------------------------------------------------- | ------------------------------------------ |
| `x86_64-pc-windows-msvc`                                 | `windows/amd64`                            |
| `aarch64-pc-windows-msvc`                                | `windows/arm64`, subject to driver support |
| `x86_64-apple-darwin` / `aarch64-apple-darwin`           | `darwin/amd64` / `darwin/arm64`            |
| `x86_64-unknown-linux-gnu` / `aarch64-unknown-linux-gnu` | `linux/amd64` / `linux/arm64`              |

Windows produces NSIS, macOS DMG, and Linux deb / AppImage. Tauri requires native builds; Go can cross-compile subject to drivers. Unsupported or unlicensed DM8 targets must be identified rather than claiming all four drivers everywhere.

Since Windows 1.0.4, the default application path is `%LOCALAPPDATA%\db-mcp-tools` while the display name and Start menu remain `DB MCP Tools`. Registered legacy paths migrate to `db-mcp-tools` under the same parent. Custom paths remain; `/D` explicitly selects a target. The target must be empty or carry a matching interrupted-migration marker. The installer copies the new package, invokes the old uninstaller with `/S /UPDATE` to remove known program files while retaining data, registers the new location, and fixes shortcuts / startup. Unknown old files are not recursively removed. MCP Server runtime and external agent configuration do not move with the application directory.

The implementation uses Tauri `nsis.template` / `installerHooks` with the pinned CLI 2.12.4 official template and SHA256. MJS generates small adaptations without maintaining a whole copied template. Template updates require reviewing insertion points. Migration logs go to the centralized client log directory and are included in diagnostic exports.

Manifests include application / MCP Server versions, Admin API major, architecture, drivers, hashes, and third-party licenses. No signing certificate or automatic-update channel is assumed; unsigned builds are marked accordingly.

DM8 uses the native Go driver where enabled. ODBC is not required merely because reference documentation mentions it. If native support is insufficient, assess ODBC separately with explicit dynamic libraries, platform driver manager, and redistribution terms rather than maintaining two default installation paths.

Pure clients do not automatically download the MCP Server. STDIO needs a local `dbmcp bridge`, either selected separately or installed first. Otherwise generate supported authenticated HTTP configuration. Do not silently include Go in a client-only package.

## 5. MCP Server lifecycle and services

### Ownership

Go `internal/runtime` owns background process and `kardianos/service` integration. Desktop local operations call the same CLI. `run status --json` and optional CLI `service status --json` return structured status; Rust does not reimplement SCM / systemd / launchd.

| Mode                   | Startup                                         | After client exit                                                      | Restart owner                     |
| ---------------------- | ----------------------------------------------- | ---------------------------------------------------------------------- | --------------------------------- |
| Foreground             | `dbmcp serve`                                   | Independent of desktop; follows the terminal                           | User                              |
| Independent background | `dbmcp run start`                               | Continues, already detached from the terminal                          | User; no custom daemon supervisor |
| Locally managed        | Start or attach when activating a local profile | Stops on true exit or switching away; tray / lightweight mode continue | Client                            |
| OS service             | Start after `dbmcp service install`             | Continues under the system service manager                             | OS service policy                 |

Tauri `externalBin` is a distribution mechanism, not a process lifetime guarantee. The executable is prepared in a stable runtime directory; Go startup does not depend on WebView, shell plugin child handles, or UI job lifetime. Closing to tray retains the process; true exit stops only the active locally managed instance.

`server_id`, data directory, and health responses identify the target. Local process `instance_id` is checked on stop; PID is only supporting information. Port conflicts never justify killing unrelated processes. The desktop exposes no system service operations; connect to services through existing MCP Server HTTP configurations. Each data directory has one state owner.

Local management owns startup and shutdown by default. Existing HTTP connections never control processes, regardless of loopback address. Foreground processes remain controlled by their original terminal.

### Platforms and directories

| Platform | Personal data / stable executable                              | Optional CLI service                                                              | Permissions and persistence                                                                                                                                |
| -------- | -------------------------------------------------------------- | --------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Windows  | `%LOCALAPPDATA%\DBMCP\server`, executable under `runtime/`     | SCM, default `NT AUTHORITY\LocalService`; system data under `%PROGRAMDATA%\DBMCP` | Installation / removal and lifecycle require system authorization; ACLs are restricted to the manager, service account, and required system administrators |
| macOS    | `~/Library/Application Support/DBMCP/server`                   | Current-user LaunchAgent; system LaunchDaemon when explicitly configured          | Independent of UI exit; login / boot behavior depends on selected service mode                                                                             |
| Linux    | `$XDG_DATA_HOME/dbmcp/server` or `~/.local/share/dbmcp/server` | Current-user systemd; dedicated non-root account for system mode                  | UI exit has no effect; logout persistence depends on linger; system data uses `/var/lib/dbmcp`                                                             |

Configuration stores arguments and deployment paths, never service command-line passwords. Actual account, permission, and user-service support depends on the pinned library and platform testing; do not default every OS to elevation for superficial consistency.

CLI conversion to a system service stops the managed process, snapshots state, explicitly selects the target path and account permissions, migrates executable / settings / keys to a stable location, installs, starts, and validates `server_id`. Failures retain original data and recovery paths rather than creating two independent running state stores.

Explicit upgrade stops, backs up, deploys, migrates, starts, and checks health. Versioned executable directories avoid overwriting running Windows executables. Bridge configuration points to the stable local launcher, not temporary unpacking paths. Reconnect agents when required; no background auto-upgrade daemon is introduced.

Service uninstall removes registration and retains data by default. Client-only uninstall does not touch remote MCP Servers. Client login startup and MCP Server service startup remain independent.

## 6. Agent configuration integration

Installation selects the target client and user / project scope, previews paths and changes, issues a registration credential, backs up the target, merges only this tool's entry, and saves atomically. The entity is created on first MCP access and authorized separately. Failures revoke unused credentials and preserve original files.

- Claude Code changes only this tool's `mcpServers` entry in JSON. Codex uses `toml_edit` for `[mcp_servers.<entry>]`, preserving unrelated configuration and comments, and does not emit an unsupported `type` field.
- STDIO arguments carry a restricted Bridge credential file path, not a token. HTTP uses each target client's supported authentication format.
- Removal affects only this tool's entries. Same-name unrelated entries conflict rather than replacing the whole MCP collection.
- Use same-directory temporary files, mature file libraries, backups, and a pre-write modification check. JSON preserves unrelated values; TOML preserves unrelated sections / comments. External line endings are retained rather than forced to repository LF.
- Pass paths, including spaces and non-ASCII characters, through system APIs as arguments rather than shell concatenation. Credentials and backups keep private permissions.

Other clients use generic MCP export rather than duplicating CC Switch's complete compatibility matrix. Installation / update occurs only on explicit request and never continuously rewrites external files.

### Target files and conflicts

Desktop integration supports Codex / Claude Code user and project scope, HTTP / STDIO, entry previews, and removal previews. Codex inherits `CODEX_HOME`, defaulting to `~/.codex/config.toml`; projects use `.codex/config.toml` and must be trusted. Claude Code defaults to `~/.claude.json` and project `.mcp.json`, with its own initial project MCP confirmation.

Rust calls Admin rather than calculating policy. Installation records exclude tokens. Updating this tool's entry revokes the previous credential; failures clean up new credentials.

TOML preserves comments with `toml_edit`. JSON retains unrelated fields / values with two-space formatting. Both retain LF / CRLF. Before writing, validate the preview fingerprint, save a restricted backup, sync a same-directory temporary file, and atomically replace. Other clients do not share the lock; this is optimistic conflict detection, not a lock on external editors. Removal requires an unchanged semantic hash; user-edited entries remain while their previous credential is revoked.

Formats follow the [Codex MCP documentation](https://developers.openai.com/codex/mcp), [Codex configuration reference](https://developers.openai.com/codex/config-reference), and [Claude Code MCP documentation](https://code.claude.com/docs/en/mcp).

### Stable runtime

`dbmcp runtime deploy` requires the data directory to be stopped, saves a consistent snapshot with matching keys, places the build in `runtime/versions/<version>-<hash>/`, writes `current.json`, and retains `previous.json`. `runtime/dbmcp[.exe]` forwards to the hash-verified executable. CLI services register the actual versioned executable so SCM owns the service process. Repeated deployment of the same build does not recopy it. Ordinary local startup uses `runtime prepare` for first-time setup without a backup or upgrade.

On Windows, reopening state validates existing ACLs without requiring LocalService WRITE_DAC. Initial creation uses `x/sys/windows` private DACLs and rejects shared-group access. Service accounts get data Modify and executable Read / Execute permissions. Service startup waits for listeners, TLS, and runtime identity. Controls have timeouts, and start also waits for Admin health. POSIX system mode requires an explicit non-root user and data ownership.

### Upgrade, migration, and desktop boundaries

`runtime upgrade` coordinates stopping, backup, deployment, existing service registration replacement, and restoration of the original mode. It refuses foreground process takeover. `runtime migrate --to <empty-directory>` consistently copies state / keys, retaining source and snapshots, and can install / start a target service after removing old registration. `runtime rollback-program` selects only the verified `previous.json` executable; state restore remains explicit. Services must be reinstalled to follow the changed executable path. Failures do not automatically roll back business state that may have received MCP writes.

The desktop offers executable / directory selection, bundled executable discovery, stable local startup, explicit upgrade, and independent client login startup. It does not offer service installation, migration, or uninstall. Pure clients without Go ask users to select an executable or connect over HTTP.

For explicit CLI service administration, Windows elevation uses `ShellExecuteExW` with `runas` and individually encoded arguments, with the OS owning UAC and results passed through a private one-time directory. Authorization carries the original desktop user's concrete SID so elevation through a different administrator does not remove that user's access. Existing private directories add only that user; service accounts retain data Modify / executable Read-Execute. Initialization refuses unrelated files, state / secret paths reject symbolic links, and critical Windows files have their own ACL checks to prevent shared access through explicit child permissions.

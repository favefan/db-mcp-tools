# Changelog

Notable changes for users and contributors, listed from newest to oldest.

## [Unreleased]

## [1.0.14] - 2026-11-13

- Unify sidebar item widths and alignment, separate the profile divider from its rounded button, and add a three-state theme pill / collapsed icon button.
- Add About to settings and the tray, including lightweight-window restoration.
- Summarize database access logs by request with authoritative current outcomes, administrator filtering, real execution durations, and complete detail timelines.
- Accept non-blocking Agent explanations of up to 100 Unicode characters, retain them in encrypted history, and display them in approvals, request details, and foreground / lightweight notifications without changing executable retry identity.
- Wait briefly for background reads before desktop lifecycle changes and identify active work in conflict messages; ordinary local status checks now share read permits.

## [1.0.13] - 2026-11-12

### Added

- Backup deletion with confirmation in Data management, automatic list refresh, and restore selection cleanup.
- An authenticated `DELETE /admin/v1/backups/{id}` endpoint, server-issued backup IDs, bounded filesystem operations, and deletion audit events.

### Changed

- MCP Server catches up to client version 1.0.13 for the new endpoint. Older servers retain existing features; only deletion requires 1.0.13.
- Backup deletion rejects path traversal, directory links, and unexpected contents while preserving live state and other snapshots.

## [1.0.12] - 2026-11-12

### Changed

- Client version is 1.0.12; MCP Server remains 1.0.11. Client-only changes retain the MCP Server version; MCP Server changes catch up to the accompanying client release.
- Package names, manifests, verification, and tag-driven release assets use each component's own version.
- Windows bundled installation upgrades older managed MCP Servers before replacing application files, preserving data, credentials, running state, and upgrade safety snapshots.
- HTTP connections, services, and separately selected external executables remain independent. Repeated installation skips unchanged or newer managed versions.
- Removed the blanket 1.0.11 local-management requirement. API compatibility remains at 1.0.2, with supported CLI operations selected for older versions.

## [1.0.11] - 2026-11-11

### Changed

- Renamed environments to configurations, standardized MCP Server terminology, added a sidebar quick-switch menu, and moved full management into Configurations & MCP Server.
- Simplified help text and panel notifications, moved necessary explanations to tooltips, corrected collapsed sidebar and form alignment, and rearranged connection options and the delete action.
- Unified local MCP Server startup, attachment, and shutdown while retaining manual start, stop, and restart. HTTP connections, including loopback addresses, leave the MCP Server running independently.
- Removed desktop system service controls; the MCP Server upgrade action now appears only when a newer executable is available.
- Removed upgrade backups from initial deployment and ordinary startup, and removed scheduled backups. Explicit upgrades, restores, and schema migrations retain safety snapshots.

## [1.0.10] - 2026-11-11

### Added

- An initial `default` environment, named profiles, and a single active session with mutually exclusive local management and existing MCP Server connections.
- Per-profile credentials, theme, notifications, close behavior, and automatic lightweight mode; system login startup remains shared.
- An option to start a local MCP Server on profile activation and stop the instance started by the client on switching or exit.
- Go deployment metadata queries, available loopback port allocation, and process instance identity checks.

### Changed

- Profile switching clears business caches, event subscriptions, and notifications, and rejects switching during active operations or requests from stale sessions.
- Unified window and tray exit handling, with retry or exit-without-stopping options if shutdown fails.
- Migrated legacy connections into `default` while retaining deployment addresses, data, identity, and preferences; client-managed startup was disabled by default.

## [1.0.9] - 2026-11-11

### Added

- Development instructions for frontend preview, MCP Server / CLI, and full desktop, plus contribution and security guidance.
- An isolated desktop development configuration and `dev:desktop` command with separate profiles, single-instance identity, and debug credentials.
- Windows x64 GitHub checks, optional manual unsigned packaging, and version-tag-triggered release publication.

### Changed

- Standardized the Node 24 requirement and formatting after API contract generation.
- Database tests dynamically allocate host ports and report failures, skips, and cleanup results. Service and elevation tests accept explicit executable paths.
- Standardized the use of contributors' own Git identities and simplified usage, development, and architecture documentation.

## [1.0.8] - 2026-11-10

### Added

- An access log request drawer showing SQL, exact parameters, execution summaries, Mask matches, and related events, with adjacent navigation, ID copying, and individual JSON exports.
- Encrypted request history in the MCP Server, separate from approval execution payloads and result caches, with retention cleanup.

### Changed

- New policies default to 5000 rows / 10 MiB; connection TLS defaults to off.
- Stored passwords use a fixed six-dot indicator; pages and dialogs open with neutral focus.

### Fixed

- Access log notification timing so lists do not refresh before audit records are persisted.

## [1.0.7] - 2026-11-10

### Changed

- New instances, bindings, and templates start without Mask rules; users add rules manually or explicitly copy a template.
- Removed separate result content detection in favor of user-configured Mask regular expressions.

### Fixed

- Removed legacy automatic status notes after registration approval while preserving custom notes and grants.
- Clipped content at the bottom of instance dialogs and registration forms.

## [1.0.6] - 2026-11-10

### Changed

- Local MCP Server controls use the selected target's actual status and consistent start, stop, and restart colors.
- Unified backup page spacing, fixed page headers and dialog tabs, and confined scrolling to content areas.

### Fixed

- Incorrectly showing a stopped state after a stop failure.
- Width shifts caused by scrolling lists, expanded backups, and object trees.

## [1.0.5] - 2026-11-10

### Fixed

- Moved MCP Server upgrade prompts to local settings, with inline retry after failure and prompt cleanup after success.
- Standardized long notifications, action spacing, and small-window layouts.

## [1.0.4] - 2026-11-10

### Changed

- Changed the default Windows installation directory to `db-mcp-tools` without spaces, while retaining application identity.

### Added

- Migration from the legacy installation directory, with corrected shortcuts and startup targets while preserving profiles and independent MCP Server data.
- Installer migration logs and retry after interrupted copying.

## [1.0.3] - 2026-11-10

### Added

- Coordinated upgrades and reconnection for managed local MCP Servers, minimum version checks, and legacy API diagnostics.

### Changed

- Unified buttons, tables, lists, and notifications; fixed instance, approval, and settings tab bars.
- Mask editors start with empty rules; examples appear only as placeholders.

### Fixed

- Connecting to an old stable runtime executable after updating the client.
- Focus outlines and content layout in small windows.

## [1.0.2] - 2026-11-10

### Added

- Database-specific selection of databases, schemas, tables, and columns, with object tree multiselect and keyboard navigation.
- Catalog discovery for unsaved connections, separating the default connection database from authorized resources.

### Changed

- Added database-qualified resource keys and unified authorization for discovery, SQL checks, and pre-execution rechecks.

## [1.0.1] - 2026-11-10

### Added

- Single, selected, and all-database scopes for instances, plus MCP database discovery.
- Per-database connection pools sharing the instance concurrency limit; approvals and resumption bind to an exact database.

### Changed

- Retained existing single-database configurations and added frontend scope selection and structure switching.

## [1.0.0] - 2026-11-09

### Added

- A standalone Go MCP Server, management CLI, STDIO Bridge, and Tauri / React desktop client.
- Database instances, entity credentials, unified permissions, SQL classification, write approvals, Mask, limits, and auditing.
- PostgreSQL, MySQL, SQL Server, and optional DM8 drivers.
- Codex / Claude Code configuration integration, generic MCP configuration export, approval notifications, and a system tray.
- Local background processes and CLI system services, upgrades and migrations, backups and recovery, diagnostics, and lightweight mode.
- Standalone MCP Server packages and client installers with or without a bundled MCP Server.

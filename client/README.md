# Client

A Tauri 2 + React + TypeScript desktop client with five navigation entries: instances, entities, approvals, logs, and settings. It includes seven-tab instance editing, policies and Mask rules, local background process management, and light / dark themes.

The frontend lives in `src/`; the Tauri shell and local integration live in `src-tauri/`. The client manages the MCP Server through the Admin API and invokes its Go CLI for local process operations. Database execution, permissions, approvals, Mask, and SQLite logic remain in the MCP Server.

Client-only and bundled MCP Server builds are described in [Client and packaging](../docs/client-and-packaging.md).

Run from the repository root:

```sh
pnpm --dir client generate:api
pnpm --dir client typecheck
pnpm --dir client lint
pnpm --dir client dev:desktop
pnpm --dir client tauri build --debug --no-bundle
cargo clippy --manifest-path client/src-tauri/Cargo.toml --all-targets -- -D warnings
```

Install dependencies using Node 24 LTS and the locked pnpm version. Tauri also requires native platform dependencies; see the [development guide](../docs/development.md) for setup and an isolated development MCP Server. `dev:desktop` uses separate configuration, single-instance identity, and debug keyring storage. Direct `tauri dev` does not isolate the configuration directory.

Rust forwards allowlisted Admin requests and stores administrator credentials in the OS keyring; the WebView does not store tokens. Desktop integration includes upgrades, agent configuration installation / removal, executable and directory selection, and client login startup. System service management is available through the Go CLI, not the desktop.

Build with `pnpm package:client --server=none|bundled --target=<triple>` from the repository root. A pure client does not compile or contain Go. A bundled local DM build additionally requires `--dm=native`. Windows NSIS tools use pinned official versions and hashes, cached under `src-tauri/target/.tauri/`; packaging downloads them from official sources and verifies their hashes.

Business pages share `components/ui.tsx`: Button, DataTable, DataList / DataListItem, Card, ViewTabs, Field, Modal, Notice, and Badge. Button uses Radix Slot for semantic elements such as file selectors. Its primary, secondary, text, and icon variants share sizing, focus, and danger colors. DataTable requires headers and consistent empty states. Use tables for column comparisons and lists for title / description / action groups. Operation notifications use the bottom-right toast area; contextual warnings stay near their relevant content. Do not insert business notices above the page header or window controls. Reuse `styles.css` spacing and color tokens.

Window controls and sidebar navigation retain dedicated interactions. Instance tabs use Radix Tabs, and Headless Tree manages object selection. Inline tree variables represent depth only; shared CSS computes spacing.

Main pages use Page from `components/page-header.tsx`. The window and header stay fixed, and only `.page-content` scrolls with space reserved for its scrollbar. Dialog scrolling belongs to `modal-body` or the active `tab-content`, without additional window scrolling. Stack provides a 16px gap between actions, tables, and expanded content; children do not add contextual margin. Button success / danger / warning tones are shared and disabled controls are gray. Local controls use Go status for the selected executable and data directory and remain disabled until status is known.

# Technical choices and references

Prefer mature dependencies and system APIs with small adapters suitable for a personal tool. Do not copy full reference projects, introduce a second business state store, or add generic frameworks. Lockfiles record exact dependency versions.

## Reference projects

- [CC Switch](https://github.com/farion1231/cc-switch): a reference for desktop cards, settings, tray behavior, lightweight mode, and configuration integration flows. This project retains its independent MCP Server, permission and approval pipeline, and does not require CC Switch at runtime.
- [Bytebase Omni](https://github.com/bytebase/omni): reused PostgreSQL, MySQL, SQL Server, and Oracle dialect ASTs at a pinned commit, with thin adapters for table and column provenance. The Oracle parser supports a common DM syntax subset, not all DM syntax.

## Implementation choices

Go uses standard `net/http`, `database/sql`, and `slog` with the official MCP SDK, Cobra, goose, and an established service library. SQLite provides persistence without an ORM, Redis, message queue, external IAM, or DI container.

The desktop uses Tauri 2, React, Radix, and Headless Tree. The object tree reuses asynchronous caching, multiselect, ARIA, and keyboard support; business policies and database execution remain in Go. React Hook Form / Zod manage forms, TanStack Query manages remote state, and semver handles version semantics.

External TOML uses `toml_edit` to preserve comments. File updates use backups, preview fingerprints, and atomic replacement. Configuration formats follow the [Codex MCP documentation](https://developers.openai.com/codex/mcp), [Codex configuration reference](https://developers.openai.com/codex/config-reference), and [Claude Code MCP documentation](https://code.claude.com/docs/en/mcp).

DM uses its native `database/sql` driver, referencing the [DM native interface guide](https://eco.dameng.com/document/dm/zh-cn/app-dev/go_dm.html). The [ODBC guide](https://eco.dameng.com/document/dm/zh-cn/app-dev/go_odbc.html) is an alternative reference only; the project does not maintain two driver management systems. Use Go Modules normally rather than copying source into GOROOT or disabling module verification. See [Dependencies and limitations](dependencies.md) for license and TLS restrictions.

## Provenance and licenses

Document each new dependency's purpose, license, and platform effects. Extracted upstream code must retain its pinned commit, modification notes, and original license; design references do not imply copied code. Distribution packages include actual dependency inventories, license texts, and exact source URLs. `third-party/notices.json` records supplemental text sources and hashes. The project's MIT license does not replace upstream terms.

# Dependencies and limitations

Exact versions are recorded in `server/go.mod`, `client/src-tauri/Cargo.lock`, and `pnpm-lock.yaml`. The source uses MIT; see [Third-party licenses](../third-party/README.md) for dependency licenses and provenance.

## Main dependencies

| Capability             | Choice                                           | Use and constraints                                                                                      |
| ---------------------- | ------------------------------------------------ | -------------------------------------------------------------------------------------------------------- |
| SQL AST                | Bytebase Omni, pinned commit `e500bc714251`      | Dialect parsing and field provenance; unknown nodes are rejected rather than allowed by keyword matching |
| MCP                    | Official Go SDK                                  | Streamable HTTP and STDIO Bridge; each request authenticates independently                               |
| Proxy source           | realclientip-go                                  | Forwarding chains are parsed only for explicitly trusted proxies, after checking the real peer           |
| SQLite                 | modernc.org/sqlite, goose                        | WAL, migrations, conditional execution claims, online backup, and exclusive data directory locking       |
| PostgreSQL             | pgx stdlib                                       | `database/sql` driver, basic extension metadata, and non-executing Explain                               |
| MySQL                  | go-sql-driver/mysql                              | Bound parameters, connection pools, and catalog discovery                                                |
| SQL Server             | go-mssqldb                                       | Named parameters, OUTPUT, and multi-database access                                                      |
| DM8                    | gitee.com/chunanyong/dm, optional `dm` build tag | Native `database/sql` interface; omitted by default                                                      |
| CLI / services         | Cobra, kardianos/service                         | CLI, background processes, and platform system services                                                  |
| Configuration / locks  | go-toml/v2, toml_edit, flock                     | Go deployment settings, external TOML comment preservation, and mutual exclusion                         |
| Desktop                | Tauri 2, keyring, reqwest                        | Credentials, local integration, and an allowlisted Admin proxy; no business SQLite access                |
| Frontend               | React, TanStack Query, React Hook Form, Zod      | Pages, remote state, and forms                                                                           |
| Interaction            | Radix, Headless Tree, Lucide                     | Dialogs, tabs, popovers, tooltips, object tree multiselect, keyboard navigation, and icons               |
| Local profile identity | uuid (v4), tempfile                              | Random profile IDs and atomic saves in the same directory; no extra business state database              |
| Version comparison     | semver                                           | Minimum MCP Server versions and available upgrade prompts in frontend and Rust                           |
| Logs                   | slog, lumberjack, tauri-plugin-log               | Per-PID files, rotation, and diagnostic exports                                                          |
| Packaging              | archiver, Tauri NSIS tools                       | Standalone packages, Windows installers, and checksum manifests                                          |

## SQL and database boundaries

Common SELECT / DML, read CTEs, aliases, JOINs, subqueries, and field provenance checks are supported. Write CTEs, multiple statements, unknown provenance, and unsupported syntax are explicitly rejected or handled under the common write policy. Cross-database joins, arbitrary stored procedures, arbitrary SQL rewriting, and complete cross-dialect lineage are outside the current scope.

Instances maintain pools per exact database while sharing the instance concurrency limit. Discovery, resource scope, SQL checks, and pre-execution rechecks use the same authorization. User-configured instance Mask rules and additional entity rules pass through the same result pipeline.

PostgreSQL Explain returns non-executing plans and estimates only; other dialects do not expose Explain. PostGIS / TimescaleDB support is limited to basic recognition and metadata display. The object tree loads a full catalog per database without virtualization or in-tree search; select whole databases or schemas when catalogs are large.

The DM driver cannot enforce and verify client TLS. Only connections with TLS explicitly disabled over a local or protected tunnel are accepted; this is not verified TLS support. Its independent redistribution license has not been confirmed, so public packages omit DM. Explicit `--dm=native` builds produce local-use `dm-local` packages. The project's MIT license does not cover the upstream driver.

## Platforms and configuration

The desktop requires native OS dependencies, a graphical session, and system credential storage; see the [development guide](development.md). The MCP Server can be cross-compiled; the desktop must be built on its target OS. GitHub CI runs on Windows x64 only.

External MCP configuration updates use preview fingerprints, restricted backups, and atomic saves in the same directory. TOML preserves unrelated comments; JSON preserves unrelated values with two-space formatting. Other processes do not share the project's lock, so conflict detection is optimistic and does not lock external editors. Removal affects only this tool's entries whose semantic hash still matches.

Backups include a consistent SQLite snapshot, matching keys, credentials, and a hash manifest. Restore requires shutdown. Configuration imports exclude secrets and create disabled new objects without replacing existing state. See [Architecture](architecture.md) and the [user guide](user-guide.md).

# Project documentation

Current usage, development, and design documentation for DB MCP Tools. The MCP Server / CLI / Bridge live in `server/`; the desktop client lives in `client/`.

| Document                                        | Contents                                                                                      |
| ----------------------------------------------- | --------------------------------------------------------------------------------------------- |
| [User guide](user-guide.md)                     | Installation, first connection, agent integration, approvals, Mask, backups, and recovery     |
| [Development guide](development.md)             | Toolchains, platform prerequisites, three development paths, formatting, tests, and packaging |
| [Product scope](product.md)                     | Features, defaults, and security boundaries                                                   |
| [Architecture](architecture.md)                 | State ownership, execution pipeline, identity, permissions, approvals, and APIs               |
| [Client and packaging](client-and-packaging.md) | Desktop interaction, local lifecycle, installers, and configuration integration               |
| [Dependencies and limitations](dependencies.md) | Dependencies, SQL scope, drivers, and distribution restrictions                               |
| [Technical choices](research.md)                | References and reasons for reuse                                                              |
| [Diagnostics](diagnostics.md)                   | Log locations, exports, and troubleshooting                                                   |
| [OpenAPI](openapi.json)                         | Management API contract generated from Go models                                              |

See [Contributing](../CONTRIBUTING.md) for the contribution process and [Security](../SECURITY.md) for sensitive information and vulnerability reports.

See [Release notes](../RELEASE.md) and [Changelog](../CHANGELOG.md) for version information.

The Go MCP Server owns business state, SQLite, permissions, approvals, and Mask. Rust handles desktop and local integration. `main` is the integration branch; contributors use their own Git identities. GitHub workflows run on Windows x64 only, and desktop builds require the target OS.

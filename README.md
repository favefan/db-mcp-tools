# DB MCP Tools

A database MCP manager for individual developers. The standalone Go MCP Server also provides a management CLI and STDIO Bridge. The Tauri / React desktop client manages instances, entities, permissions, approvals, Mask rules, logs, and locally managed MCP Servers.

All business state belongs to the MCP Server. The desktop controls the lifecycle of locally managed instances; MCP Servers connected over HTTP run independently. PostgreSQL, MySQL, SQL Server, and optional DM8 are supported. Default builds omit the DM driver because its redistribution license has not been confirmed; local DM builds are not public distribution packages.

## Platforms and builds

The MCP Server supports Windows, macOS, and Linux. The desktop requires each platform's native Tauri toolchain. GitHub workflows run on Windows x64 only: PR checks, optional manual unsigned packaging, and release validation, builds, and publication triggered by version tags. See the [development guide](docs/development.md) for prerequisites and driver restrictions.

## Getting started with development

Run commands from the repository root. Choose the environment that matches your work; the [development guide](docs/development.md) covers setup, connection, shutdown, and cleanup.

| Path                            | Required tools                                                                 | Entry point                                                                            |
| ------------------------------- | ------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------- |
| Frontend preview with mock data | Node 24.22.2, pnpm 10.27.0                                                     | Install dependencies, then run `pnpm --dir client preview:ui`                          |
| MCP Server / CLI                | Go 1.27.3                                                                      | `go -C server build -o bin/dbmcp.exe ./cmd/dbmcp` produces `server/bin/dbmcp.exe`      |
| Full desktop                    | Node / pnpm, Rust 1.96.1, native build dependencies; Go for a local MCP Server | Initialize an isolated development directory, then run `pnpm --dir client dev:desktop` |

On Linux / macOS, use `bin/dbmcp` as the MCP Server output name. Do not use an installed application's data directory for initial development. Containers are needed only for real database integration tests.

## Documentation and contributions

- [v1.0.14 release notes](RELEASE.md) · [Changelog](CHANGELOG.md)
- [Installation and usage](docs/user-guide.md)
- [Development setup and checks](docs/development.md)
- [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md)
- [Design and architecture](docs/README.md) · [MCP Server](server/README.md) · [Client](client/README.md)
- [Third-party licenses](third-party/README.md)

## Local packaging

```sh
pnpm package:server --platform=windows --arch=amd64
pnpm package:client --server=none --target=x86_64-pc-windows-msvc
pnpm package:client --server=bundled --target=x86_64-pc-windows-msvc
```

Packages and `SHA256SUMS` are written to `artifacts/releases/<version>/`, currently `1.0.14`. Build the desktop on its target OS; the MCP Server can be cross-compiled. Use `--dm=native` only for explicitly marked local DM builds. See the development guide for prerequisites and test commands.

The source code is licensed under [MIT](LICENSE). Third-party licenses remain applicable.

Client and MCP Server versions are independent. Client 1.0.14 includes MCP Server 1.0.14. Client-only changes retain the MCP Server version; MCP Server changes catch up to the accompanying client release. Minimum MCP Server requirements change only when required APIs change.

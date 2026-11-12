# DB MCP Tools v1.0.12

Client **1.0.12** ships with MCP Server **1.0.11**. This release fixes bundled installation upgrades and separates client and MCP Server version maintenance.

## Highlights

- Windows bundled installers automatically upgrade older client-managed MCP Server deployments during installation, with a safety snapshot and preservation of their running or stopped state.
- Existing HTTP connections, independent services, and separately selected external executables are left alone. Matching or newer managed MCP Server versions are not upgraded again.
- Installation failures are reported before application replacement completes, with recovery information in the installer log and a retry option.
- Client-only changes no longer require an MCP Server version bump. When MCP Server code changes, its version catches up to that client release.
- Removed the blanket MCP Server 1.0.11 requirement for local management. The existing API compatibility floor remains 1.0.2; newer CLI operations are used only when supported.

## Downloads

| File                                                         | Purpose                                                                           |
| ------------------------------------------------------------ | --------------------------------------------------------------------------------- |
| `db-mcp-tools-client-bundled-1.0.12-windows-amd64.setup.exe` | Desktop client 1.0.12 with MCP Server 1.0.11; recommended for local use           |
| `db-mcp-tools-client-none-1.0.12-windows-amd64.setup.exe`    | Desktop client only, for existing MCP Servers or separately installed executables |
| `dbmcp-server-1.0.11-windows-amd64.zip`                      | Standalone MCP Server 1.0.11, management CLI, and STDIO Bridge                    |
| `SHA256SUMS`                                                 | Checksums for all six release attachments                                         |

Installers are unsigned. Public packages include PostgreSQL, MySQL, and SQL Server drivers; optional local DM8 builds are not included. The attachments also include client build manifests and `source-maps.zip`.

## Upgrading

Run the bundled Windows installer to update the client and older managed local MCP Servers together. No separate in-app upgrade action is needed for those deployments. Profiles, credentials, business data, and permissions are preserved. Reinstalling with an unchanged MCP Server version does not create another upgrade snapshot.

Client-only installations and independently maintained MCP Servers keep explicit upgrade controls. Ordinary application startup and profile switching do not upgrade or back up the MCP Server.

See the [user guide](docs/user-guide.md) for setup and recovery, the [development guide](docs/development.md) for platform prerequisites, and the [changelog](CHANGELOG.md) for the full history.

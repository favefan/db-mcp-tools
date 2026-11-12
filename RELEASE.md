# DB MCP Tools v1.0.11

This update simplifies configuration management and desktop interaction, and unifies the lifecycle of locally managed MCP Servers.

## Highlights

- Renamed environments to configurations and grouped management under **Configurations & MCP Server**. The sidebar menu supports quick switching.
- Highlighted the active configuration with the accent color, compacted connection options, and corrected collapsed sidebar and form alignment.
- Removed unnecessary explanations, moved essential help into tooltips, and placed operation notifications in the bottom-right corner.
- Locally managed MCP Servers now start, attach, and stop with the client. Manual start, stop, and restart remain available. HTTP connections, including `127.0.0.1`, leave process control to the existing MCP Server.
- Removed desktop system service controls. The MCP Server upgrade action appears only when a newer executable is available.
- Removed scheduled backups and backups on startup or configuration switching. Manual backups and safety snapshots before upgrades, restores, and schema migrations remain available.

## Downloads

Choose a Windows x64 package from the release assets:

| File                                                         | Purpose                                                                                            |
| ------------------------------------------------------------ | -------------------------------------------------------------------------------------------------- |
| `db-mcp-tools-client-bundled-1.0.11-windows-amd64.setup.exe` | Desktop client with a local MCP Server; recommended for first-time users                           |
| `db-mcp-tools-client-none-1.0.11-windows-amd64.setup.exe`    | Desktop client only; connect to an existing MCP Server or select a separately installed executable |
| `dbmcp-server-1.0.11-windows-amd64.zip`                      | Standalone MCP Server, management CLI, and STDIO Bridge                                            |
| `SHA256SUMS`                                                 | SHA256 checksums for the release assets                                                            |

Installers are unsigned. Default packages include PostgreSQL, MySQL, and SQL Server drivers. DM8 is available only through an explicit local build and is not included in public packages. See the [development guide](docs/development.md) for native macOS / Linux builds.

The assets also include two client build manifests and `source-maps.zip`. `SHA256SUMS` covers all six attachments other than the checksum file itself. Download the files into the same directory to verify them.

## Upgrading

Run the installer to update an existing client while retaining its data. A bundled client can upgrade its managed local MCP Server. For standalone or remote deployments, back up and update the executable as described in the [user guide](docs/user-guide.md).

Existing instances, entities, policies, connection passwords, configurations, and credentials are preserved. This version adds no SQLite schema migration. Local management requires MCP Server 1.0.11 or later; the old lifecycle toggle is no longer used. Use an HTTP configuration if the MCP Server should run independently. Ordinary startup does not upgrade it automatically; select the upgrade action in settings.

See the [changelog](CHANGELOG.md) for the full history and the [user guide](docs/user-guide.md) for installation and configuration.

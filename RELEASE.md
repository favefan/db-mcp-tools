# DB MCP Tools v1.0.13

Client and MCP Server **1.0.13** add deletion of saved backups.

## Highlights

- Delete individual backups from Settings → Data management → Backup and restore, with confirmation before removal.
- Refresh the backup list after deletion and clear the restore selection if it pointed to the deleted snapshot.
- The MCP Server handles deletion through an authenticated administrator endpoint using a server-issued backup ID. Paths outside the backup directory, directory links, and unexpected contents are rejected.
- Existing MCP Servers remain compatible. Only backup deletion requires MCP Server 1.0.13; the global API compatibility floor remains 1.0.2.

## Downloads

| File                                                         | Purpose                                                                           |
| ------------------------------------------------------------ | --------------------------------------------------------------------------------- |
| `db-mcp-tools-client-bundled-1.0.13-windows-amd64.setup.exe` | Desktop client with MCP Server 1.0.13; recommended for local use                  |
| `db-mcp-tools-client-none-1.0.13-windows-amd64.setup.exe`    | Desktop client only, for existing MCP Servers or separately installed executables |
| `dbmcp-server-1.0.13-windows-amd64.zip`                      | Standalone MCP Server, management CLI, and STDIO Bridge                           |
| `SHA256SUMS`                                                 | Checksums for all six release attachments                                         |

Installers are unsigned. Public packages include PostgreSQL, MySQL, and SQL Server drivers; optional local DM8 builds are not included. Client build manifests and `source-maps.zip` are also attached.

## Upgrading

The bundled Windows installer updates older managed local MCP Servers during installation, preserving data, credentials, and upgrade safety snapshots. This version adds an administrator endpoint and does not change the SQLite schema or existing backup format.

For independently maintained MCP Servers, update the server to enable deletion. Other client features continue to work with compatible older servers.

See the [user guide](docs/user-guide.md), [development guide](docs/development.md), and [changelog](CHANGELOG.md).

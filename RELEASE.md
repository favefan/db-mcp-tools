# DB MCP Tools v1.0.15

Client and MCP Server **1.0.15** fix database connection reuse, failure reporting, and operational alerts.

## Highlights

- Database discovery for saved instances reuses the initial database's connection pool instead of creating a new login for each request. Current database visibility is still checked on every call, and unsaved connection edits remain isolated.
- Connection and metadata failures appear as failed requests in access logs, with safe reasons preserved in request details. Permission denials still appear as denied.
- Native database failure dialogs identify the instance and request and explain whether the request reached approval. Dialogs also work in lightweight mode, independently of optional approval notifications. Repeated failures for one instance are coalesced for 60 seconds and while its dialog is open.
- If connectivity fails during an approved request's execution recheck, the request becomes failed without executing SQL. Resuming it cannot silently execute the old write after connectivity returns; submit a new request for normal approval.

## Downloads

| File                                                         | Purpose                                               |
| ------------------------------------------------------------ | ----------------------------------------------------- |
| `db-mcp-tools-client-bundled-1.0.15-windows-amd64.setup.exe` | Desktop client with MCP Server 1.0.15                 |
| `db-mcp-tools-client-none-1.0.15-windows-amd64.setup.exe`    | Desktop client for a separately maintained MCP Server |
| `dbmcp-server-1.0.15-windows-amd64.zip`                      | MCP Server, CLI, and STDIO Bridge                     |
| `SHA256SUMS`                                                 | Release attachment checksums                          |

Client build manifests and `source-maps.zip` are also attached. Installers are unsigned. Public packages include PostgreSQL, MySQL, and SQL Server drivers; optional local DM8 builds are excluded.

## Upgrading

Update both client and MCP Server to 1.0.15 for database failure dialogs. Older compatible servers remain connectable; the minimum API compatibility version remains 1.0.2. There is no SQLite schema or backup format change. Existing historical events retain their original decisions.

Write approval is created only after database scope, schema, and SQL authorization checks succeed. A connection failure before these checks completes the request as failed without creating a pending approval.

The bundled Windows installer updates managed local MCP Servers while preserving data, credentials, and upgrade snapshots. Independently maintained servers require a separate server update.

See the [user guide](docs/user-guide.md), [development guide](docs/development.md), and [changelog](CHANGELOG.md).

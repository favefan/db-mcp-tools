# DB MCP Tools v1.0.16

MCP Server **1.0.16** adds dedicated tools for inspecting tables, views, primary keys, and indexes.

## Highlights

- `list_views` lists authorized ordinary views and can filter by schema.
- `describe_table` keeps its existing column fields and adds the object kind, column nullability, primary-key column order, and supported indexes.
- Metadata follows the configured database scope, column permissions, and Mask deny rules. A primary key or index is omitted if any of its columns is hidden. The server reads metadata without querying business rows.

## Downloads

| File                                                         | Purpose                                               |
| ------------------------------------------------------------ | ----------------------------------------------------- |
| `db-mcp-tools-client-bundled-1.0.16-windows-amd64.setup.exe` | Desktop client with MCP Server 1.0.16                 |
| `db-mcp-tools-client-none-1.0.16-windows-amd64.setup.exe`    | Desktop client for a separately maintained MCP Server |
| `dbmcp-server-1.0.16-windows-amd64.zip`                      | MCP Server, CLI, and STDIO Bridge                     |
| `SHA256SUMS`                                                 | Release attachment checksums                          |

Client build manifests and `source-maps.zip` are also attached. Installers are unsigned. Public packages include PostgreSQL, MySQL, and SQL Server drivers. DM8 is available through an optional local build.

## Upgrading

Update the MCP Server to 1.0.16 and reconnect the agent to refresh the MCP tool list. The bundled Windows installer updates managed local MCP Servers. Independently maintained servers use the standalone server package. The minimum client API compatibility remains 1.0.2.

See the [user guide](docs/user-guide.md), [supported metadata scope](docs/dependencies.md#sql-and-database-boundaries), and [changelog](CHANGELOG.md).

# DB MCP Tools v1.0.14

Client and MCP Server **1.0.14** improve navigation, access logs, and approval explanations.

## Highlights

- Align sidebar navigation, settings, profile switching, branding, and collapse controls. Profile switching now uses a rounded item below a separate divider.
- Add an animated Auto / Light / Dark theme pill; the collapsed sidebar cycles through the same three modes with distinct icons.
- Add Settings → About and a tray About entry, including reopening the About page from lightweight mode.
- Access logs show one current outcome per database request, retain the full event timeline in details, and support filtering administrator operations. Completed requests use actual execution duration; waiting requests show no completed duration.
- Ask Agents to supply an optional explanation of purpose and action in at most 100 characters. Store it encrypted with request history and show it in approval cards, details, access log details, and desktop notifications, including lightweight mode. Missing explanations never block requests or change retry identity.
- Queue desktop lifecycle changes briefly behind background reads, allow status checks alongside other reads, and name active operations when a conflict remains.

## Downloads

| File                                                         | Purpose                                               |
| ------------------------------------------------------------ | ----------------------------------------------------- |
| `db-mcp-tools-client-bundled-1.0.14-windows-amd64.setup.exe` | Desktop client with MCP Server 1.0.14                 |
| `db-mcp-tools-client-none-1.0.14-windows-amd64.setup.exe`    | Desktop client for a separately maintained MCP Server |
| `dbmcp-server-1.0.14-windows-amd64.zip`                      | MCP Server, CLI, and STDIO Bridge                     |
| `SHA256SUMS`                                                 | Release attachment checksums                          |

Client build manifests and `source-maps.zip` are also attached. Installers are unsigned. Public packages include PostgreSQL, MySQL, and SQL Server drivers; optional local DM8 builds are excluded.

## Upgrading

Upgrade the MCP Server to 1.0.14 for request summaries, current outcome filtering, administrator filtering, and Agent explanation storage. Older compatible servers remain connectable; the minimum API compatibility version remains 1.0.2. There is no SQLite schema or backup format change.

The bundled Windows installer updates managed local MCP Servers while preserving data, credentials, and upgrade snapshots. Independently maintained servers require a separate server update.

See the [user guide](docs/user-guide.md), [development guide](docs/development.md), and [changelog](CHANGELOG.md).

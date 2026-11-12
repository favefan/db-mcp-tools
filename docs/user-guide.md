# DB MCP Tools user guide

DB MCP Tools keeps database connections, agent identity, permissions, approvals, Mask, and audit in an independent Go MCP Server. The desktop manages one active configuration per session. It controls locally managed MCP Servers; existing MCP Servers connected over HTTP run independently.

## Choosing a package

- **Client only**: connect to an existing MCP Server or select an installed `dbmcp`. Use HTTP agent integration if no local executable is available.
- **Bundled MCP Server**: includes the Go executable for the platform. Deploy, start, and connect from settings.
- **Standalone MCP Server**: one executable provides the MCP Server, management CLI, STDIO Bridge, and CLI service management.

Package manifests record versions, API compatibility, platform, drivers, file hashes, and third-party licenses. Installers are unsigned. Default builds include PostgreSQL, MySQL, and SQL Server. Explicitly marked `dm-local` builds also include the DM8 native driver; its redistribution license remains unconfirmed, so these are not public distribution packages.

Since Windows 1.0.4, the default application directory is `%LOCALAPPDATA%\db-mcp-tools`. Upgrades from the legacy `DB MCP Tools` directory migrate to `db-mcp-tools` under the same parent, preserving profiles, credentials, MCP Server data, and startup settings while correcting shortcuts. Custom paths remain unchanged; an empty directory can also be selected manually. The installer refuses to overwrite unrelated target files. Unknown files in the old directory stay there. Retry failed migrations and inspect `installer-migration.log` in a diagnostic bundle.

## First run

1. Open the configuration menu at the bottom of the sidebar, then configuration settings. Edit `default` or create a configuration and choose local management or an existing MCP Server. For local management, select `dbmcp`. The bundled package discovers it automatically. Leave the data directory blank for a new isolated directory; never choose the project root or another application's data directory.
2. Saving a local configuration starts and connects to its MCP Server. Switching away or exiting stops it. Executables use `runtime/versions/` in the data directory with a stable launcher. New configurations receive available loopback ports; existing `deployment.toml` addresses are preserved.
3. Add a database instance, enter credentials, and test the connection. The default database is only for connecting and discovery: PostgreSQL defaults to `postgres`, SQL Server to `master`, MySQL can be blank, and DM8 identifies the endpoint's actual database. In Scope, select remote objects on the left and use → to grant them on the right; use ← to remove them. Select databases, schemas, tables, or columns. New instances initially grant no objects. Configure operations and approvals, then save. Instance policy is the upper bound; bindings can only tighten it or add Mask.
4. Install agent configuration under Settings → Agent integration. The agent's first connection creates pending entity registration. The entity card's authorization action opens Approvals → Entity registration, where you select instances and permissions. Reconnect the agent after approval. Manual entity creation, credential issuance, and binding remain available.

Standalone example:

```sh
dbmcp --data-dir <dedicated-directory> init
dbmcp --data-dir <dedicated-directory> runtime upgrade --start
dbmcp --data-dir <dedicated-directory> run status
```

Data and installed application files are separate. Default data locations are `%LOCALAPPDATA%\DBMCP\server` on Windows, `~/Library/Application Support/DBMCP/server` on macOS, and `$XDG_DATA_HOME/dbmcp/server` or `~/.local/share/dbmcp/server` on Linux. Deployment settings are in `deployment.toml`; stop the MCP Server before editing listeners.

For an existing MCP Server, enter its loopback Admin URL and administrator credential in the configuration editor. Use an SSH tunnel for remote administration. This mode only connects and never controls the process. Configurations & MCP Server shows the current target rather than parallel connection tabs.

## Configurations and sessions

A profile stores its connection target, credentials, and client preferences. First launch creates a disconnected `default` profile. The sidebar menu switches profiles; its settings action opens Configurations & MCP Server for full management. The active profile uses the accent color. Disconnect before editing a connected profile. Deleting a profile does not delete MCP Server data. Switching closes unsaved forms and clears caches and subscriptions; active operations must finish first.

Theme, notifications, close behavior, and automatic lightweight mode are per profile. Login startup, installation location, and application version are shared. Business data and permissions live in the MCP Server, so profiles targeting the same MCP Server share its business state. Administrator credentials are stored separately by profile in the OS keyring.

Local management requires MCP Server 1.0.11 or later. Activating the profile starts or attaches to an identity-matched background process; switching away, disconnecting, or exiting stops it. Manual start, stop, and restart remain available in settings. The desktop does not install, migrate, or uninstall system services. Connect to services or terminal-controlled foreground processes using an existing MCP Server HTTP configuration. HTTP connections, including `127.0.0.1`, never control processes. Tray and lightweight mode keep the local MCP Server running. Forced termination cannot guarantee graceful shutdown.

Legacy connections and preferences are preserved. Old lifecycle toggles in `profiles.json` are ignored: local management owns startup and shutdown, while HTTP remains independent. Legacy `profile.json` can still migrate into `default`.

## Connecting Codex / Claude Code

In Settings → Agent integration, choose installation, user / project scope, HTTP / STDIO, and an entry name, then preview. No entity or instance selection is required beforehand.

- Codex user configuration: `$CODEX_HOME/config.toml`, or `~/.codex/config.toml` when unset. Project configuration: `.codex/config.toml`, loaded only for trusted projects.
- Claude Code user configuration: `~/.claude.json`. Project configuration: `.mcp.json`, requiring Claude Code's own initial confirmation.
- HTTP uses authentication fields supported by each client. STDIO uses a stable `dbmcp` path and restricted credential file. Client-only installations can select a separately installed executable.

Installation issues a dedicated access credential, backs up the original file, and saves atomically. On first MCP access with that credential, the MCP Server atomically creates a pending entity and returns `entity_pending`. Database discovery and data tools remain unavailable until approval. In entity registration, select instances, resources, operations, and limits. Approval commits all bindings and entity activation in one transaction; failure leaves no partial grants. Reconnect with the original credential afterward.

Identity follows the independent random credential, not mutable or spoofable names and IPs. Retries, concurrent requests, and reconnections with the same credential create only one entity. Copied configurations share identity; install separately to distinguish agents. Unknown requests cannot register anonymously. Denial revokes the registration credential, so reconnecting cannot register again.

Installation failure revokes the new credential. Unrelated entries with the same name are not overwritten. Changed files require another preview. Unrelated JSON values, TOML sections and comments, and original line endings are preserved. Do not commit project files containing credentials.

Restart the agent or reconnect MCP after installation. Tools include `list_instances`, `list_databases`, `list_schemas`, `list_tables`, `describe_table`, `db_query`, `db_execute`, `db_explain`, `approval_status`, and `resume_request`.

Removal deletes only this tool's unchanged entry and revokes its installation credential. Modified entries remain, but their old credential is still revoked. Rotating other credentials requires updating the corresponding agent configuration. Tokens are shown only when issued.

For other clients, issue or rotate a credential in Entities, enter the MCP URL, and export generic MCP configuration. The JSON uses `mcpServers` and HTTP `headers` for compatible clients; follow the target client's import instructions. Exports contain the current entity credential, use private permissions, and back up existing files before overwrite. Permissions, rotation, and revocation remain managed by the MCP Server.

## Permissions, approvals, and Mask

Resources and operations are the intersection of instance and binding policy. Both source CIDRs must match; limits use the smaller value. Authorized reads are allowed and writes require approval by default. DDL, procedures, dynamic SQL, multiple statements, and unknown provenance are denied. Approval cannot override hard denials.

An instance may grant one, selected, or all databases. PostgreSQL and SQL Server use database → schema → table → column. MySQL combines database and schema, displaying database → table → column. A DM8 endpoint exposes one actual database, with user / schema → table → column beneath it. Empty schemas can be authorized, including future objects. Discovery is limited by the database account's visibility.

Agents call `list_databases`, then pass an exact `database` to catalog, query, execution, or Explain tools. Requests target one database; USE and cross-database SQL are unsupported. Multi-database requests without a target are rejected. A single authorized database can still be inferred, and DM8 can infer its endpoint's sole database. Scope discovery works before saving, but grants take effect only after save. Changing the default connection database does not change resource grants. Approvals and remembered rules apply only to their recorded database. The object selector requires MCP Server 1.0.2 or later.

Selecting a parent includes its current and future descendants and merges duplicate children. Remove the parent before granting a narrower subset. Use Ctrl / Command or Shift for multiselect, arrows for navigation, and Space to select. Expanding a database loads its catalog; refresh objects after connection or catalog changes. For large catalogs, select a database or schema; in-tree search and virtualization are not provided.

Resource keys are `db/<database>/*` for a database, `db/<database>/<schema>.*` for a schema, `db/<database>/<schema>.<table>` for a table, and `db/<database>/<schema>.<table>.<column>` for a column. Reserved characters such as dots, percent signs, wildcards, and slashes in names are encoded. Legacy `schema.table[.column]` rules retain their meaning across configured databases. Editing old single / selected-database instances converts them to explicit database-qualified keys. Binding `*` means all objects within the instance's scope only.

When a write returns `pending`, inspect its SQL, parameters, source, and scope in Approvals. Allow once, deny, or remember an explicit table and operation scope. An `always` policy must be explicitly changed before remembering a rule. The agent resumes with the original `request_id` using `approval_status` / `resume_request`; it cannot substitute SQL. Concurrent retries claim execution once. Writes with unknown commit results are not replayed automatically; inspect the target database first.

Instance Mask rules run before additional entity rules. Denied columns cannot be accessed in predicates or expressions. Derived expressions involving sensitive sources receive whole-value replacement. There is no separate built-in content detector; use Mask regular expressions as needed. Rule previews and the entity simulator use actual MCP Server logic; simulation does not execute SQL.

Resource scope limits data: `public.orders` grants a table, `public.users.email` grants a column, and `*` grants all data within the current instance. SQL operations require separate permission. New instances, bindings, and templates have no preset Mask rules. Add rules manually or explicitly copy a template. Previews require schema, table, column, and sample value, and apply only matching source rules.

For content replacement, select a field or pattern, choose regular expression replacement, and enter the expression and replacement. Legacy content detection toggles are no longer used. Explicit Mask rules are retained and can be removed in the Mask editor.

See [Dependencies and limitations](dependencies.md) for SQL scope. PostgreSQL Explain returns plans and estimates without execution; other dialects do not expose it. The native DM driver cannot enforce and verify client TLS; only explicitly TLS-disabled local or protected-tunnel connections are accepted.

## Runtime and system services

Bundled clients show the packaged executable version and offer explicit upgrades. The coordinator stops the managed process, creates a safety snapshot, deploys the new version, and restores running state. Ordinary startup and configuration switching do not upgrade or back up.

Remote MCP Servers and separately selected executables are not replaced automatically. Disconnect and select a newer executable in the configuration editor, then use the MCP Server upgrade action when an update is available. Business requests require MCP Server 1.0.2 or later; local management requires 1.0.11. Settings show running and packaged versions. On upgrade failure, inspect diagnostics and retained snapshots rather than repeatedly uninstalling or deleting data.

Local management follows the client lifecycle; existing MCP Servers only connect. Client login startup, closing to tray, and system service startup are separate settings.

The desktop manages local background processes and does not offer Windows service setup. The standalone Go CLI retains platform service management for independently maintained deployments. Connect to these over HTTP in the desktop.

```sh
dbmcp --data-dir <directory> service install
dbmcp --data-dir <directory> service start
dbmcp --data-dir <directory> service status
dbmcp --data-dir <directory> service stop
dbmcp --data-dir <directory> service uninstall
```

Windows service commands without a data directory use the system location. Uninstalling a service retains data and stable executables. Uninstalling the desktop does not stop external MCP Servers; retained application data may still include installed Bridge credential files.

## Upgrades and recovery

Choose a newer executable and use the upgrade action, shown only when an update is available, or run the new build:

```sh
new-dbmcp --data-dir <directory> runtime upgrade
```

Upgrade stops the managed process, backs up, deploys, updates existing service registration, and restores the original mode. The stable launcher follows `current.json`; old executables and `previous.json` remain. Use `runtime migrate --to <empty-directory> --start` to move to an empty directory, optionally with `--service` for a service. The source stays stopped.

Full backups contain a consistent SQLite snapshot, matching keys, administrator credentials, and a hash manifest. There are no scheduled, startup, or profile-switch backups. Manual backups and safety snapshots before upgrades, restores, and schema migrations remain. Configuration exports omit secrets; imports create disabled new objects without replacing existing state.

```sh
dbmcp --data-dir <directory> backup
dbmcp --data-dir <directory> restore <backup-directory>
```

Restore requires shutdown, verifies the manifest and SQLite integrity, and snapshots current state first. After upgrade failure, inspect service / startup logs and explicitly restore from the snapshot identified by the error. State potentially used by MCP clients is not rolled back automatically. To roll back only the executable, stop and run `runtime rollback-program`; reinstall existing services to update their executable paths. Never open a newer schema directly with an older executable.

## Remote deployment and troubleshooting

Click an access log row or its detail action to open the right-hand drawer with overview, full SQL, bound parameters, execution summary, matched Mask rules, and related events. Navigate adjacent requests, copy IDs, refresh, or export the request without losing filters, pagination, or list position. Only drawer content scrolls; its title and actions stay fixed. Escape closes it.

Since 1.0.8, database request details are encrypted in the state store and retained for 30 days. They cannot resume execution; original execution payloads follow their own cleanup. Request exports include retained SQL, parameters, and related events. Results retain row counts, affected counts, columns, truncation, and error summaries, not raw rows. Older missing content is shown as unrecorded or cleaned up and cannot be reconstructed. Management operations display their own audit records.

New policies default to 5000 rows and 10485760 bytes; new connections default to TLS off. Existing explicit instance and template settings are preserved. Stored passwords display a fixed six-dot indicator: leaving the field blank retains the password, while entering a new value replaces it. Pages and dialogs do not initially emphasize an input or button; Tab navigation remains available.

Admin listens on loopback only; use SSH tunnels remotely. Non-loopback MCP requires TLS or an explicitly configured reverse proxy. Client Admin credentials remain in the OS keyring. Agent credentials cannot call the management API.

For reverse proxies, set `reverse_proxy = true` and `trusted_proxies = ["127.0.0.1/32", "::1/128"]` in `deployment.toml`, listing only actual proxy IPs / CIDRs. Proxies should replace or append `X-Forwarded-For` and restrict direct backend access. Only connections from trusted proxies use the chain, stripping trusted hops from right to left. Missing, invalid, or entirely trusted chains are rejected. Without trusted proxies, forwarding headers are ignored. Admin always checks the real connection.

For connection failures, check versions, drivers, addresses, running mode, and data directory first. Port conflicts never cause arbitrary processes to be killed. Copied credentials in another directory cannot stop the original MCP Server. Export diagnostics from General settings, access logs, or the tray; local evidence remains available while disconnected, and unavailable remote logs are reported. On the host, run `dbmcp --data-dir <directory> diagnostics --out <new-file>.json`. See [Diagnostics](diagnostics.md) for paths and retention.

The first window close asks whether to minimize to tray or exit, with an option to remember the choice; change it later in General settings. Automatic lightweight mode releases the hidden main window and WebView after three minutes in the tray. Reopening reloads the same profile and discards unsaved forms. The tray provides window reopening, immediate lightweight mode, diagnostic export, and exit; launching from the Start menu also restores the window. Independent HTTP-connected MCP Servers remain running when the client closes or exits.

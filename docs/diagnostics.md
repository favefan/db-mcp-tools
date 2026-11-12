# Logs and diagnostics

Export a diagnostic bundle from Settings → General → Diagnostic logs, the access log page, or the tray menu. The resulting `dbmcp-diagnostics-<timestamp>.json` can be reviewed and shared without copying the entire data directory. Reproduce the problem first when possible and note the time and action.

The client collects a snapshot through its current Admin connection, including remote deployments reached through an existing SSH tunnel. If the MCP Server is unreachable, credentials are unavailable, or the version lacks support, local logs are still saved and missing stages are listed in `server_status` and `issues`. On the MCP Server host, run:

```sh
dbmcp --data-dir <data-directory> diagnostics --out <new-file>.json
```

The CLI can collect existing logs while the MCP Server is stopped without opening or migrating business SQLite. It creates a new file only. Desktop exports cannot overwrite application configuration or the connected local MCP Server's data directory. Administrator tokens are not needed in command arguments or shared reports.

## Locations

| Component    | Windows                                           | macOS                                         | Linux                                                                              |
| ------------ | ------------------------------------------------- | --------------------------------------------- | ---------------------------------------------------------------------------------- |
| Client       | `%LOCALAPPDATA%\local.dbmcp.tools\logs`           | `~/Library/Logs/local.dbmcp.tools`            | `$XDG_DATA_HOME/local.dbmcp.tools/logs` or `~/.local/share/local.dbmcp.tools/logs` |
| CLI / Bridge | `%LOCALAPPDATA%\DBMCP\logs`                       | `~/Library/Logs/DBMCP`                        | `$XDG_STATE_HOME/dbmcp/logs` or `~/.local/state/dbmcp/logs`                        |
| MCP Server   | `<data-directory>\logs`                           | `<data-directory>/logs`                       | `<data-directory>/logs`                                                            |
| Access audit | `audit_events` in `<data-directory>\state.sqlite` | Same table in `<data-directory>/state.sqlite` | Same table in `<data-directory>/state.sqlite`                                      |

The default Windows personal data directory is `%LOCALAPPDATA%\DBMCP\server`. Service commands without an explicit directory use `%PROGRAMDATA%\DBMCP\server`. Service and desktop accounts may differ; the Admin API collects the MCP Server process's own logs.

Runtime logs use JSON Lines. Go files are `server-<PID>.jsonl`, `cli-<PID>.jsonl`, and `bridge-<PID>.jsonl`; desktop files are `desktop-<PID>.log`. Each process has a 2 MiB file limit and at most three rotated files. Startup removes inactive files older than seven days or beyond the newest 32 inactive files. Active processes hold locks to prevent cleanup by other instances. Access audit retention remains 30 days. Legacy `server.log` is only a background startup stderr capture and is excluded from new bundles.

The Windows installation directory migration introduced in 1.0.4 writes `installer-migration.log` in the client log directory. UTF-8 JSON Lines record UTC time, version, source / target paths, rejection reasons, old uninstaller exit codes, and completion stages. A write after the file exceeds 1 MiB restarts it. Diagnostic `desktop_logs` include this file to distinguish conflicts, missing uninstallers, cleanup failures, and successful migrations.

## Contents and investigation

- Version, source revision, OS / architecture, timestamps, PID, WebView version, and actual data / log paths.
- Main window hiding, reconstruction, and lightweight mode scheduling / cancellation / release; current lightweight state, tray check state, and remaining timer distinguish preferences from immediate actions.
- MCP Server configuration, state opening, listeners, TLS, readiness, maintenance, and shutdown; CLI / service operations and process results.
- Client management requests, Bridge calls, HTTP status, and elapsed time. `trace_id` links client and MCP Server; database operations add `request_id`, entity / instance / credential IDs, policy revision, and request digest.
- Database error stages, type chains, network errors / errno, and driver SQLSTATE or error numbers. Raw driver and parser errors can contain row values or SQL literals, so only safe messages, fingerprints, and known reason codes are retained.
- Up to 1000 recent audit summaries, entity state, credential revocation / expiry, registration status, endpoints, and policy snapshots without replacement values.
- Fingerprints and source locations for unhandled frontend errors, Promise rejections, and React render failures; Rust / Go panic locations and stacks without panic argument values. Client builds retain hidden source maps, stored by asset hash for production diagnosis.

Bundles exclude passwords, tokens, Authorization headers, keys, credential files, SQLite copies, raw SQL / parameters, result rows, and Mask replacement values. Collection reads only allowed regular log files and does not follow symbolic links. Each directory contributes at most 16 recent files and 8 MiB, with a 64 KiB line limit. Truncation, corrupt lines, and read failures are reported. Configuration snapshots include at most 100 instances, entities, and registrations each.

The separate **Export request** action includes the SQL, parameters, and related events deliberately viewed by an administrator. Request details are encrypted in MCP Server state and retained for 30 days, accessible only through administrator endpoints. They are outside diagnostic collection. Execution results retain summaries, not raw rows.

Start with version, time, and collection status, then follow `trace_id` and error stages / codes to distinguish configuration, authentication, policy, connection, execution, and persistence problems. `request_id` links approvals, resumption, and audit. Missing, truncated, or rotated evidence requires reproduction and export on the actual host. Diagnostics only save a file; they do not upload it or contact external services.

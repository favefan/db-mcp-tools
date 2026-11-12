# Product scope and defaults

## 1. Purpose

DB MCP Tools manages AI agents' database access over MCP for individual developers. The flow is: run an MCP Server → add database instances → issue separate agent credentials and install MCP configuration → set scope, permissions, and Mask → approve operations when required → inspect results and audit records.

The MCP Server runs independently; the desktop is optional. The desktop can manage a local background process or connect to an existing MCP Server. Local management provides start, stop, and restart. System services remain a separate Go CLI deployment option.

Windows is the primary development platform. The Go MCP Server supports Windows, macOS, and Linux; desktop builds use each OS's native toolchain. GitHub workflows cover Windows x64 only. Remote management uses existing SSH tunnels; there is no built-in SSH manager.

## 2. Objects and pages

| Page      | Contents                                                                                                                                          |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Instances | Logical database connections, allowed resources, SQL permissions, approval mode, Mask, limits, associated entities, and records                   |
| Entities  | Stable entity IDs, display names, credentials, enabled state, bindings, effective permissions, recent sources, and requests                       |
| Approvals | Separate SQL operation approvals and entity registration, one-time allowance, denial, and explicitly scoped persistent rules                      |
| Logs      | Requests, denials, approvals, errors, and administrator changes, filtered by entity, instance, time, and outcome                                  |
| Settings  | Configurations and MCP Server connections, local lifecycle, instance templates, agent integration, backups and recovery, theme, and notifications |

Policies, networking, Mask, and drivers do not have separate top-level navigation. Instance details have seven tabs: Connection, Scope, Permissions, Mask, Entities, Records, and Advanced.

Entity names, IPs, and client declarations are display information; credentials establish identity. An entity can have several rotatable credentials and bindings to multiple instances with separate authorization. Agents sharing a credential share an identity.

## 3. Defaults

| Setting                 | Default                                                                                                                                        |
| ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| MCP / Admin listeners   | Separate listeners, both bound to `127.0.0.1`; administrator and entity credentials are separate                                               |
| Unauthenticated clients | Data tools and anonymous database discovery are denied; access requires administrator registration                                             |
| New entities            | Configuration installation prepares an access credential; first connection creates pending registration, requiring administrator authorization |
| Instance templates      | A snapshot is copied; edits affect only future instances                                                                                       |
| Sources                 | Local only; remote access requires explicit listener, CIDR, and TLS configuration                                                              |
| SQL                     | SELECT, INSERT, UPDATE, DELETE; DDL denied by default; procedure calls and arbitrary scripts are outside the initial scope                     |
| Approval                | Allow authorized reads, ask for writes; always-ask and no-per-request-approval modes are also available                                        |
| Execution limits        | 15 seconds, 5000 rows, 10 MiB of encoded results, 4 concurrent executions per instance; approval waits hold no database connection             |
| Approval lifetime       | 5 minutes, configurable in advanced settings; expired requests are denied, not executed                                                        |
| Mask                    | No preset rules; only user-entered or explicitly copied template rules apply, with preview support                                             |
| Audit                   | Enabled, retained for 30 days; summaries exclude parameter values, raw result values, authentication headers, and passwords                    |
| Client exit             | Stop the active locally managed MCP Server; leave existing HTTP-connected MCP Servers running                                                  |
| Configuration           | Initial `default` profile; one active profile and one connection mode at a time                                                                |
| System services         | No automatic registration or elevation; optional CLI management is separate from desktop local management                                      |

No-per-request-approval mode removes human confirmation only. It does not change permissions or mandatory Mask. Approval cannot override forbidden databases, tables, columns, or operation classes.

## 4. Capabilities

| Capability                | Scope                                                                                                                                    |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| MCP Server / CLI / Bridge | Headless lifecycle, queries, and approvals; Streamable HTTP and STDIO Bridge share one MCP Server                                        |
| Databases                 | PostgreSQL, MySQL, SQL Server, and optional DM8 connections, catalogs, common reads / writes, timeouts, and limits                       |
| PostgreSQL extensions     | Basic PostGIS / TimescaleDB recognition and metadata, with readable spatial values                                                       |
| Identity and permissions  | Separate credentials, rotation / revocation, entity disabling, IP/CIDR, resource scope, SQL classes, and additional entity restrictions  |
| Approvals                 | One-time approval, denial, expiry, and narrowly scoped remembered rules without passing authorization strings through a model            |
| Mask                      | Fixed replacement, partial retention, NULL, regular expressions, denied access, inheritance, source matching, and previews               |
| Assistance                | Connection tests, schema selection, effective permissions, analysis-only rule simulation, and concise risk hints                         |
| Logs and recovery         | Query / denial / error / approval auditing, configuration export / import, consistent SQLite backup and restore                          |
| Desktop                   | Five navigation entries, light / dark themes, tray and approval notifications, and local MCP Server lifecycle                            |
| Agent integration         | Claude Code and Codex configuration generation, targeted installation / removal, backups, and previews; generic export for other clients |
| Distribution              | Standalone MCP Server, client-only, and bundled MCP Server packages with explicit driver and platform capabilities                       |

The simulator shares actual policy and SQL analysis code without executing SQL. Mask preview normally uses user-supplied sample values; reading real samples requires an explicit administrator action, and raw values do not enter ordinary logs.

## 5. Mask and database security

Output Mask reduces exposed information but cannot eliminate inference from arbitrary predicates, aggregates, or timing. Protect confidential fields with denied access and database column permissions / security views. Use database views or native RLS for row restrictions.

The shared pipeline parses and checks resources and field references in common SQL. Unsupported syntax or uncertain provenance produces an explicit error and guidance to simplify the query or use authorized views. Arbitrary SQL compatibility does not justify allowing unknown syntax or building a general SQL rewriting and lineage engine.

## 6. Outside the current scope

Enterprise multitenancy, complex RBAC / IAM, clusters and high availability, a custom OAuth authorization server, arbitrary cross-dialect SQL rewriting, generic row-filter expressions, automatic change snapshots and rollback, index recommendations, large performance dashboards, arbitrary procedures or scripts, long-lived cross-request transactions, a plugin marketplace, and automatic driver downloads / hot switching.

MCP Tasks may be an enhancement when both the pinned SDK and target clients support it; approval resumption does not depend on it. Target databases are tested in temporary containers with a runtime available on the development platform. Missing usable images permit a documented skip; any available image build method may be used for dedicated test images, which are removed after use. Results describe only scenarios actually run.

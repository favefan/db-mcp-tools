# Contributing

Read the [development guide](docs/development.md) and choose frontend preview, MCP Server, or full desktop setup. A coding agent is not required; `AGENTS.md` describes the same engineering boundaries for automated collaboration.

## Issues and changes

- Include the version, OS / architecture, reproduction steps, expected and actual behavior, and reviewed minimal logs. Follow the [security policy](SECURITY.md) for vulnerabilities.
- Create a topic branch from the latest `main` and use your own Git identity. Preserve other contributors' work, global Git configuration, and unrelated history.
- Read the relevant architecture and call chain before editing. Identity, permissions, approvals, Mask, and SQLite logic belong to the MCP Server; Rust handles desktop and local integration.
- Install with the lockfile. Do not mix npm / yarn, upgrade unrelated dependencies, or reformat unrelated files. Use commit prefixes such as `feat:`, `fix:`, `docs:`, and `chore:`, grouping commits by complete changes.

## Before submitting

Run the checks relevant to your changes from the development guide. Ordinary UI work does not require all database or installer tests. Cover the affected boundaries when changing permissions, SQL, approvals, Mask, or configuration preservation. All target database tests use temporary containers created for that run, never existing business databases.

Describe the problem, final behavior, validation commands and actual results in the PR, including skipped or unverified checks. Explain licensing and platform effects for dependency changes. Run `pnpm generate:api` and commit the generated files when API models change.

GitHub checks run on Windows only. Report the OS version, architecture, and commands for local macOS / Linux results; a Windows result does not establish support elsewhere. Delete your topic branch after merging into `main`.

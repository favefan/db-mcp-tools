# Security and sensitive information

Do not publish real connection passwords, Admin / MCP tokens, keys, credential files, database backups, request details, or unreviewed diagnostic bundles in issues, PRs, or attachments. Request details may contain raw SQL and exact parameters. Review diagnostic exports before sharing, even though they apply exclusion rules.

Use the private **Report a vulnerability** entry on the repository's Security page. If unavailable, ask the maintainer for a private contact channel without exposing exploit details or sensitive data. Do not publish exploit payloads.

Include affected versions, a minimal reproduction, impact, and sanitized evidence. Do not test against databases or services you do not own.

If real credentials are exposed, revoke or rotate them first, then coordinate cleanup. Deleting a current file does not remove secrets from Git history. Do not rewrite shared history without coordination.

Fixed passwords in test scripts are only for temporary database containers bound to loopback and cleaned up after the run. Default distributions omit the DM driver because its redistribution license has not been confirmed.

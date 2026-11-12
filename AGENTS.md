# Collaboration guidelines

## Goals

- Read `docs/README.md` and the relevant design first. Locate problems through the application's call chain, state ownership, and user flow before changing individual implementations.
- Work within the current task's authorization. Historical tasks do not provide default authorization for new work.
- The client lives in `client/`; the Go MCP Server / CLI / Bridge live in `server/`. Business logic and SQLite state belong only to the MCP Server. Rust handles desktop and local integration.
- Build a practical personal tool. Keep logic direct and style simple; do not add frameworks, abstractions, compatibility layers, or configuration for hypothetical scenarios.
- Apart from serious security or performance issues and fixes required by the current feature, avoid unsolicited refinements. Do not substitute local optimization for complete delivery.

## Implementation and dependencies

- Investigate mature libraries before implementing components, protocols, parsers, drivers, service management, or file operations. Prefer reuse when a small adapter is sufficient.
- Document dependency choices that affect architecture and real limitations. Do not present upstream README claims or unrun tests as verified facts.
- Organize code by business capability, using simple functions and small interfaces. Do not copy entire reference projects or create empty modules in advance.
- Do not bypass the unified identity, permission, approval, and Mask entry points. Fix shared business problems in the common pipeline rather than separate tool-specific patches.

## Git management

- Check status before operating. Follow the current task's authorization for branches, commits, and merges. `main` is the integration branch; use short topic branches, with `codex/` as the default agent prefix, and delete them after merging.
- Use the contributor's own Git identity. Do not replace it with the project author's identity or change global Git configuration.
- Do not add remotes or push unless the user requests it. Preserve others' changes and commits; do not perform destructive resets, force pushes, or unrelated history rewrites.
- Documentation, guidelines, a complete capability, or a fix can each form a clear commit. Do not split mechanically by file or tiny step. Use prefixes such as `docs:`, `chore:`, `feat:`, and `fix:`.
- At task completion, report key changes, appropriate validation, commits, and branches. When commits are authorized, leave your work committed.

## Formatting and validation

- Follow `.gitattributes`, `.editorconfig`, Prettier, Taplo, and `rustfmt.toml`. Use UTF-8 without BOM and LF, except CRLF for `.bat` / `.cmd`.
- Use `gofmt` for Go and `cargo fmt` for Rust. Use the repository's locked Prettier and SQL plugin for TS / TSX / JS / CSS / JSON / YAML / Markdown / migration SQL, and Taplo for TOML.
- Choose and configure formatting when adding another language; do not rely on individual editor settings. Commands are in `docs/development.md`.
- Run only the necessary checks for the affected scope during development. Do not run the full matrix, screenshot sweeps, or repetitive tests after every small change.
- Keep focused regressions for real boundaries: identity and permissions, SQL classification, masking bypasses, approvals and duplicate writes, and configuration preservation. Do not write tests that mirror ordinary styling or simple CRUD.
- Integrate by affected user flow. Before release, check the relevant databases and platforms; never report skipped checks as passed.
- Use temporary containers for all target database integration tests and validation. Choose WSLC (the Windows Subsystem for Linux container CLI), Docker, Apple Container, or Podman according to the development platform. Multiple runtimes are not required. Do not substitute host-installed databases or existing business instances.
- If no suitable database image is available, skip that database with a reason and continue other work. Any available image build method may be used for dedicated test images, not only Docker or Dockerfiles. Building an image is optional before skipping.
- Dispose of test containers after use. On success, failure, or interruption, clean up containers, volumes, networks, temporary data, and dedicated test images created for the run. Keep only necessary test records and leave unrelated resources alone. See `docs/development.md`.

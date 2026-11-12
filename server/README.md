# MCP Server

The standalone Go MCP Server, management CLI, and STDIO Bridge share one `dbmcp` executable. Business state, identity, permissions, approvals, Mask, and auditing are implemented here.

Run these commands from `server/`:

```sh
go build -o bin/dbmcp ./cmd/dbmcp
bin/dbmcp --data-dir /path/to/private-data init
bin/dbmcp --data-dir /path/to/private-data serve
# Or run independently in the background:
bin/dbmcp --data-dir /path/to/private-data run start
bin/dbmcp --data-dir /path/to/private-data run status
bin/dbmcp --data-dir /path/to/private-data run stop
```

Use `bin/dbmcp.exe` on Windows. Admin defaults to `127.0.0.1:19876` and MCP to `http://127.0.0.1:19877/mcp`. Edit deployment addresses in the data directory's `deployment.toml`. Admin only listens on loopback; use an SSH tunnel for remote management. Non-loopback MCP requires explicit TLS or reverse proxy configuration.

`admin.token` is a restricted file of raw random bytes. The CLI reads and encodes it automatically; its file text is not a Bearer value. The desktop connects using the local data directory or an encoded credential supplied by the user and stores it in the OS keyring. MCP entity tokens are entirely separate from Admin credentials.

Management examples:

```sh
dbmcp instances list
dbmcp instances save --file instance.json
dbmcp entities save --file entity.json
dbmcp api /admin/v1/entities/ENTITY_ID/credentials --method POST --file credential-request.json
dbmcp api /admin/v1/entities/ENTITY_ID/bindings/INSTANCE_ID --method PUT --file binding.json
dbmcp approvals list
dbmcp approvals approve REQUEST_ID
dbmcp approvals deny REQUEST_ID
dbmcp backup
dbmcp bridge --credentials /path/to/private-bridge.json
```

`api` accepts `--file -` for JSON on stdin. Remote tunnels can use `--admin-url` and `DBMCP_ADMIN_TOKEN`. Bridge credentials use `{"url":"http://127.0.0.1:19877/mcp","token":"dbmcp_..."}` and must be stored privately. The Bridge waits up to 20 seconds for human approval by default, then returns the persisted request ID. Agents can continue with `approval_status` / `resume_request`.

Go models generate the [OpenAPI contract](../docs/openapi.json). Fetch default instance policies from `GET /admin/v1/default-policy` and restriction-only binding defaults with `?kind=binding`. Both start with empty Mask rules and no built-in content detection. Rules are added only by explicit configuration or template selection. New entities have no default bindings. Updates must include the original revision. CLI output is JSON; do not put passwords or tokens in command arguments.

```sh
go test ./...
go vet ./...
```

Ordinary tests do not connect to target databases. Integration tests require temporary containers created for the run and explicit `DBMCP_TEST_CONTAINER=1`; never point them at business databases. From the repository root, `pnpm test:databases --runtime=docker` creates and cleans up containers; Podman / WSLC can also be selected. See [Dependencies and limitations](../docs/dependencies.md) for DM build restrictions.

See [Architecture](../docs/architecture.md) for database adapters, the shared execution pipeline, and SQLite ownership.

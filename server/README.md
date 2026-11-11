# Server

独立 Go Server、管理 CLI 和 STDIO Bridge 共用一个 `dbmcp` 可执行文件。业务状态、身份、权限、审批、Mask 与审计只在这里实现。

```sh
go build -o bin/dbmcp ./cmd/dbmcp
bin/dbmcp --data-dir /path/to/private-data init
bin/dbmcp --data-dir /path/to/private-data serve
# 或独立后台运行
bin/dbmcp --data-dir /path/to/private-data run start
bin/dbmcp --data-dir /path/to/private-data run status
bin/dbmcp --data-dir /path/to/private-data run stop
```

Windows 使用 `bin/dbmcp.exe`。默认 Admin 为 `127.0.0.1:19876`，MCP 为 `http://127.0.0.1:19877/mcp`；部署地址在数据目录 `deployment.toml` 修改。Admin 限制回环监听，远程管理使用 SSH 隧道。非回环 MCP 必须显式配置 TLS 或反向代理。

`admin.token` 是受限的原始随机字节文件，CLI 自动读取并编码，不能直接把文件文本当 Bearer 值。桌面通过本机数据目录或用户输入的编码后凭据连接，并保存到 OS keyring。MCP 实体 Token 与 Admin 完全分离。

管理命令示例：

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

`api` 支持 `--file -` 从 stdin 读取 JSON；远程隧道可使用 `--admin-url` 和 `DBMCP_ADMIN_TOKEN`。Bridge 凭据文件格式为 `{"url":"http://127.0.0.1:19877/mcp","token":"dbmcp_..."}`，必须按私密文件保存。Bridge 默认最多等待人工审批 20 秒，超时仍返回持久化请求 ID，Agent 可通过 `approval_status` / `resume_request` 恢复。

业务 JSON 模型由 Go 生成到 [OpenAPI](../docs/openapi.json)。新实例策略可从 `GET /admin/v1/default-policy` 取得，绑定使用 `?kind=binding` 取得仅追加限制的默认策略；两者的 Mask 均为空，不包含内置内容检测。只有显式配置或复制模板才增加脱敏规则。新实体没有默认绑定。更新需要携带原 revision。CLI 输出 JSON，不在命令参数中传密码或 Token。

```sh
go test ./...
go vet ./...
```

普通测试不连接目标数据库。集成测试需要本轮新建的临时容器和显式 `DBMCP_TEST_CONTAINER=1`，不可指向已有业务库；使用 `pnpm test:databases --runtime=docker` 创建和清理临时容器（也可选择 Podman / WSLC）。DM 构建限制见 [依赖与限制](../docs/dependencies.md)。

数据库接入、统一执行管线与 SQLite 状态归属详见 [架构设计](../docs/architecture.md)。

//! Local Agent configuration adapters. All identities and policies remain in Server.
#[cfg(windows)]
use crate::hide;
use crate::{entry, read_profile, request, Desktop, Profile};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
#[cfg(windows)]
use std::process::Command;
use std::{
    fs,
    io::Write,
    path::{Path, PathBuf},
};
use tauri::{Manager, State};
use tauri_plugin_dialog::DialogExt;
use toml_edit::{DocumentMut, Item, Table};

#[derive(Clone, Serialize, Deserialize)]
pub struct Target {
    client: String,
    scope: String,
    project_dir: String,
    entry: String,
    transport: String,
    url: String,
    executable: String,
}
#[derive(Clone, Serialize, Deserialize)]
pub struct Record {
    id: String,
    client: String,
    path: PathBuf,
    entry: String,
    transport: String,
    server_id: String,
    entity_id: String,
    #[serde(default)]
    registration_id: String,
    credential_id: String,
    entry_hash: String,
    secret_file: Option<PathBuf>,
    backup: Option<PathBuf>,
}
#[derive(Serialize)]
pub struct Preview {
    path: PathBuf,
    fingerprint: String,
    state: String,
    entry_preview: Value,
}
fn hash(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}
fn entry_hash(value: &Value) -> String {
    fn sorted(v: &Value) -> Value {
        match v {
            Value::Object(m) => {
                let mut keys: Vec<_> = m.keys().collect();
                keys.sort();
                Value::Object(
                    keys.into_iter()
                        .map(|k| (k.clone(), sorted(&m[k])))
                        .collect(),
                )
            }
            Value::Array(a) => Value::Array(a.iter().map(sorted).collect()),
            _ => v.clone(),
        }
    }
    hash(&serde_json::to_vec(&sorted(value)).expect("JSON value"))
}
fn read(path: &Path) -> Result<Option<Vec<u8>>, String> {
    match fs::symlink_metadata(path) {
        Ok(meta) => {
            if !meta.is_file() || meta.file_type().is_symlink() || meta.len() > 8 << 20 {
                return Err("配置必须是小于 8 MiB 的普通文件；请直接选择符号链接的目标".into());
            }
            fs::read(path).map(Some).map_err(|e| e.to_string())
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(e) => Err(e.to_string()),
    }
}
fn fingerprint(bytes: &Option<Vec<u8>>) -> String {
    bytes
        .as_ref()
        .map(|b| hash(b))
        .unwrap_or_else(|| "absent".into())
}
fn text(bytes: &[u8]) -> Result<&str, String> {
    std::str::from_utf8(bytes).map_err(|_| "配置不是 UTF-8 文件".into())
}
fn parsed(client: &str, source: &str) -> Result<Value, String> {
    let value = if client == "codex" {
        toml_edit::de::from_str(source).map_err(|_| "Codex TOML 配置无效")?
    } else {
        serde_json::from_str(source).map_err(|_| "Claude Code JSON 配置无效")?
    };
    Ok(value)
}
fn current(client: &str, bytes: &Option<Vec<u8>>, name: &str) -> Result<Option<Value>, String> {
    let Some(bytes) = bytes else {
        return Ok(None);
    };
    let root = parsed(client, text(bytes)?)?;
    let key = if client == "codex" {
        "mcp_servers"
    } else {
        "mcpServers"
    };
    if !root.is_object() || root.get(key).is_some_and(|v| !v.is_object()) {
        return Err("MCP 配置根节点必须是对象".into());
    }
    Ok(root.get(key).and_then(|v| v.get(name)).cloned())
}
fn config_entry(t: &Target, token: &str, secret: &Path) -> Result<Value, String> {
    if t.transport == "http" {
        return Ok(http_entry(&t.client, &t.url, token));
    }
    let args = vec![
        "bridge",
        "--credentials",
        secret.to_str().ok_or("凭据文件路径编码无效")?,
    ];
    Ok(if t.client == "codex" {
        json!({"command":t.executable,"args":args,"startup_timeout_sec":20,"tool_timeout_sec":60})
    } else {
        json!({"type":"stdio","command":t.executable,"args":args})
    })
}
fn http_entry(client: &str, url: &str, token: &str) -> Value {
    if client == "codex" {
        json!({"url":url,"http_headers":{"Authorization":format!("Bearer {token}")},"tool_timeout_sec":60})
    } else {
        json!({"type":"http","url":url,"headers":{"Authorization":format!("Bearer {token}")}})
    }
}
fn toml_table(value: &Value) -> Result<Table, String> {
    let mut table = Table::new();
    for (key, v) in value.as_object().ok_or("MCP 条目必须是对象")? {
        let item = match v {
            Value::String(s) => toml_edit::value(s),
            Value::Number(n) => toml_edit::value(n.as_i64().ok_or("无效的配置数值")?),
            Value::Array(a) => {
                let mut values = toml_edit::Array::new();
                for v in a {
                    values.push(v.as_str().ok_or("配置参数必须是字符串")?);
                }
                toml_edit::value(values)
            }
            Value::Object(_) => Item::Table(toml_table(v)?),
            _ => return Err("不支持的 MCP 配置值".into()),
        };
        table.insert(key, item);
    }
    Ok(table)
}
fn merged(
    client: &str,
    bytes: &Option<Vec<u8>>,
    name: &str,
    value: Option<&Value>,
) -> Result<Vec<u8>, String> {
    let source = bytes.as_deref().map(text).transpose()?.unwrap_or("");
    let rendered = if client == "codex" {
        let mut doc: DocumentMut = source.parse().map_err(|_| "Codex TOML 配置无效")?;
        if doc.get("mcp_servers").is_none() {
            doc["mcp_servers"] = Item::Table(Table::new());
        }
        let inline = doc["mcp_servers"].is_inline_table();
        let servers = doc["mcp_servers"]
            .as_table_like_mut()
            .ok_or("mcp_servers 必须是表")?;
        if let Some(v) = value {
            let table = toml_table(v)?;
            let item = if inline {
                Item::Value(toml_edit::Value::InlineTable(table.into_inline_table()))
            } else {
                Item::Table(table)
            };
            servers.insert(name, item);
        } else {
            servers.remove(name);
        }
        doc.to_string()
    } else {
        let mut doc = if bytes.is_none() {
            json!({})
        } else {
            parsed(client, source)?
        };
        let root = doc.as_object_mut().ok_or("JSON 配置根节点必须是对象")?;
        let servers = root
            .entry("mcpServers")
            .or_insert_with(|| json!({}))
            .as_object_mut()
            .ok_or("mcpServers 必须是对象")?;
        if let Some(v) = value {
            servers.insert(name.into(), v.clone());
        } else {
            servers.remove(name);
        }
        serde_json::to_string_pretty(&doc).map_err(|e| e.to_string())? + "\n"
    };
    let rendered = rendered.replace("\r\n", "\n");
    Ok(if source.contains("\r\n") {
        rendered.replace('\n', "\r\n").into_bytes()
    } else {
        rendered.into_bytes()
    })
}
fn target_path(home: &Path, t: &Target) -> Result<PathBuf, String> {
    if !matches!(t.client.as_str(), "codex" | "claude")
        || !matches!(t.scope.as_str(), "user" | "project")
        || !matches!(t.transport.as_str(), "http" | "stdio")
    {
        return Err("请选择支持的客户端、作用域与传输方式".into());
    }
    if t.entry.is_empty()
        || t.entry.len() > 64
        || !t
            .entry
            .bytes()
            .all(|c| c.is_ascii_alphanumeric() || c == b'-' || c == b'_')
    {
        return Err("条目名称只能使用英文字母、数字、短横线和下划线".into());
    }
    validate_mcp_url(&t.url)?;
    if t.transport == "stdio"
        && (!Path::new(&t.executable).is_absolute() || !Path::new(&t.executable).is_file())
    {
        return Err("STDIO 需要已安装的 dbmcp 程序，请选择其绝对路径；也可改用 HTTP".into());
    }
    let base = if t.scope == "user" {
        home.to_path_buf()
    } else {
        let path = PathBuf::from(&t.project_dir);
        if !path.is_absolute() || !path.is_dir() {
            return Err("项目作用域需要现有项目目录的绝对路径".into());
        }
        fs::canonicalize(path).map_err(|e| e.to_string())?
    };
    Ok(base.join(match (t.client.as_str(), t.scope.as_str()) {
        ("codex", _) => ".codex/config.toml",
        ("claude", "user") => ".claude.json",
        _ => ".mcp.json",
    }))
}
fn validate_mcp_url(value: &str) -> Result<(), String> {
    let url = reqwest::Url::parse(value).map_err(|_| "MCP URL 无效")?;
    let local = matches!(
        url.host_str(),
        Some("localhost" | "127.0.0.1" | "::1" | "[::1]")
    );
    if url.host_str().is_none()
        || url.username() != ""
        || url.password().is_some()
        || url.fragment().is_some()
        || !(url.scheme() == "https" || (url.scheme() == "http" && local))
    {
        return Err("MCP 请使用 HTTPS 或本机 HTTP 地址".into());
    }
    Ok(())
}
fn private(path: &Path, directory: bool) -> Result<(), String> {
    #[cfg(windows)]
    {
        let mut who = Command::new("whoami.exe");
        who.args(["/user", "/fo", "csv", "/nh"]);
        hide(&mut who);
        let out = who.output().map_err(|e| e.to_string())?;
        let output = String::from_utf8_lossy(&out.stdout);
        let sid = output
            .rsplit(',')
            .next()
            .unwrap_or("")
            .trim()
            .trim_matches('"');
        if !out.status.success()
            || !sid.starts_with("S-1-5-")
            || !sid
                .bytes()
                .all(|b| b.is_ascii_digit() || b == b'-' || b == b'S')
        {
            return Err("无法识别当前文件所有者".into());
        }
        let rights = if directory { "(OI)(CI)F" } else { "F" };
        let mut cmd = Command::new("icacls.exe");
        hide(&mut cmd);
        let out = cmd
            .arg(path)
            .args([
                "/inheritance:r",
                "/grant:r",
                &format!("*{sid}:{rights}"),
                &format!("*S-1-5-18:{rights}"),
                &format!("*S-1-5-32-544:{rights}"),
            ])
            .output()
            .map_err(|e| e.to_string())?;
        if !out.status.success() {
            return Err("无法限制配置文件权限".into());
        }
    }
    #[cfg(not(windows))]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(
            path,
            fs::Permissions::from_mode(if directory { 0o700 } else { 0o600 }),
        )
        .map_err(|e| e.to_string())?;
    }
    Ok(())
}
fn save_private(path: &Path, bytes: &[u8]) -> Result<(), String> {
    let parent = path.parent().ok_or("文件目录无效")?;
    fs::create_dir_all(parent).map_err(|e| e.to_string())?;
    let mut temp = tempfile::NamedTempFile::new_in(parent).map_err(|e| e.to_string())?;
    private(temp.path(), false)?;
    temp.write_all(bytes).map_err(|e| e.to_string())?;
    temp.as_file().sync_all().map_err(|e| e.to_string())?;
    temp.persist(path).map_err(|e| e.to_string())?;
    Ok(())
}
fn replace(path: &Path, expected: &str, bytes: &[u8], id: &str) -> Result<Option<PathBuf>, String> {
    let before = read(path)?;
    if fingerprint(&before) != expected {
        return Err("配置已被外部修改，请重新预览".into());
    }
    let backup = if let Some(original) = &before {
        let name = path.file_name().ok_or("配置路径无效")?.to_string_lossy();
        let backup = path.with_file_name(format!("{name}.dbmcp-backup-{id}"));
        if backup.exists() {
            return Err("备份已存在，请重新预览".into());
        }
        save_private(&backup, original)?;
        Some(backup)
    } else {
        None
    };
    // Optimistic check immediately before the atomic replacement. Agent clients
    // do not share an advisory lock, so previews must never be treated as locks.
    if fingerprint(&read(path)?) != expected {
        return Err("配置已被外部修改，原文件已保留".into());
    }
    save_private(path, bytes)?;
    Ok(backup)
}
fn record_dir(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    let p = read_profile(&app.state::<Desktop>())?;
    let root = app.path().app_config_dir().map_err(|e| e.to_string())?;
    let path = if p.id == "legacy" {
        root.join("integrations")
    } else {
        root.join("profiles").join(p.id).join("integrations")
    };
    fs::create_dir_all(&path).map_err(|e| e.to_string())?;
    private(&path, true)?;
    Ok(path)
}
fn records(dir: &Path) -> Result<Vec<Record>, String> {
    let mut result = vec![];
    for item in fs::read_dir(dir).map_err(|e| e.to_string())? {
        let path = item.map_err(|e| e.to_string())?.path();
        if path.extension().is_some_and(|x| x == "json")
            && !path
                .file_name()
                .is_some_and(|x| x.to_string_lossy().ends_with(".credential.json"))
        {
            let bytes = read(&path)?.ok_or("安装记录消失")?;
            let record: Record = serde_json::from_slice(&bytes)
                .map_err(|_| "安装记录损坏，请保留文件并手动恢复配置备份")?;
            let valid_id = |s: &str| s.len() == 32 && s.bytes().all(|c| c.is_ascii_hexdigit());
            let valid_identity = if record.registration_id.is_empty() {
                valid_id(&record.credential_id) && valid_id(&record.entity_id)
            } else {
                record.registration_id == record.id
                    && record.credential_id.is_empty()
                    && record.entity_id.is_empty()
            };
            if !valid_id(&record.id)
                || !valid_identity
                || !valid_id(&record.server_id)
                || path.file_stem().and_then(|v| v.to_str()) != Some(&record.id)
                || !matches!(record.client.as_str(), "codex" | "claude")
            {
                return Err("安装记录身份无效，请保留原文件并在 Server 撤销对应凭据".into());
            }
            result.push(record);
        }
    }
    Ok(result)
}
fn owned<'a>(records: &'a [Record], path: &Path, name: &str, value: &Value) -> Option<&'a Record> {
    records
        .iter()
        .find(|r| r.path == path && r.entry == name && r.entry_hash == entry_hash(value))
}
fn preview(path: PathBuf, dir: &Path, t: &Target) -> Result<Preview, String> {
    let bytes = read(&path)?;
    let existing = current(&t.client, &bytes, &t.entry)?;
    let all = records(dir)?;
    let state = match existing.as_ref() {
        None => "new",
        Some(v) if owned(&all, &path, &t.entry, v).is_some() => "managed",
        Some(_) => "conflict",
    };
    Ok(Preview {
        path,
        fingerprint: fingerprint(&bytes),
        state: state.into(),
        entry_preview: config_entry(t, "<安装时签发>", Path::new("<受限凭据文件>"))?,
    })
}

fn app_target(app: &tauri::AppHandle, t: &Target) -> Result<PathBuf, String> {
    let home = app.path().home_dir().map_err(|e| e.to_string())?;
    let path = target_path(&home, t)?;
    if t.client == "codex" && t.scope == "user" {
        if let Some(custom) = std::env::var_os("CODEX_HOME").filter(|s| !s.is_empty()) {
            let custom = PathBuf::from(custom);
            if !custom.is_absolute() {
                return Err("CODEX_HOME 必须是绝对路径".into());
            }
            return Ok(custom.join("config.toml"));
        }
    }
    Ok(path)
}

#[tauri::command]
pub fn integration_list(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    session: crate::profiles::Stamp,
) -> Result<Vec<Record>, String> {
    let _guard = crate::profiles::operation(&state, &session)?;
    records(&record_dir(&app)?)
}
#[tauri::command]
pub fn integration_preview(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    session: crate::profiles::Stamp,
    target: Target,
) -> Result<Preview, String> {
    let _guard = crate::profiles::operation(&state, &session)?;
    preview(app_target(&app, &target)?, &record_dir(&app)?, &target)
}

// A manual export uses an already-issued entity credential. Identity, policy,
// rotation and revocation remain entirely in Server.
fn export_at(path: &Path, url: &str, token: &str) -> Result<Value, String> {
    validate_mcp_url(url)?;
    if token.is_empty() {
        return Err("请先签发实体凭据".into());
    }
    let before = read(path)?;
    let output = json!({"mcpServers":{"dbmcp":http_entry("generic",url,token)}});
    let id = format!(
        "export-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map_err(|e| e.to_string())?
            .as_nanos()
    );
    let backup = replace(
        path,
        &fingerprint(&before),
        &(serde_json::to_string_pretty(&output).map_err(|e| e.to_string())? + "\n").into_bytes(),
        &id,
    )?;
    Ok(json!({"path":path,"backup":backup}))
}
#[tauri::command]
pub async fn integration_export(
    app: tauri::AppHandle,
    url: String,
    token: String,
    state: State<'_, Desktop>,
    session: crate::profiles::Stamp,
) -> Result<Value, String> {
    let _guard = crate::profiles::operation(&state, &session)?;
    validate_mcp_url(&url)?;
    tauri::async_runtime::spawn_blocking(move || {
        let Some(file) = app
            .dialog()
            .file()
            .set_title("导出包含实体凭据的 MCP 配置")
            .add_filter("MCP JSON", &["json"])
            .set_file_name("dbmcp.mcp.json")
            .blocking_save_file()
        else {
            return Ok(Value::Null);
        };
        let path = file.into_path().map_err(|e| e.to_string())?;
        export_at(&path, &url, &token)
    })
    .await
    .map_err(|e| e.to_string())?
}
async fn admin(
    state: &Desktop,
    p: &Profile,
    token: &str,
    method: &str,
    path: &str,
    body: Option<Value>,
) -> Result<Value, String> {
    request(state, p, token, method, &format!("/admin/v1/{path}"), body).await
}
async fn revoke(state: &Desktop, p: &Profile, token: &str, r: &Record) -> Result<(), String> {
    if !r.registration_id.is_empty() {
        admin(
            state,
            p,
            token,
            "POST",
            &format!("registrations/{}/revoke", r.registration_id),
            None,
        )
        .await?;
        return Ok(());
    }
    admin(
        state,
        p,
        token,
        "POST",
        &format!("credentials/{}/revoke", r.credential_id),
        None,
    )
    .await?;
    // Dedicated installation entities can be disabled without changing another Agent.
    let list = admin(state, p, token, "GET", "entities", None).await?;
    if let Some(mut entity) = list
        .as_array()
        .and_then(|v| v.iter().find(|e| e["id"] == r.entity_id))
        .cloned()
    {
        entity["enabled"] = json!(false);
        admin(
            state,
            p,
            token,
            "PUT",
            &format!("entities/{}", r.entity_id),
            Some(entity),
        )
        .await?;
    }
    Ok(())
}
fn discard(dir: &Path, r: &Record) -> Result<(), String> {
    // Paths are derived from the recorded credential ID; never delete an
    // arbitrary path supplied through a modified local manifest.
    for path in [
        dir.join(format!("{}.credential.json", r.id)),
        dir.join(format!("{}.json", r.id)),
    ] {
        match fs::remove_file(path) {
            Ok(()) => {}
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
            Err(e) => return Err(e.to_string()),
        }
    }
    Ok(())
}

#[tauri::command]
pub async fn integration_install(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    target: Target,
    expected: String,
    session: crate::profiles::Stamp,
) -> Result<Value, String> {
    let _guard = crate::profiles::operation(&state, &session)?;
    if !state.session.lock().map_err(|e| e.to_string())?.connected {
        return Err("当前环境未连接".into());
    }
    let p = read_profile(&state)?;
    let token = entry(&p.id)?
        .get_password()
        .map_err(|_| "管理员凭据不可用")?;
    let dir = record_dir(&app)?;
    let plan = preview(app_target(&app, &target)?, &dir, &target)?;
    install_at(
        &state,
        &p,
        &token,
        &dir,
        plan,
        InstallInput { target, expected },
    )
    .await
}

struct InstallInput {
    target: Target,
    expected: String,
}
async fn install_at(
    state: &Desktop,
    p: &Profile,
    token: &str,
    dir: &Path,
    plan: Preview,
    input: InstallInput,
) -> Result<Value, String> {
    let InstallInput { target, expected } = input;
    if plan.fingerprint != expected || plan.state == "conflict" {
        return Err("配置已变化或条目名称冲突，请重新预览".into());
    }
    let source = read(&plan.path)?;
    if fingerprint(&source) != expected {
        return Err("配置已被外部修改，请重新预览".into());
    }
    let all = records(dir)?;
    let old = current(&target.client, &source, &target.entry)?
        .as_ref()
        .and_then(|v| owned(&all, &plan.path, &target.entry, v))
        .cloned();
    let issued = admin(
        state,
        p,
        token,
        "POST",
        "registrations",
        Some(json!({"name":format!("{} · {} · {}",target.client,target.scope,target.entry)})),
    )
    .await?;
    let registration_id = issued["registration"]["id"]
        .as_str()
        .ok_or("Server 登记响应无效")?
        .to_string();
    let mut record = Record {
        id: registration_id.clone(),
        client: target.client.clone(),
        path: plan.path.clone(),
        entry: target.entry.clone(),
        transport: target.transport.clone(),
        server_id: p.server_id.clone(),
        entity_id: String::new(),
        registration_id: registration_id.clone(),
        credential_id: String::new(),
        entry_hash: String::new(),
        secret_file: None,
        backup: None,
    };
    let install: Result<(), String> = async {
        let agent_token = issued["token"].as_str().ok_or("凭据响应无效")?;
        let secret = dir.join(format!("{}.credential.json", record.id));
        if target.transport == "stdio" {
            save_private(
                &secret,
                &serde_json::to_vec(&json!({"url":target.url,"token":agent_token}))
                    .map_err(|e| e.to_string())?,
            )?;
            record.secret_file = Some(secret.clone());
        }
        let desired = config_entry(&target, agent_token, &secret)?;
        record.entry_hash = entry_hash(&desired);
        // Persist recovery information before touching an external config file.
        save_private(
            &dir.join(format!("{}.json", record.id)),
            &serde_json::to_vec_pretty(&record).map_err(|e| e.to_string())?,
        )?;
        let output = merged(&target.client, &source, &target.entry, Some(&desired))?;
        record.backup = replace(&plan.path, &expected, &output, &record.id)?;
        Ok(())
    }
    .await;
    if let Err(error) = install {
        let mut cleanup = vec![];
        if let Err(e) = revoke(state, p, token, &record).await {
            cleanup.push(e);
        } else if let Err(e) = discard(dir, &record) {
            cleanup.push(e);
        }
        return Err(if cleanup.is_empty() {
            error
        } else {
            format!("{error}；接入凭据清理未完成，请在 Agent 接入中重试移除：{registration_id}")
        });
    }
    let mut warnings = vec![];
    if save_private(
        &dir.join(format!("{}.json", record.id)),
        &serde_json::to_vec_pretty(&record).map_err(|e| e.to_string())?,
    )
    .is_err()
    {
        warnings.push("配置已安装；备份路径记录更新失败，原备份仍在目标文件旁".to_string());
    }
    if let Some(old) = old {
        if old.server_id == p.server_id {
            match revoke(state, p, token, &old).await {
                Ok(()) => {
                    if discard(dir, &old).is_err() {
                        warnings.push("旧凭据已撤销，安装记录清理失败".into());
                    }
                }
                Err(_) => warnings.push("新配置已安装；请在原安装记录中撤销旧凭据".into()),
            }
        } else {
            warnings.push("请连接原 Server 后撤销旧安装凭据".into());
        }
    }
    Ok(json!({"record":record,"warnings":warnings}))
}

#[tauri::command]
pub fn integration_remove_preview(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    session: crate::profiles::Stamp,
    id: String,
) -> Result<Value, String> {
    let _guard = crate::profiles::operation(&state, &session)?;
    let r = records(&record_dir(&app)?)?
        .into_iter()
        .find(|r| r.id == id)
        .ok_or("安装记录不存在")?;
    let bytes = read(&r.path)?;
    let ours = current(&r.client, &bytes, &r.entry)
        .ok()
        .flatten()
        .is_some_and(|v| entry_hash(&v) == r.entry_hash);
    Ok(
        json!({"path":r.path,"fingerprint":fingerprint(&bytes),"remove_entry":ours,"message":if ours {"只删除本工具创建的条目并撤销凭据；保留其他设置"} else {"条目缺失或已改写：保留配置，仅撤销这次安装的凭据"}}),
    )
}
#[tauri::command]
pub async fn integration_remove(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    id: String,
    expected: String,
    session: crate::profiles::Stamp,
) -> Result<Value, String> {
    let _guard = crate::profiles::operation(&state, &session)?;
    let dir = record_dir(&app)?;
    let r = records(&dir)?
        .into_iter()
        .find(|r| r.id == id)
        .ok_or("安装记录不存在")?;
    if !state.session.lock().map_err(|e| e.to_string())?.connected {
        return Err("当前环境未连接".into());
    }
    let p = read_profile(&state)?;
    if p.server_id != r.server_id {
        return Err("请先连接这次安装使用的 Server，以便撤销凭据".into());
    }
    let token = entry(&p.id)?
        .get_password()
        .map_err(|_| "管理员凭据不可用")?;
    remove_at(&state, &p, &token, &dir, r, &expected).await
}
async fn remove_at(
    state: &Desktop,
    p: &Profile,
    token: &str,
    dir: &Path,
    r: Record,
    expected: &str,
) -> Result<Value, String> {
    if p.server_id != r.server_id {
        return Err("请先连接这次安装使用的 Server，以便撤销凭据".into());
    }
    let bytes = read(&r.path)?;
    if fingerprint(&bytes) != expected {
        return Err("配置已被外部修改，请重新预览".into());
    }
    let ours = current(&r.client, &bytes, &r.entry)
        .ok()
        .flatten()
        .is_some_and(|v| entry_hash(&v) == r.entry_hash);
    let backup = if ours {
        Some(replace(
            &r.path,
            expected,
            &merged(&r.client, &bytes, &r.entry, None)?,
            &format!("remove-{}", r.id),
        )?)
    } else {
        None
    };
    revoke(state, p, token, &r)
        .await
        .map_err(|_| "配置已处理，但凭据撤销失败；请保持连接后重试移除或到实体页撤销")?;
    discard(dir, &r)?;
    Ok(json!({"removed_entry":ours,"backup":backup,"revoked":true}))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn generic_export_keeps_backup_and_rejects_insecure_remote_endpoint() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("通用 MCP.json");
        fs::write(&path, b"original file\n").unwrap();
        assert!(export_at(&path, "http://remote.example/mcp", "test-secret").is_err());
        assert_eq!(fs::read(&path).unwrap(), b"original file\n");
        assert_eq!(fs::read_dir(dir.path()).unwrap().count(), 1);
        let result = export_at(&path, "https://mcp.example/mcp", "test-secret").unwrap();
        let saved: Value = serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
        assert_eq!(
            saved["mcpServers"]["dbmcp"]["headers"]["Authorization"],
            "Bearer test-secret"
        );
        assert_eq!(
            fs::read(result["backup"].as_str().unwrap()).unwrap(),
            b"original file\n"
        );
        assert!(!result.to_string().contains("test-secret"));
    }

    // Uses the actual Go coordinator, without a database connection, real user
    // configuration, credential store, or a Tauri window.
    #[cfg(windows)]
    #[test]
    #[ignore = "requires DBMCP_TEST_SERVER pointing to a built Go Server"]
    fn real_server_install_replace_remove_and_failed_save_revoke() {
        use base64::Engine;
        use std::{
            net::TcpListener, os::windows::fs::OpenOptionsExt, process::Stdio, sync::Mutex,
            time::Duration,
        };
        struct ChildGuard(std::process::Child);
        impl Drop for ChildGuard {
            fn drop(&mut self) {
                let _ = self.0.kill();
                let _ = self.0.wait();
            }
        }
        let exe = std::env::var("DBMCP_TEST_SERVER").expect("DBMCP_TEST_SERVER is required");
        let root = tempfile::tempdir().unwrap();
        let data = root.path().join("Server 中文 data");
        let output = Command::new(&exe)
            .arg("--data-dir")
            .arg(&data)
            .arg("init")
            .output()
            .unwrap();
        assert!(output.status.success(), "Server initialization failed");
        let init: Value = serde_json::from_slice(&output.stdout).unwrap();
        let admin_port = TcpListener::bind("127.0.0.1:0").unwrap();
        let mcp_port = TcpListener::bind("127.0.0.1:0").unwrap();
        let admin_addr = admin_port.local_addr().unwrap();
        let mcp_addr = mcp_port.local_addr().unwrap();
        fs::write(
            data.join("deployment.toml"),
            format!("admin = '{admin_addr}'\nmcp = '{mcp_addr}'\n"),
        )
        .unwrap();
        drop((admin_port, mcp_port));
        let mut command = Command::new(&exe);
        hide(&mut command);
        let _child = ChildGuard(
            command
                .arg("--data-dir")
                .arg(&data)
                .arg("serve")
                .stdout(Stdio::null())
                .stderr(Stdio::null())
                .spawn()
                .unwrap(),
        );
        let p = Profile {
            endpoint: format!("http://{admin_addr}"),
            server_id: init["server_id"].as_str().unwrap().into(),
            ..Profile::default()
        };
        let state = Desktop {
            windows: Mutex::new(crate::windows::WindowState::default()),
            session: Mutex::new(crate::profiles::Runtime {
                profile: Some(p.clone()),
                ..Default::default()
            }),
            catalog: Mutex::new(crate::profiles::Store::default()),
            operations: tokio::sync::RwLock::new(()),
            events: tokio::sync::watch::channel(()).0,
            exiting: std::sync::atomic::AtomicBool::new(false),
            preferences: Mutex::new(crate::preferences::Preferences::default()),
            notifications: Mutex::new(crate::notifications::NotificationState::default()),
            http: reqwest::Client::builder()
                .no_proxy()
                .timeout(Duration::from_secs(10))
                .build()
                .unwrap(),
        };
        let token = base64::engine::general_purpose::URL_SAFE_NO_PAD
            .encode(fs::read(data.join("admin.token")).unwrap());
        tauri::async_runtime::block_on(async {
            let mut ready = false;
            for _ in 0..100 {
                if admin(&state, &p, &token, "GET", "health", None)
                    .await
                    .is_ok()
                {
                    ready = true;
                    break;
                }
                tokio::time::sleep(Duration::from_millis(50)).await;
            }
            assert!(ready, "Server did not become ready");
            let dir = root.path().join("records");
            fs::create_dir(&dir).unwrap();
            for client in ["codex", "claude"] {
                let path = root.path().join(format!("{client} 中文 config"));
                let original = if client == "codex" {
                    b"# retained\nmodel='keep'\n[mcp_servers.other]\ncommand='keep'\n".as_slice()
                } else {
                    br#"{"theme":"keep","mcpServers":{"other":{"command":"keep"}}}"#.as_slice()
                };
                fs::write(&path, original).unwrap();
                let mut t = target();
                t.client = client.into();
                t.url = format!("http://{mcp_addr}/mcp");
                t.executable = exe.clone();
                for transport in ["http", "stdio"] {
                    t.transport = transport.into();
                    let plan = preview(path.clone(), &dir, &t).unwrap();
                    let expected = plan.fingerprint.clone();
                    install_at(
                        &state,
                        &p,
                        &token,
                        &dir,
                        plan,
                        InstallInput {
                            target: t.clone(),
                            expected,
                        },
                    )
                    .await
                    .unwrap();
                    assert_eq!(records(&dir).unwrap().len(), 1);
                    let r = records(&dir).unwrap().pop().unwrap();
                    let saved = fs::read(&path).unwrap();
                    assert!(text(&saved).unwrap().contains("keep"));
                    assert!(!fs::read_to_string(dir.join(format!("{}.json", r.id)))
                        .unwrap()
                        .contains("Bearer "));
                    if transport == "stdio" {
                        let secret: Value = serde_json::from_slice(
                            &fs::read(r.secret_file.as_ref().unwrap()).unwrap(),
                        )
                        .unwrap();
                        assert!(!text(&saved)
                            .unwrap()
                            .contains(secret["token"].as_str().unwrap()));
                        remove_at(&state, &p, &token, &dir, r, &fingerprint(&Some(saved)))
                            .await
                            .unwrap();
                    }
                }
                assert!(records(&dir).unwrap().is_empty());
                assert!(current(client, &read(&path).unwrap(), "dbmcp")
                    .unwrap()
                    .is_none());
                let before = fs::read(&path).unwrap();
                let lock = fs::OpenOptions::new()
                    .read(true)
                    .share_mode(3)
                    .open(&path)
                    .unwrap();
                let plan = preview(path.clone(), &dir, &t).unwrap();
                let expected = plan.fingerprint.clone();
                assert!(install_at(
                    &state,
                    &p,
                    &token,
                    &dir,
                    plan,
                    InstallInput {
                        target: t.clone(),
                        expected,
                    }
                )
                .await
                .is_err());
                drop(lock);
                assert_eq!(fs::read(&path).unwrap(), before);
                assert_eq!(fs::read_dir(&dir).unwrap().count(), 0);
            }
            let entities = admin(&state, &p, &token, "GET", "entities", None)
                .await
                .unwrap();
            assert!(
                entities.as_array().unwrap().is_empty(),
                "installing config must defer entity creation until first use"
            );
            let registrations = admin(&state, &p, &token, "GET", "registrations", None)
                .await
                .unwrap();
            assert_eq!(registrations.as_array().unwrap().len(), 6);
            for registration in registrations.as_array().unwrap() {
                assert_eq!(registration["status"], "revoked");
            }
        });
    }

    fn target() -> Target {
        Target {
            client: "codex".into(),
            scope: "user".into(),
            project_dir: String::new(),
            entry: "dbmcp".into(),
            transport: "http".into(),
            url: "http://127.0.0.1:19877/mcp".into(),
            executable: String::new(),
        }
    }
    #[test]
    fn toml_preserves_comments_settings_other_servers_and_crlf() {
        let original=b"# my settings\r\nmodel = 'keep-me' # model comment\r\n\r\n[mcp_servers.other]\r\ncommand = 'other-tool' # other comment\r\n".to_vec();
        let entry = config_entry(&target(), "secret", Path::new("unused")).unwrap();
        let saved = merged("codex", &Some(original.clone()), "dbmcp", Some(&entry)).unwrap();
        let source = text(&saved).unwrap();
        for comment in ["# my settings", "# model comment", "# other comment"] {
            assert!(source.contains(comment));
        }
        assert!(!source.replace("\r\n", "").contains('\n'));
        assert_eq!(
            current("codex", &Some(saved.clone()), "dbmcp").unwrap(),
            Some(entry)
        );
        let removed = merged("codex", &Some(saved), "dbmcp", None).unwrap();
        let value = parsed("codex", text(&removed).unwrap()).unwrap();
        assert_eq!(value["model"], "keep-me");
        assert_eq!(value["mcp_servers"]["other"]["command"], "other-tool");
        assert!(value["mcp_servers"].get("dbmcp").is_none());
    }
    #[test]
    fn json_and_inline_toml_keep_unrelated_values() {
        let source=Some(br#"{"theme":"dark","projects":{"x":{"allowed":true}},"mcpServers":{"other":{"type":"http","url":"https://example.test"}}}"#.to_vec());
        let mut t = target();
        t.client = "claude".into();
        let item = config_entry(&t, "secret", Path::new("unused")).unwrap();
        assert_eq!(item["type"], "http");
        let saved = merged("claude", &source, "dbmcp", Some(&item)).unwrap();
        let removed = merged("claude", &Some(saved), "dbmcp", None).unwrap();
        assert_eq!(
            parsed("claude", text(&removed).unwrap()).unwrap(),
            parsed("claude", text(source.as_ref().unwrap()).unwrap()).unwrap()
        );
        let inline = Some(b"mcp_servers = { other = { command = 'keep' } }\n".to_vec());
        let saved = merged(
            "codex",
            &inline,
            "dbmcp",
            Some(&config_entry(&target(), "secret", Path::new("unused")).unwrap()),
        )
        .unwrap();
        let value = parsed("codex", text(&saved).unwrap()).unwrap();
        assert_eq!(value["mcp_servers"]["other"]["command"], "keep");
        assert_eq!(
            value["mcp_servers"]["dbmcp"]["http_headers"]["Authorization"],
            "Bearer secret"
        );
    }
    #[test]
    fn external_change_and_invalid_input_never_truncate_original() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("中文 空格 config.toml");
        fs::write(&path, b"# original\n").unwrap();
        let expected = fingerprint(&read(&path).unwrap());
        fs::write(&path, b"# another editor changed it\n").unwrap();
        assert!(replace(&path, &expected, b"replacement", "test").is_err());
        assert_eq!(fs::read(&path).unwrap(), b"# another editor changed it\n");
        assert!(merged(
            "claude",
            &Some(b"{invalid".to_vec()),
            "dbmcp",
            Some(&json!({}))
        )
        .is_err());
        let expected = fingerprint(&read(&path).unwrap());
        let backup = replace(&path, &expected, b"# saved\n", "test")
            .unwrap()
            .unwrap();
        assert_eq!(fs::read(backup).unwrap(), b"# another editor changed it\n");
        assert_eq!(fs::read(&path).unwrap(), b"# saved\n");
    }
    #[test]
    fn preview_detects_foreign_entry_without_exposing_its_secrets() {
        let home = tempfile::tempdir().unwrap();
        let records = tempfile::tempdir().unwrap();
        let path = home.path().join(".codex/config.toml");
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::write(&path,b"[mcp_servers.dbmcp]\nurl='https://existing.test'\nhttp_headers={Authorization='private-existing-value'}\n").unwrap();
        let plan = preview(
            target_path(home.path(), &target()).unwrap(),
            records.path(),
            &target(),
        )
        .unwrap();
        assert_eq!(plan.state, "conflict");
        assert!(!serde_json::to_string(&plan)
            .unwrap()
            .contains("private-existing-value"));
        assert_eq!(plan.path, path);
    }
    #[test]
    fn stdio_paths_remain_distinct_arguments_and_owned_hash_is_semantic() {
        let mut t = target();
        t.transport = "stdio".into();
        t.executable = "D:/中文 目录/dbmcp.exe".into();
        let value = config_entry(
            &t,
            "not-on-command-line",
            Path::new("D:/中文 目录/private.json"),
        )
        .unwrap();
        assert_eq!(
            value["args"],
            json!(["bridge", "--credentials", "D:/中文 目录/private.json"])
        );
        assert!(!value.to_string().contains("not-on-command-line"));
        assert_eq!(
            entry_hash(&json!({"url":"x","headers":{"b":2,"a":1}})),
            entry_hash(&json!({"headers":{"a":1,"b":2},"url":"x"}))
        );
        assert_ne!(
            entry_hash(&value),
            entry_hash(&json!({"command":"user-modified"}))
        );
    }
}

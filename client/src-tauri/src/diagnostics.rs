use crate::{entry, read_profile, Desktop};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{
    backtrace::Backtrace,
    fs::{self, File},
    io::{BufRead, BufReader, Read, Write},
    path::{Path, PathBuf},
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tauri::Manager;
use tauri_plugin_dialog::DialogExt;

const FILE_LIMIT: u64 = 2 << 20;
const COLLECTION_LIMIT: usize = 8 << 20;
struct ActiveLog {
    _file: File,
}

pub fn plugin<R: tauri::Runtime>() -> tauri::plugin::TauriPlugin<R> {
    tauri_plugin_log::Builder::new()
        .level(log::LevelFilter::Info)
        .filter(|m| m.target() == "dbmcp_diagnostics")
        .clear_format()
        .max_file_size(FILE_LIMIT.into())
        .rotation_strategy(tauri_plugin_log::RotationStrategy::KeepSome(3))
        .targets([tauri_plugin_log::Target::new(
            tauri_plugin_log::TargetKind::LogDir {
                file_name: Some(format!("desktop-{}", std::process::id())),
            },
        )])
        .build()
}
pub fn now_ms() -> u128 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis()
}
pub fn id() -> String {
    use std::sync::atomic::{AtomicU64, Ordering};
    static SEQUENCE: AtomicU64 = AtomicU64::new(0);
    format!(
        "{:016x}{:08x}{:08x}",
        now_ms() as u64,
        std::process::id(),
        SEQUENCE.fetch_add(1, Ordering::Relaxed) as u32
    )
}
pub fn fingerprint(value: &str) -> String {
    format!("{:x}", Sha256::digest(value.as_bytes()))[..16].into()
}
pub fn event(name: &str, fields: Value) {
    let fields = sanitize(fields);
    log::info!(target:"dbmcp_diagnostics", "{}", json!({
        "time_unix_ms":now_ms(),"component":"desktop","pid":std::process::id(),
        "version":env!("CARGO_PKG_VERSION"),"revision":option_env!("DBMCP_BUILD_REV").unwrap_or("unknown"),
        "msg":name,"fields":fields
    }));
    log::logger().flush();
}
pub fn result<T>(operation: &str, result: &Result<T, String>) {
    event(
        operation,
        match result {
            Ok(_) => json!({"status":"succeeded"}),
            Err(e) => json!({"status":"failed","error_fingerprint":fingerprint(e)}),
        },
    );
}
pub fn init(app: &tauri::AppHandle) -> Result<(), String> {
    let dir = app.path().app_log_dir().map_err(|e| e.to_string())?;
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    let file = File::options()
        .read(true)
        .write(true)
        .create(true)
        .truncate(false)
        .open(dir.join(format!("desktop-{}.lock", std::process::id())))
        .map_err(|e| e.to_string())?;
    file.try_lock().map_err(|e| e.to_string())?;
    app.manage(ActiveLog { _file: file });
    prune(&dir);
    std::panic::set_hook(Box::new(|info| {
        // Panic text may embed data. Locations and a stack identify the defect.
        event(
            "desktop.panic",
            json!({"location":info.location().map(|l|format!("{}:{}",l.file(),l.line())),"stack":Backtrace::force_capture().to_string()}),
        );
    }));
    event(
        "desktop.start",
        json!({"os":std::env::consts::OS,"arch":std::env::consts::ARCH,"log_dir":dir,"webview_version":tauri::webview_version().ok()}),
    );
    Ok(())
}

fn family(name: &str) -> Option<&str> {
    let rest = name.strip_prefix("desktop-")?;
    let length = rest.bytes().take_while(u8::is_ascii_digit).count();
    if length == 0 {
        return None;
    };
    Some(&name[.."desktop-".len() + length])
}
fn eligible(name: &str, desktop: bool) -> bool {
    if desktop {
        (family(name).is_some() && name.ends_with(".log")) || name == "installer-migration.log"
    } else {
        (name.starts_with("server-") || name.starts_with("cli-") || name.starts_with("bridge-"))
            && name.ends_with(".jsonl")
            && name
                .bytes()
                .all(|c| c.is_ascii_alphanumeric() || b"-._".contains(&c))
    }
}
fn prune(dir: &Path) {
    let Ok(entries) = fs::read_dir(dir) else {
        return;
    };
    let mut files: Vec<_> = entries
        .flatten()
        .filter_map(|e| {
            let name = e.file_name().to_string_lossy().to_string();
            if !eligible(&name, true) {
                return None;
            };
            let m = fs::symlink_metadata(e.path()).ok()?;
            if !m.is_file() {
                return None;
            };
            Some((name, m.modified().ok()?))
        })
        .collect();
    files.sort_by_key(|(_, t)| std::cmp::Reverse(*t));
    let mut inactive = 0;
    for (name, time) in files {
        let Some(base) = family(&name) else { continue };
        let Ok(lock) = File::options()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .open(dir.join(format!("{base}.lock")))
        else {
            continue;
        };
        if lock.try_lock().is_err() {
            continue;
        };
        inactive += 1;
        if inactive > 32 || time.elapsed().unwrap_or_default() > Duration::from_secs(7 * 86400) {
            let _ = fs::remove_file(dir.join(name));
        }
    }
}

fn user_logs() -> PathBuf {
    let home = std::env::var_os(if cfg!(windows) { "USERPROFILE" } else { "HOME" })
        .map(PathBuf::from)
        .unwrap_or_default();
    if cfg!(windows) {
        std::env::var_os("LOCALAPPDATA")
            .map(PathBuf::from)
            .unwrap_or_else(|| home.join("AppData/Local"))
            .join("DBMCP/logs")
    } else if cfg!(target_os = "macos") {
        home.join("Library/Logs/DBMCP")
    } else {
        std::env::var_os("XDG_STATE_HOME")
            .map(PathBuf::from)
            .filter(|p| p.is_absolute())
            .unwrap_or_else(|| home.join(".local/state"))
            .join("dbmcp/logs")
    }
}

fn collect(dir: &Path, desktop: bool) -> Value {
    let mut out = json!({"directory":dir,"files":[],"issues":[],"truncated":false});
    if !fs::symlink_metadata(dir).is_ok_and(|m| m.is_dir() && !m.file_type().is_symlink()) {
        out["issues"] = json!(["log directory missing or not regular"]);
        return out;
    }
    let Ok(entries) = fs::read_dir(dir) else {
        out["issues"] = json!(["cannot list logs"]);
        return out;
    };
    let mut files: Vec<_> = entries
        .flatten()
        .filter_map(|e| {
            let name = e.file_name().to_string_lossy().to_string();
            if !eligible(&name, desktop) {
                return None;
            };
            let m = fs::symlink_metadata(e.path()).ok()?;
            if !m.is_file() {
                return None;
            };
            Some((name, m.modified().ok()?))
        })
        .collect();
    files.sort_by_key(|(_, t)| std::cmp::Reverse(*t));
    let mut remaining = COLLECTION_LIMIT;
    for (index, (name, _)) in files.iter().enumerate() {
        if index >= 16 || remaining == 0 {
            out["truncated"] = json!(true);
            break;
        }
        let Ok(file) = File::open(dir.join(name)) else {
            out["issues"]
                .as_array_mut()
                .unwrap()
                .push(json!(format!("cannot read {name}")));
            continue;
        };
        let cap = (remaining as u64).min(FILE_LIMIT);
        let mut bytes = Vec::new();
        if file.take(cap + 1).read_to_end(&mut bytes).is_err() {
            out["issues"]
                .as_array_mut()
                .unwrap()
                .push(json!(format!("cannot read {name}")));
            continue;
        }
        let truncated = bytes.len() > cap as usize;
        bytes.truncate(cap as usize);
        remaining = remaining.saturating_sub(bytes.len());
        let mut records = Vec::new();
        let mut skipped = 0;
        for line in BufReader::new(bytes.as_slice()).split(b'\n') {
            let Ok(line) = line else {
                skipped += 1;
                continue;
            };
            if line.is_empty() {
                continue;
            }
            if line.len() > 65536 {
                skipped += 1;
                continue;
            }
            if let Ok(v) = serde_json::from_slice::<Value>(&line) {
                if v["component"].is_string() && v["msg"].is_string() {
                    records.push(sanitize(v));
                    continue;
                }
            }
            skipped += 1;
        }
        out["files"].as_array_mut().unwrap().push(
            json!({"name":name,"records":records,"truncated":truncated,"skipped_lines":skipped}),
        );
        if truncated {
            out["truncated"] = json!(true)
        }
    }
    out
}

#[tauri::command]
pub fn diagnostics_info(app: tauri::AppHandle) -> Result<Value, String> {
    Ok(
        json!({"desktop_dir":app.path().app_log_dir().map_err(|e|e.to_string())?,"process_dir":user_logs(),"file_size_mb":2,"rotated_files":3,"inactive_files":32,"retention_days":7,"tray_present":app.tray_by_id("main-tray").is_some(),"tray_items":crate::windows::TRAY_ITEMS.map(|(_,label)|label),"window_present":app.get_webview_window("main").is_some(),"lightweight":crate::windows::runtime_info(&app)?}),
    )
}

pub async fn snapshot(app: &tauri::AppHandle) -> Result<Value, String> {
    event("diagnostics.collect", json!({}));
    let state = app.state::<Desktop>();
    let profile = read_profile(&state).ok();
    let mut out = json!({"format":"dbmcp-diagnostics/v1","created_at_unix_ms":now_ms(),"desktop":{"version":env!("CARGO_PKG_VERSION"),"revision":option_env!("DBMCP_BUILD_REV").unwrap_or("unknown"),"os":std::env::consts::OS,"arch":std::env::consts::ARCH},"desktop_logs":collect(&app.path().app_log_dir().map_err(|e|e.to_string())?,true),"process_logs":collect(&user_logs(),false),"server_status":"not_configured","issues":[]});
    out["window"] = crate::windows::runtime_info(app)?;
    if let Some(profile) = profile {
        out["connection"] = json!({"endpoint":profile.endpoint,"server_id":profile.server_id,"data_dir":profile.data_dir});
        if !profile.data_dir.is_empty() {
            out["local_server_logs"] =
                collect(&PathBuf::from(&profile.data_dir).join("logs"), false)
        }
        let mut stage = "credential.load";
        let collected: Result<Value, String> = async {
            let token = entry(&profile.id)?
                .get_password()
                .map_err(|_| "administrator credential unavailable")?;
            stage = "server.request";
            tokio::time::timeout(
                Duration::from_secs(15),
                crate::request(
                    &state,
                    &profile,
                    &token,
                    "GET",
                    "/admin/v1/diagnostics",
                    None,
                ),
            )
            .await
            .map_err(|_| "MCP Server diagnostic request timed out".to_string())?
        }
        .await;
        match collected {
            Ok(v) => {
                out["server"] = sanitize(v);
                out["server_status"] = json!("collected")
            }
            Err(e) => {
                out["server_status"] = json!("unavailable");
                out["issues"] = json!([{"stage":stage,"error_fingerprint":fingerprint(&e),"message":"MCP Server 日志未取得：检查连接与认证；旧 MCP Server 需升级后才能提供诊断快照。"}])
            }
        }
    }
    Ok(out)
}

pub fn save(path: &Path, value: &Value) -> Result<(), String> {
    if path.extension().and_then(|x| x.to_str()) != Some("json") {
        return Err("诊断包请使用 .json 扩展名".into());
    }
    let parent = path.parent().ok_or("导出目录无效")?;
    let mut tmp = tempfile::NamedTempFile::new_in(parent).map_err(|e| e.to_string())?;
    serde_json::to_writer(tmp.as_file_mut(), value).map_err(|e| e.to_string())?;
    tmp.as_file_mut().flush().map_err(|e| e.to_string())?;
    tmp.as_file().sync_all().map_err(|e| e.to_string())?;
    tmp.persist(path).map_err(|e| e.to_string())?;
    Ok(())
}

pub async fn export(app: tauri::AppHandle) -> Result<Value, String> {
    let state = app.state::<Desktop>();
    let _guard = state.operations.try_read("导出诊断包（含文件选择）")?;
    let dialog_app = app.clone();
    let path = tauri::async_runtime::spawn_blocking(move || {
        dialog_app
            .dialog()
            .file()
            .set_title("导出诊断包")
            .add_filter("诊断 JSON", &["json"])
            .set_file_name(format!("dbmcp-diagnostics-{}.json", now_ms()))
            .blocking_save_file()
    })
    .await
    .map_err(|e| e.to_string())?;
    let Some(file) = path else {
        return Ok(Value::Null);
    };
    let path = file.into_path().map_err(|e| e.to_string())?;
    export_to(app.clone(), path).await
}

async fn export_to(app: tauri::AppHandle, path: PathBuf) -> Result<Value, String> {
    let parent = path
        .parent()
        .ok_or("导出目录无效")?
        .canonicalize()
        .map_err(|e| e.to_string())?;
    let mut protected = vec![app.path().app_config_dir().map_err(|e| e.to_string())?];
    if let Ok(profile) = read_profile(&app.state::<Desktop>()) {
        if !profile.data_dir.is_empty() {
            protected.push(PathBuf::from(profile.data_dir))
        }
    }
    for root in protected {
        if root
            .canonicalize()
            .is_ok_and(|root| parent.starts_with(root))
        {
            return Err("请将诊断包保存到应用配置和 MCP Server 数据目录之外".into());
        }
    }
    let value = snapshot(&app).await?;
    let status = value["server_status"].clone();
    let output = path.clone();
    tauri::async_runtime::spawn_blocking(move || save(&output, &value))
        .await
        .map_err(|e| e.to_string())??;
    event("diagnostics.exported", json!({"server_status":status}));
    Ok(json!({"path":path,"server_status":status}))
}

fn sanitize(value: Value) -> Value {
    match value {
        Value::Object(fields) => Value::Object(
            fields
                .into_iter()
                .filter_map(|(key, value)| {
                    if matches!(
                        key.to_ascii_lowercase().as_str(),
                        "password"
                            | "passwd"
                            | "token"
                            | "authorization"
                            | "credential"
                            | "credentials"
                            | "secret"
                            | "key"
                            | "sql"
                            | "params"
                            | "parameters"
                            | "rows"
                            | "result"
                            | "body"
                            | "payload"
                            | "message"
                    ) {
                        None
                    } else {
                        Some((key, sanitize(value)))
                    }
                })
                .collect(),
        ),
        Value::Array(values) => Value::Array(values.into_iter().map(sanitize).collect()),
        other => other,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn collection_excludes_secrets_nonlogs_and_marks_corruption() {
        let dir = tempfile::tempdir().unwrap();
        fs::write(dir.path().join("desktop-123.log"), b"{\"component\":\"desktop\",\"msg\":\"admin.failed\",\"fields\":{\"token\":\"TOKEN_CANARY\",\"nested\":{\"sql\":\"SQL_CANARY\"},\"status\":403}}\nnot-json\n").unwrap();
        fs::write(dir.path().join("admin.token"), "SECRET_CANARY").unwrap();
        let value = collect(dir.path(), true);
        let encoded = value.to_string();
        assert!(!encoded.contains("CANARY"));
        assert_eq!(value["files"][0]["skipped_lines"], 1);
        assert_eq!(value["files"][0]["records"][0]["fields"]["status"], 403);
        let output = dir.path().join("report.json");
        save(&output, &value).unwrap();
        let reread: Value = serde_json::from_slice(&fs::read(output).unwrap()).unwrap();
        assert_eq!(reread, value);
    }

    #[test]
    fn installer_migration_log_is_included_in_diagnostics() {
        let dir = tempfile::tempdir().unwrap();
        fs::write(
            dir.path().join("installer-migration.log"),
            "{\"component\":\"installer\",\"version\":\"1.0.4\",\"msg\":\"migration completed; D:\\\\工具\\\\db-mcp-tools\"}\n",
        )
        .unwrap();
        let value = collect(dir.path(), true);
        assert_eq!(value["files"][0]["records"][0]["component"], "installer");
        assert_eq!(value["files"][0]["skipped_lines"], 0);
        assert!(value.to_string().contains("工具"));
    }
}
#[tauri::command]
pub async fn diagnostics_export(
    app: tauri::AppHandle,
    destination: Option<PathBuf>,
) -> Result<Value, String> {
    let state = app.state::<Desktop>();
    let _guard = state.operations.try_read("导出诊断包（含文件选择）")?;
    match destination {
        Some(path) => export_to(app.clone(), path).await,
        None => export(app.clone()).await,
    }
}

#[tauri::command]
pub fn frontend_error(
    kind: String,
    message: String,
    stack: Option<String>,
    operation: Option<String>,
) {
    let kind = match kind.as_str() {
        "error" => "error",
        "rejection" => "rejection",
        "react" => "react",
        "query" => "query",
        "mutation" => "mutation",
        _ => "unknown",
    };
    let mut frames = Vec::new();
    for token in stack.as_deref().unwrap_or("").split_whitespace().take(128) {
        let cleaned = token.trim_matches(|c| c == '(' || c == ')');
        let parts: Vec<_> = cleaned.rsplitn(3, ':').collect();
        if parts.len() != 3 {
            continue;
        };
        let (Ok(column), Ok(line)) = (parts[0].parse::<u32>(), parts[1].parse::<u32>()) else {
            continue;
        };
        let file = parts[2].rsplit('/').next().unwrap_or("");
        if file.len() <= 128
            && file.ends_with(".js")
            && file
                .bytes()
                .all(|c| c.is_ascii_alphanumeric() || b"-_.".contains(&c))
        {
            frames.push(json!({"file":file,"line":line,"column":column}));
        }
    }
    let operation = operation
        .filter(|v| v.len() < 80 && v.bytes().all(|c| c.is_ascii_alphanumeric() || c == b'_'));
    event(
        "frontend.failed",
        json!({"kind":kind,"operation":operation,"error_fingerprint":fingerprint(&message),"frames":frames}),
    );
}

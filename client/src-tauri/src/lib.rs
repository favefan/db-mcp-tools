use base64::Engine;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{fs, path::PathBuf, process::Command, sync::Mutex, time::Duration};
use tauri::{
    menu::{CheckMenuItem, Menu, MenuItem},
    tray::TrayIconBuilder,
    Emitter, Manager, State,
};

mod diagnostics;
mod integrations;
mod notifications;
mod preferences;
mod windows;

#[derive(Clone, Default, Serialize, Deserialize)]
struct Profile {
    endpoint: String,
    #[serde(default)]
    data_dir: String,
    #[serde(default)]
    executable: String,
    #[serde(default)]
    server_id: String,
    #[serde(default, skip_serializing)]
    close_to_tray: bool,
}
struct Desktop {
    profile: Mutex<Option<Profile>>,
    preferences: Mutex<preferences::Preferences>,
    notifications: Mutex<notifications::NotificationState>,
    windows: Mutex<windows::WindowState>,
    http: reqwest::Client,
}
fn entry() -> Result<keyring::Entry, String> {
    let service = if cfg!(debug_assertions) {
        "local.dbmcp.tools.dev"
    } else {
        "local.dbmcp.tools"
    };
    keyring::Entry::new(service, "admin").map_err(|e| e.to_string())
}
fn profile_path(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    let dir = app.path().app_config_dir().map_err(|e| e.to_string())?;
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    Ok(dir.join("profile.json"))
}
fn read_profile(state: &Desktop) -> Result<Profile, String> {
    state
        .profile
        .lock()
        .map_err(|e| e.to_string())?
        .clone()
        .ok_or("请先连接 Server".into())
}
fn validate_endpoint(endpoint: &str) -> Result<(), String> {
    let url = reqwest::Url::parse(endpoint).map_err(|_| "Admin 地址无效")?;
    if !matches!(
        url.host_str(),
        Some("127.0.0.1" | "localhost" | "[::1]" | "::1")
    ) || !matches!(url.scheme(), "http" | "https")
        || url.username() != ""
        || url.password().is_some()
        || url.path() != "/"
        || url.query().is_some()
        || url.fragment().is_some()
    {
        return Err("Admin 请使用回环地址；远程 Server 通过 SSH 隧道连接".into());
    }
    Ok(())
}
async fn request(
    state: &Desktop,
    profile: &Profile,
    token: &str,
    method: &str,
    path: &str,
    body: Option<Value>,
) -> Result<Value, String> {
    let trace = diagnostics::id();
    let started = std::time::Instant::now();
    let route = path.split('?').next().unwrap_or("");
    diagnostics::event(
        "admin.start",
        serde_json::json!({"trace_id":trace,"method":method,"route":route,"server_id":profile.server_id}),
    );
    validate_endpoint(&profile.endpoint)?;
    if !path.starts_with("/admin/v1/")
        || path.contains("..")
        || path.contains('#')
        || !matches!(method, "GET" | "POST" | "PUT" | "DELETE")
    {
        return Err("管理请求无效".into());
    }
    let mut req = state
        .http
        .request(
            reqwest::Method::from_bytes(method.as_bytes()).map_err(|e| e.to_string())?,
            format!("{}{}", profile.endpoint.trim_end_matches('/'), path),
        )
        .bearer_auth(token)
        .header("X-DBMCP-Trace-ID", &trace);
    if !profile.server_id.is_empty() {
        req = req.header("X-DBMCP-Server-ID", &profile.server_id);
    }
    if let Some(v) = body {
        req = req.json(&v);
    }
    let response = req.send().await.map_err(|error| {
        let kind=if error.is_timeout(){"timeout"}else if error.is_connect(){"connect"}else{"transport"};
        diagnostics::event("admin.transport_failed",serde_json::json!({"trace_id":trace,"route":route,"error_kind":kind,"duration_ms":started.elapsed().as_millis()}));
        format!("无法连接 Server，请检查地址及运行状态（诊断 ID：{trace}）")
    })?;
    let status = response.status();
    let json_response = response
        .headers()
        .get(reqwest::header::CONTENT_TYPE)
        .and_then(|value| value.to_str().ok())
        .is_some_and(|value| value.starts_with("application/json"));
    if status.as_u16() == 405 || (status.as_u16() == 404 && !json_response) {
        diagnostics::event(
            "admin.unsupported_endpoint",
            serde_json::json!({"trace_id":trace,"route":route,"status":status.as_u16()}),
        );
        return Err(format!(
            "Server 不支持当前操作，请升级 Server 后重试（HTTP {}，诊断 ID：{trace}）",
            status.as_u16()
        ));
    }
    let mut response = response;
    let mut bytes = Vec::new();
    while let Some(chunk) = response.chunk().await.map_err(|_| "Server 响应读取失败")? {
        if bytes.len() + chunk.len() > 20 << 20 {
            diagnostics::event(
                "admin.response_too_large",
                serde_json::json!({"trace_id":trace,"route":route}),
            );
            return Err("Server 响应超过 20 MiB 限制".into());
        };
        bytes.extend_from_slice(&chunk);
    }
    let value: Value = serde_json::from_slice(&bytes).map_err(|_| {
        diagnostics::event(
            "admin.invalid_response",
            serde_json::json!({"trace_id":trace,"route":route,"status":status.as_u16()}),
        );
        format!("Server 响应无效或 API 不兼容（诊断 ID：{trace}）")
    })?;
    diagnostics::event(
        "admin.complete",
        serde_json::json!({"trace_id":trace,"route":route,"status":status.as_u16(),"duration_ms":started.elapsed().as_millis(),"code":value.get("code").and_then(Value::as_str)}),
    );
    if !status.is_success() {
        return Err(value
            .get("message")
            .and_then(Value::as_str)
            .unwrap_or("管理员认证失败或请求被拒绝")
            .to_string());
    }
    Ok(value)
}
#[tauri::command]
fn get_profile(state: State<'_, Desktop>) -> Result<Option<Profile>, String> {
    Ok(state.profile.lock().map_err(|e| e.to_string())?.clone())
}
#[tauri::command]
fn get_preferences(state: State<'_, Desktop>) -> Result<preferences::Preferences, String> {
    Ok(state.preferences.lock().map_err(|e| e.to_string())?.clone())
}
#[tauri::command]
async fn connect_profile(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    mut profile: Profile,
    token: Option<String>,
) -> Result<Value, String> {
    validate_endpoint(&profile.endpoint)?;
    let credential = if let Some(t) = token.filter(|s| !s.is_empty()) {
        t
    } else if !profile.data_dir.is_empty() {
        let b = fs::read(PathBuf::from(&profile.data_dir).join("admin.token"))
            .map_err(|_| "无法读取本机管理员凭据")?;
        base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(b)
    } else {
        entry()?.get_password().map_err(|_| "请输入管理员凭据")?
    };
    let health = request(
        &state,
        &profile,
        &credential,
        "GET",
        "/admin/v1/health",
        None,
    )
    .await?;
    if health["api_version"] != 1 {
        return Err("Server API 版本不兼容，请升级客户端或 Server".into());
    }
    let id = health["server_id"].as_str().ok_or("Server 缺少身份信息")?;
    if !profile.server_id.is_empty() && profile.server_id != id {
        return Err("Server 身份已变化，请重新建立连接档案".into());
    }
    profile.server_id = id.to_string();
    entry()?
        .set_password(&credential)
        .map_err(|e| e.to_string())?;
    let path = profile_path(&app)?;
    let parent = path.parent().ok_or("配置目录无效")?;
    let mut tmp = tempfile::NamedTempFile::new_in(parent).map_err(|e| e.to_string())?;
    serde_json::to_writer(&mut tmp, &profile).map_err(|e| e.to_string())?;
    tmp.as_file().sync_all().map_err(|e| e.to_string())?;
    tmp.persist(&path).map_err(|e| e.to_string())?;
    *state.profile.lock().map_err(|e| e.to_string())? = Some(profile);
    Ok(health)
}
#[tauri::command]
async fn admin_request(
    state: State<'_, Desktop>,
    method: String,
    path: String,
    body: Option<Value>,
) -> Result<Value, String> {
    let profile = read_profile(&state)?;
    let token = entry()?
        .get_password()
        .map_err(|_| "管理员凭据不可用，请重新连接")?;
    request(&state, &profile, &token, &method, &path, body).await
}
#[tauri::command]
async fn local_command(
    executable: String,
    data_dir: String,
    action: String,
) -> Result<Value, String> {
    let trace = diagnostics::id();
    diagnostics::event(
        "local.start",
        serde_json::json!({"trace_id":trace,"action":action,"executable":executable,"data_dir":data_dir}),
    );
    if executable.is_empty() || data_dir.is_empty() {
        return Err("请选择已安装的 dbmcp 程序和数据目录".into());
    }
    let mut args = match action.as_str() {
        "init" => vec!["init"],
        "start" => vec!["runtime", "upgrade", "--start"],
        "stop" => vec!["run", "stop"],
        "restart" => vec!["run", "restart"],
        "status" => vec!["run", "status"],
        "service-status" => vec!["service", "status"],
        "upgrade" => vec!["runtime", "upgrade"],
        "deploy" => vec!["runtime", "deploy"],
        "service-install" => vec!["runtime", "upgrade", "--service", "--start"],
        "service-migrate" => vec!["runtime", "migrate", "--service", "--start"],
        "service-start" => vec!["service", "start"],
        "service-stop" => vec!["service", "stop"],
        "service-restart" => vec!["service", "restart"],
        "service-uninstall" => vec!["service", "uninstall"],
        "rollback-program" => vec!["runtime", "rollback-program"],
        _ => return Err("不支持的本机操作".into()),
    };
    let result = tauri::async_runtime::spawn_blocking(move || {
        let mut native = false;
        if matches!(
            action.as_str(),
            "start" | "stop" | "restart" | "upgrade" | "rollback-program"
        ) {
            let mut probe = Command::new(&executable);
            probe
                .arg("--data-dir")
                .arg(&data_dir)
                .args(["service", "status"]);
            hide(&mut probe);
            let out = probe.output().map_err(|e| e.to_string())?;
            if out.status.success() {
                native = serde_json::from_slice::<Value>(&out.stdout)
                    .ok()
                    .is_some_and(|v| v["installed"] == true);
            }
        }
        if native {
            args = match action.as_str() {
                "start" => vec!["service", "start"],
                "stop" => vec!["service", "stop"],
                "restart" => vec!["service", "restart"],
                _ => args,
            };
        }
        let mut cmd = Command::new(executable);
        cmd.env("DBMCP_TRACE_ID", &trace);
        cmd.arg("--data-dir").arg(data_dir);
        if cfg!(windows) && (native || action.starts_with("service-") && action != "service-status")
        {
            cmd.arg("--elevate");
        }
        cmd.args(args);
        hide(&mut cmd);
        let out = cmd.output().map_err(|e| e.to_string())?;
        diagnostics::event(
            "local.process_complete",
            serde_json::json!({"trace_id":trace,"action":action,"exit_code":out.status.code()}),
        );
        if !out.status.success() {
            return Err(String::from_utf8_lossy(&out.stderr).trim().to_string());
        }
        serde_json::from_slice(&out.stdout).map_err(|_| "dbmcp CLI 响应无效".into())
    })
    .await
    .map_err(|e| e.to_string())?;
    diagnostics::result("local.complete", &result);
    result
}

#[tauri::command]
async fn local_defaults(
    app: tauri::AppHandle,
    executable: Option<String>,
    data_dir: Option<String>,
) -> Result<Value, String> {
    let mut bundled = None;
    if cfg!(feature = "bundled-server") {
        let name = if cfg!(windows) { "dbmcp.exe" } else { "dbmcp" };
        let own = std::env::current_exe().map_err(|e| e.to_string())?;
        let mut candidates = vec![own.parent().ok_or("应用目录无效")?.join(name)];
        if let Ok(resources) = app.path().resource_dir() {
            candidates.push(resources.join(name));
        }
        bundled = candidates.into_iter().find(|p| p.is_file());
    }
    let selected = executable
        .filter(|p| !p.is_empty())
        .map(PathBuf::from)
        .or(bundled.clone());
    tauri::async_runtime::spawn_blocking(move || {
        let Some(path) = selected else {
            return Ok(serde_json::json!({"bundled":false,"platform":std::env::consts::OS}));
        };
        let version = program_version(&path)?;
        let bundled_version = bundled.as_ref().map(|path| program_version(path)).transpose()?;
        let managed = data_dir.is_some_and(|dir| managed_launcher(&path, &PathBuf::from(dir)));
        Ok(serde_json::json!({"executable":path,"managed_runtime":managed,"bundled":bundled.is_some(),"bundled_executable":bundled,"bundled_build":bundled_version,"platform":std::env::consts::OS,"build":version}))
    }).await.map_err(|e| e.to_string())?
}

fn managed_launcher(path: &std::path::Path, data_dir: &std::path::Path) -> bool {
    if !data_dir.join("runtime").join("current.json").is_file() {
        return false;
    }
    let name = if cfg!(windows) { "dbmcp.exe" } else { "dbmcp" };
    let expected = data_dir.join("runtime").join(name);
    match (path.canonicalize(), expected.canonicalize()) {
        (Ok(actual), Ok(expected)) => actual == expected,
        _ => false,
    }
}

#[cfg(test)]
mod local_runtime_tests {
    use super::*;

    #[test]
    fn only_the_selected_data_directorys_managed_launcher_can_auto_upgrade() {
        let root = tempfile::tempdir().unwrap();
        let runtime = root.path().join("runtime");
        fs::create_dir(&runtime).unwrap();
        let name = if cfg!(windows) { "dbmcp.exe" } else { "dbmcp" };
        let launcher = runtime.join(name);
        fs::write(&launcher, "fixture launcher").unwrap();
        assert!(!managed_launcher(&launcher, root.path()));
        fs::write(runtime.join("current.json"), "fixture marker").unwrap();
        assert!(managed_launcher(&launcher, root.path()));
        assert!(!managed_launcher(
            &launcher,
            &root.path().join("other data")
        ));
        let external = root.path().join(name);
        fs::write(&external, "external program").unwrap();
        assert!(!managed_launcher(&external, root.path()));
    }
}

fn program_version(path: &std::path::Path) -> Result<Value, String> {
    if !path.is_absolute() || !path.is_file() {
        return Err("请选择已安装的 dbmcp 程序绝对路径".into());
    }
    let mut cmd = Command::new(path);
    cmd.arg("version");
    hide(&mut cmd);
    let out = cmd.output().map_err(|e| e.to_string())?;
    if !out.status.success() {
        return Err(String::from_utf8_lossy(&out.stderr).trim().to_string());
    }
    let version: Value =
        serde_json::from_slice(&out.stdout).map_err(|_| "所选程序未返回有效的版本信息")?;
    if version["api_version"] != 1 {
        return Err("所选程序 API 不兼容".into());
    }
    Ok(version)
}
#[cfg(windows)]
fn hide(cmd: &mut Command) {
    use std::os::windows::process::CommandExt;
    cmd.creation_flags(0x08000000);
}
#[cfg(not(windows))]
fn hide(_cmd: &mut Command) {}

#[tauri::command]
async fn local_restore(
    executable: String,
    data_dir: String,
    backup: String,
) -> Result<Value, String> {
    if executable.is_empty() || data_dir.is_empty() || backup.is_empty() {
        return Err("请选择本机程序、数据目录和备份目录".into());
    }
    tauri::async_runtime::spawn_blocking(move || {
        let mut cmd = Command::new(executable);
        cmd.arg("--data-dir")
            .arg(data_dir)
            .arg("restore")
            .arg(backup);
        hide(&mut cmd);
        let out = cmd.output().map_err(|e| e.to_string())?;
        if !out.status.success() {
            return Err(String::from_utf8_lossy(&out.stderr).trim().to_string());
        }
        serde_json::from_slice(&out.stdout).map_err(|_| "恢复命令响应无效".into())
    })
    .await
    .map_err(|e| e.to_string())?
}

async fn events(app: tauri::AppHandle) {
    loop {
        tokio::time::sleep(Duration::from_secs(2)).await;
        let state = app.state::<Desktop>();
        let Ok(profile) = read_profile(&state) else {
            continue;
        };
        if validate_endpoint(&profile.endpoint).is_err() {
            continue;
        }
        let Ok(token) = entry().and_then(|e| e.get_password().map_err(|e| e.to_string())) else {
            continue;
        };
        let Ok(mut response) = state
            .http
            .get(format!(
                "{}/admin/v1/events",
                profile.endpoint.trim_end_matches('/')
            ))
            .bearer_auth(&token)
            .send()
            .await
        else {
            continue;
        };
        if !response.status().is_success() {
            continue;
        }
        notifications::changed(&app, &profile, &token).await;
        let mut buffer = String::new();
        while let Ok(Some(chunk)) = response.chunk().await {
            if !read_profile(&state)
                .is_ok_and(|p| p.server_id == profile.server_id && p.endpoint == profile.endpoint)
            {
                break;
            }
            buffer.push_str(&String::from_utf8_lossy(&chunk));
            if buffer.len() > 4096 {
                break;
            }
            while let Some(index) = buffer.find("\n\n") {
                let event = buffer[..index].to_string();
                buffer.drain(..index + 2);
                if event.contains("event: changed") {
                    notifications::changed(&app, &profile, &token).await;
                    let _ = app.emit("server-changed", ());
                }
            }
        }
    }
}

pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _, _| {
            let handle = app.clone();
            let _ = app.run_on_main_thread(move || {
                let result = windows::show_main(&handle);
                diagnostics::result("instance.reopened", &result);
            });
        }))
        .plugin(diagnostics::plugin())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_autostart::Builder::new().build())
        .plugin(tauri_plugin_dialog::init())
        .manage(Desktop {
            profile: Mutex::new(None),
            preferences: Mutex::new(preferences::Preferences::default()),
            notifications: Mutex::new(notifications::NotificationState::default()),
            windows: Mutex::new(windows::WindowState::default()),
            http: reqwest::Client::builder()
                .no_proxy()
                .timeout(Duration::from_secs(320))
                .redirect(reqwest::redirect::Policy::none())
                .build()
                .expect("HTTP client"),
        })
        .setup(|app| {
            let handle = app.handle();
            diagnostics::init(handle).map_err(std::io::Error::other)?;
            if let Ok(path) = profile_path(handle) {
                if let Ok(b) = fs::read(&path) {
                    if let Ok(profile) = serde_json::from_slice::<Profile>(&b) {
                        *app.state::<Desktop>().profile.lock().expect("profile lock") =
                            Some(profile);
                    }
                }
                let legacy = app
                    .state::<Desktop>()
                    .profile
                    .lock()
                    .expect("profile lock")
                    .as_ref()
                    .is_some_and(|p| p.close_to_tray);
                *app.state::<Desktop>()
                    .preferences
                    .lock()
                    .expect("preferences lock") =
                    preferences::load(&path.with_file_name("preferences.json"), legacy)
                        .map_err(std::io::Error::other)?;
            }
            let [(show_id, show_label), (light_id, light_label), (export_id, export_label), (quit_id, quit_label)] = windows::TRAY_ITEMS;
            let show = MenuItem::with_id(app, show_id, show_label, true, None::<&str>)?;
            let lightweight = CheckMenuItem::with_id(app, light_id, light_label, true, false, None::<&str>)?;
            let export = MenuItem::with_id(app, export_id, export_label, true, None::<&str>)?;
            let quit = MenuItem::with_id(app, quit_id, quit_label, true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&show, &lightweight, &export, &quit])?;
            app.state::<Desktop>().windows.lock().expect("window state lock").toggle = Some(lightweight);
            let mut tray = TrayIconBuilder::with_id("main-tray")
                .menu(&menu)
                .tooltip("DB MCP Tools")
                .show_menu_on_left_click(false)
                .on_menu_event(|app, event| windows::tray_action(app, event.id.as_ref()))
                .on_tray_icon_event(|tray, event| {
                    if let tauri::tray::TrayIconEvent::DoubleClick {
                        button: tauri::tray::MouseButton::Left,
                        ..
                    } = event
                    {
                        windows::tray_action(tray.app_handle(), "show")
                    }
                });
            if let Some(icon) = app.default_window_icon() {
                tray = tray.icon(icon.clone());
            }
            tray.build(app)?;
            tauri::async_runtime::spawn(events(app.handle().clone()));
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                windows::close_requested(window);
            }
        })
        .invoke_handler(tauri::generate_handler![
            diagnostics::diagnostics_info,
            diagnostics::diagnostics_export,
            diagnostics::frontend_error,
            notifications::sync_notifications,
            get_profile,
            get_preferences,
            windows::set_desktop_preferences,
            windows::set_lightweight_mode,
            windows::confirm_close,
            connect_profile,
            admin_request,
            local_command,
            local_defaults,
            local_restore,
            integrations::integration_list,
            integrations::integration_preview,
            integrations::integration_export,
            integrations::integration_install,
            integrations::integration_remove_preview,
            integrations::integration_remove
        ])
        .build(tauri::generate_context!())
        .expect("build DB MCP Tools desktop")
        .run(|_, event| {
            if let tauri::RunEvent::ExitRequested {
                code: None, api, ..
            } = event
            {
                api.prevent_exit()
            }
        });
}

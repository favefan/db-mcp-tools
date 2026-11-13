use serde_json::Value;
#[cfg(test)]
use std::fs;
use std::{path::PathBuf, process::Command, sync::Mutex, time::Duration};
use tauri::{
    menu::{CheckMenuItem, Menu, MenuItem},
    tray::TrayIconBuilder,
    Emitter, Manager, State,
};

mod diagnostics;
mod installer;
mod integrations;
mod notifications;
mod operations;
mod preferences;
mod profiles;
use profiles::Profile;
mod windows;

struct Desktop {
    session: Mutex<profiles::Runtime>,
    catalog: Mutex<profiles::Store>,
    operations: operations::Operations,
    events: tokio::sync::watch::Sender<()>,
    exiting: std::sync::atomic::AtomicBool,
    preferences: Mutex<preferences::Preferences>,
    notifications: Mutex<notifications::NotificationState>,
    windows: Mutex<windows::WindowState>,
    http: reqwest::Client,
}
fn entry(id: &str) -> Result<keyring::Entry, String> {
    let service = if cfg!(debug_assertions) {
        "local.dbmcp.tools.dev"
    } else {
        "local.dbmcp.tools"
    };
    keyring::Entry::new(
        service,
        &if id.is_empty() {
            "admin".to_string()
        } else {
            format!("profile:{id}")
        },
    )
    .map_err(|e| e.to_string())
}
fn read_profile(state: &Desktop) -> Result<Profile, String> {
    state
        .session
        .lock()
        .map_err(|e| e.to_string())?
        .profile
        .clone()
        .ok_or("请先选择配置".into())
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
        return Err("Admin 请使用回环地址；远程 MCP Server 通过 SSH 隧道连接".into());
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
        format!("无法连接 MCP Server，请检查地址及运行状态（诊断 ID：{trace}）")
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
            "MCP Server 不支持当前操作，请升级 MCP Server 后重试（HTTP {}，诊断 ID：{trace}）",
            status.as_u16()
        ));
    }
    let mut response = response;
    let mut bytes = Vec::new();
    while let Some(chunk) = response
        .chunk()
        .await
        .map_err(|_| "MCP Server 响应读取失败")?
    {
        if bytes.len() + chunk.len() > 20 << 20 {
            diagnostics::event(
                "admin.response_too_large",
                serde_json::json!({"trace_id":trace,"route":route}),
            );
            return Err("MCP Server 响应超过 20 MiB 限制".into());
        };
        bytes.extend_from_slice(&chunk);
    }
    let value: Value = serde_json::from_slice(&bytes).map_err(|_| {
        diagnostics::event(
            "admin.invalid_response",
            serde_json::json!({"trace_id":trace,"route":route,"status":status.as_u16()}),
        );
        format!("MCP Server 响应无效或 API 不兼容（诊断 ID：{trace}）")
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
    Ok(state
        .session
        .lock()
        .map_err(|e| e.to_string())?
        .profile
        .clone())
}
#[tauri::command]
fn get_preferences(state: State<'_, Desktop>) -> Result<preferences::Preferences, String> {
    Ok(state.preferences.lock().map_err(|e| e.to_string())?.clone())
}
#[tauri::command]
async fn connect_profile(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    profile: Profile,
    token: Option<String>,
    session: profiles::Stamp,
) -> Result<Value, String> {
    if profile.id != session.profile_id {
        return Err("连接目标不是当前配置".into());
    }
    profiles::connect_current(&app, &state, session, token).await
}
#[tauri::command]
async fn admin_request(
    state: State<'_, Desktop>,
    method: String,
    path: String,
    body: Option<Value>,
    session: profiles::Stamp,
) -> Result<Value, String> {
    let _guard = profiles::operation(&state, &session, &format!("管理请求 {method} {path}"))?;
    let (profile, token) = {
        let s = state.session.lock().map_err(|e| e.to_string())?;
        if !s.connected {
            return Err("当前配置尚未连接".into());
        }
        (
            s.profile.clone().ok_or("未选择配置")?,
            s.token.clone().ok_or("管理员凭据不可用")?,
        )
    };
    request(&state, &profile, &token, &method, &path, body).await
}
#[tauri::command]
async fn local_command(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    session: profiles::Stamp,
    executable: String,
    data_dir: String,
    action: String,
) -> Result<Value, String> {
    let _read = if action == "status" {
        Some(profiles::operation(
            &state,
            &session,
            "读取本机 MCP Server 状态",
        )?)
    } else {
        None
    };
    let _write = if action != "status" {
        Some(
            state
                .operations
                .write(&format!("执行本机命令 {action}"))
                .await?,
        )
    } else {
        None
    };
    if !profiles::matches(&state, &session) {
        return Err("会话已改变".into());
    }
    let selected = read_profile(&state)?;
    let alternate_upgrade = if action == "upgrade" && selected.executable != executable {
        let info = local_defaults(
            app.clone(),
            Some(selected.executable.clone()),
            Some(selected.data_dir.clone()),
        )
        .await?;
        info["bundled_executable"].as_str() == Some(executable.as_str())
    } else {
        false
    };
    if selected.mode != profiles::ConnectionMode::Local
        || (selected.executable != executable && !alternate_upgrade)
        || selected.data_dir != data_dir
    {
        return Err("本机操作目标必须与当前配置一致；请先保存配置".into());
    }
    if matches!(action.as_str(), "start" | "restart") {
        profiles::require_managed_program(&selected)?;
    }
    let before = if matches!(action.as_str(), "start" | "stop" | "restart" | "upgrade") {
        Some(profiles::call(&selected, &["run", "status"]).await?)
    } else {
        None
    };
    if let Some(status) = &before {
        profiles::check_identity(&selected, status)?;
        if matches!(action.as_str(), "start" | "restart") && status["running"] == true {
            profiles::require_running_version(status)?;
        }
    }
    if matches!(
        action.as_str(),
        "start" | "stop" | "restart" | "upgrade" | "deploy" | "rollback-program"
    ) {
        let native = profiles::call(&selected, &["service", "status"]).await?;
        if native["installed"] != false {
            return Err("系统服务请使用已有 MCP Server 通过 HTTP 连接".into());
        }
    }
    let prepare_runtime = matches!(action.as_str(), "start" | "restart")
        && !std::path::Path::new(&data_dir)
            .join("runtime/current.json")
            .exists()
        && profiles::cli_at_least(&selected, 1, 0, 11)?;
    if prepare_runtime {
        if action == "restart" {
            profiles::call(&selected, &["run", "stop"]).await?;
        }
        profiles::initialize_local(&selected).await?;
        profiles::call(&selected, &["runtime", "prepare"]).await?;
    }
    let saved_action = action.clone();
    let trace = diagnostics::id();
    diagnostics::event(
        "local.start",
        serde_json::json!({"trace_id":trace,"action":action,"executable":executable,"data_dir":data_dir}),
    );
    if executable.is_empty() || data_dir.is_empty() {
        return Err("请选择已安装的 dbmcp 程序和数据目录".into());
    }
    let args = match action.as_str() {
        "init" => {
            if std::path::Path::new(&data_dir)
                .join("deployment.toml")
                .exists()
            {
                vec!["init"]
            } else if profiles::cli_at_least(&selected, 1, 0, 10)? {
                vec!["init", "--allocate-ports"]
            } else {
                vec!["init"]
            }
        }
        "start" => vec!["run", "start"],
        "stop" => vec!["run", "stop"],
        "restart" => {
            if prepare_runtime {
                vec!["run", "start"]
            } else {
                vec!["run", "restart"]
            }
        }
        "status" => vec!["run", "status"],
        "upgrade" => vec!["runtime", "upgrade"],
        "deploy" => vec!["runtime", "deploy"],
        "rollback-program" => vec!["runtime", "rollback-program"],
        _ => return Err("不支持的本机操作".into()),
    };
    let result = tauri::async_runtime::spawn_blocking(move || {
        let mut cmd = Command::new(executable);
        cmd.env("DBMCP_TRACE_ID", &trace);
        cmd.arg("--data-dir").arg(data_dir);
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
    if let Ok(value) = &result {
        profiles::local_result(&app, &state, &selected, &saved_action, value)?;
    }
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
        let deployed_version = data_dir.as_ref().and_then(|dir| std::fs::read(std::path::Path::new(dir).join("runtime/current.json")).ok()).and_then(|bytes| serde_json::from_slice::<Value>(&bytes).ok()).and_then(|value| value["version"].as_str().map(str::to_owned));
        let managed = data_dir.is_some_and(|dir| managed_launcher(&path, &PathBuf::from(dir)));
        Ok(serde_json::json!({"executable":path,"managed_runtime":managed,"deployed_version":deployed_version,"bundled":bundled.is_some(),"bundled_executable":bundled,"bundled_build":bundled_version,"platform":std::env::consts::OS,"build":version}))
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
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    session: profiles::Stamp,
    executable: String,
    data_dir: String,
    backup: String,
) -> Result<Value, String> {
    let _guard = state.operations.write("恢复本机备份").await?;
    if !profiles::matches(&state, &session) {
        return Err("会话已改变".into());
    }
    let p = read_profile(&state)?;
    if p.mode != profiles::ConnectionMode::Local
        || p.executable != executable
        || p.data_dir != data_dir
    {
        return Err("恢复目标必须为当前本机配置".into());
    }
    if executable.is_empty() || data_dir.is_empty() || backup.is_empty() {
        return Err("请选择本机程序、数据目录和备份目录".into());
    }
    let result = tauri::async_runtime::spawn_blocking(move || {
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
    .map_err(|e| e.to_string())??;
    profiles::local_result(&app, &state, &p, "restore", &result)?;
    Ok(result)
}

async fn events(app: tauri::AppHandle) {
    let mut changes = app.state::<Desktop>().events.subscribe();
    loop {
        tokio::select! {_=tokio::time::sleep(Duration::from_secs(2))=>{},_=changes.changed()=>{}}
        let snapshot = {
            let state = app.state::<Desktop>();
            let s = state.session.lock().expect("session lock");
            if !s.connected || s.busy {
                None
            } else {
                s.profile
                    .clone()
                    .zip(s.token.clone())
                    .map(|(p, t)| (p, t, s.generation))
            }
        };
        let Some((profile, token, generation)) = snapshot else {
            continue;
        };
        let state = app.state::<Desktop>();
        let stamp = profiles::Stamp {
            profile_id: profile.id.clone(),
            generation,
        };
        let response = tokio::select! {_=changes.changed()=>continue,r=state.http.get(format!("{}/admin/v1/events",profile.endpoint.trim_end_matches('/'))).bearer_auth(&token).header("X-DBMCP-Server-ID",&profile.server_id).send()=>r};
        let Ok(mut response) = response else { continue };
        if !response.status().is_success() {
            continue;
        }
        notifications::changed(&app, &profile, &token, &stamp).await;
        let mut buffer = String::new();
        loop {
            let chunk = tokio::select! {_=changes.changed()=>break,c=response.chunk()=>c};
            let Ok(Some(chunk)) = chunk else { break };
            if !profiles::matches(&state, &stamp) {
                break;
            }
            buffer.push_str(&String::from_utf8_lossy(&chunk));
            if buffer.len() > 4096 {
                break;
            }
            while let Some(index) = buffer.find("\n\n") {
                let event = buffer[..index].to_string();
                buffer.drain(..index + 2);
                if event.contains("event: changed") && profiles::matches(&state, &stamp) {
                    notifications::changed(&app, &profile, &token, &stamp).await;
                    if profiles::matches(&state, &stamp) {
                        let _ = app.emit("server-changed", &stamp);
                    }
                }
            }
        }
    }
}

pub fn run() {
    if let Some(code) = installer::execute() {
        std::process::exit(code);
    }
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
            session: Mutex::new(profiles::Runtime::default()),
            catalog: Mutex::new(profiles::Store::default()),
            operations: crate::operations::Operations::default(),
            events: tokio::sync::watch::channel(()).0,
            exiting: std::sync::atomic::AtomicBool::new(false),
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
            if let Err(error)=profiles::initialize(handle){let state=app.state::<Desktop>();state.session.lock().expect("session lock").error=Some(error);}
            let [(show_id, show_label), (light_id, light_label), (export_id, export_label), (about_id, about_label), (quit_id, quit_label)] = windows::TRAY_ITEMS;
            let show = MenuItem::with_id(app, show_id, show_label, true, None::<&str>)?;
            let lightweight = CheckMenuItem::with_id(app, light_id, light_label, true, false, None::<&str>)?;
            let export = MenuItem::with_id(app, export_id, export_label, true, None::<&str>)?;
            let about = MenuItem::with_id(app, about_id, about_label, true, None::<&str>)?;
            let quit = MenuItem::with_id(app, quit_id, quit_label, true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&show, &lightweight, &export, &about, &quit])?;
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
            tauri::async_runtime::spawn(profiles::boot(app.handle().clone()));
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
            profiles::get_session,
            profiles::save_profile,
            profiles::delete_profile,
            profiles::switch_profile,
            profiles::disconnect_session,
            profiles::exit_session,
            get_preferences,
            windows::set_desktop_preferences,
            windows::take_about_request,
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
        .run(|app, event| {
            if let tauri::RunEvent::ExitRequested {api,code,..}=event {
                if code.is_none(){api.prevent_exit();return;}
                if !app.state::<Desktop>().exiting.load(std::sync::atomic::Ordering::SeqCst){
                    api.prevent_exit();let app=app.clone();tauri::async_runtime::spawn(async move{if let Err(error)=profiles::exit(app.clone(),false).await{windows::show_exit_error(&app,&error);}});
                }
            }
        });
}

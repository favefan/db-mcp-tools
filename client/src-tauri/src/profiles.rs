use crate::{entry, hide, preferences, request, validate_endpoint, Desktop};
use base64::Engine;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::{
    fs,
    path::{Path, PathBuf},
    process::Command,
};
use tauri::{Emitter, Manager, State};

#[derive(Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum ConnectionMode {
    Local,
    #[default]
    Existing,
}
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct Profile {
    pub id: String,
    pub name: String,
    pub mode: ConnectionMode,
    pub endpoint: String,
    pub data_dir: String,
    pub executable: String,
    pub server_id: String,
    pub import_legacy_ui: bool,
    pub preferences: preferences::Preferences,
    #[serde(skip_serializing)]
    pub close_to_tray: bool,
}
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct Store {
    pub version: u32,
    pub active_id: Option<String>,
    pub profiles: Vec<Profile>,
}
#[derive(Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Stamp {
    pub profile_id: String,
    pub generation: u64,
}
#[derive(Default)]
pub struct Runtime {
    pub profile: Option<Profile>,
    pub token: Option<String>,
    pub generation: u64,
    pub connected: bool,
    pub busy: bool,
    pub error: Option<String>,
    pub owned: Option<Owned>,
}
#[derive(Clone)]
pub struct Owned {
    pub profile: Profile,
    pub instance_id: String,
}
impl Owned {
    fn from_status(profile: Profile, status: &Value) -> Result<Self, String> {
        let instance_id = status["instance_id"].as_str().unwrap_or_default().into();
        if status["version"]
            .as_str()
            .and_then(|s| semver::Version::parse(s).ok())
            .is_some_and(|v| v >= semver::Version::new(1, 0, 10))
            && status["instance_id"].as_str().is_none_or(str::is_empty)
        {
            return Err("MCP Server 响应缺少进程身份，请检查运行状态".into());
        }
        if status["mode"] != "background" {
            return Err("前台进程或系统服务请使用已有 MCP Server 通过 HTTP 连接".into());
        }
        Ok(Self {
            profile,
            instance_id,
        })
    }
}
#[derive(Serialize)]
pub struct View {
    pub current: Option<Profile>,
    pub profiles: Vec<Profile>,
    pub generation: u64,
    pub connected: bool,
    pub owned_server: bool,
    pub busy: bool,
    pub error: Option<String>,
    pub preferences: preferences::Preferences,
}
fn directory(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    let p = app.path().app_config_dir().map_err(|e| e.to_string())?;
    fs::create_dir_all(&p).map_err(|e| e.to_string())?;
    Ok(p)
}
pub fn save(path: &Path, store: &Store) -> Result<(), String> {
    let mut temp = tempfile::NamedTempFile::new_in(path.parent().ok_or("Profile 目录无效")?)
        .map_err(|e| e.to_string())?;
    serde_json::to_writer_pretty(&mut temp, store).map_err(|e| e.to_string())?;
    temp.as_file().sync_all().map_err(|e| e.to_string())?;
    temp.persist(path).map_err(|e| e.to_string())?;
    Ok(())
}
fn save_with_token(
    path: &Path,
    store: &Store,
    id: &str,
    token: Option<&str>,
) -> Result<(), String> {
    let Some(token) = token.filter(|t| !t.is_empty()) else {
        return save(path, store);
    };
    let credential = entry(id)?;
    let previous = match credential.get_password() {
        Ok(value) => Some(value),
        Err(keyring::Error::NoEntry) => None,
        Err(e) => return Err(e.to_string()),
    };
    credential.set_password(token).map_err(|e| e.to_string())?;
    if let Err(error) = save(path, store) {
        let rollback = if let Some(previous) = previous {
            credential.set_password(&previous)
        } else {
            credential.delete_credential()
        };
        return Err(match rollback {
            Ok(()) => error,
            Err(e) => format!("{error}；凭据恢复失败：{e}"),
        });
    }
    Ok(())
}
pub fn load(dir: &Path) -> Result<Store, String> {
    let path = dir.join("profiles.json");
    if path.exists() {
        let store: Store = serde_json::from_slice(&fs::read(path).map_err(|e| e.to_string())?)
            .map_err(|e| format!("Profile 配置损坏：{e}"))?;
        if store.version != 1 {
            return Err("不支持的 Profile 配置版本".into());
        }
        let mut ids = std::collections::HashSet::new();
        for p in &store.profiles {
            validate(p)?;
            if !ids.insert(&p.id) {
                return Err("Profile ID 重复".into());
            }
        }
        if store.active_id.as_ref().is_some_and(|id| !ids.contains(id)) {
            return Err("当前 Profile 不存在".into());
        }
        return Ok(store);
    }
    let mut store = Store {
        version: 1,
        ..Store::default()
    };
    let old = dir.join("profile.json");
    if old.exists() {
        let mut p: Profile = serde_json::from_slice(&fs::read(old).map_err(|e| e.to_string())?)
            .map_err(|e| format!("旧连接档案损坏：{e}"))?;
        p.id = "legacy".into();
        p.name = "default".into();
        p.mode = if p.data_dir.is_empty() {
            ConnectionMode::Existing
        } else {
            ConnectionMode::Local
        };
        p.import_legacy_ui = true;
        p.preferences = preferences::load(&dir.join("preferences.json"), p.close_to_tray)?;
        validate(&p)?;
        store.active_id = Some(p.id.clone());
        store.profiles.push(p);
    } else {
        let p = Profile {
            id: uuid::Uuid::new_v4().to_string(),
            name: "default".into(),
            endpoint: "http://127.0.0.1:19876".into(),
            ..Default::default()
        };
        store.active_id = Some(p.id.clone());
        store.profiles.push(p);
    }
    Ok(store)
}
pub fn validate(p: &Profile) -> Result<(), String> {
    if p.id.is_empty()
        || p.id.len() > 64
        || !p.id.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-')
    {
        return Err("Profile ID 无效".into());
    }
    if p.name.trim().is_empty() || p.name.len() > 160 {
        return Err("请输入配置名称（不超过 160 字节）".into());
    }
    validate_endpoint(&p.endpoint)?;
    if !matches!(p.preferences.theme.as_str(), "system" | "light" | "dark") {
        return Err("主题无效".into());
    }
    match p.mode {
        ConnectionMode::Existing if !p.data_dir.is_empty() || !p.executable.is_empty() => {
            return Err("已有 MCP Server 不能携带本机托管配置".into())
        }
        ConnectionMode::Local
            if !Path::new(&p.data_dir).is_absolute()
                || (!p.executable.is_empty() && !Path::new(&p.executable).is_absolute()) =>
        {
            return Err("本机程序和数据目录须使用绝对路径".into())
        }
        _ => {}
    }
    Ok(())
}
pub fn matches(state: &Desktop, stamp: &Stamp) -> bool {
    state.session.lock().is_ok_and(|s| {
        !s.busy
            && s.generation == stamp.generation
            && s.profile.as_ref().is_some_and(|p| p.id == stamp.profile_id)
    })
}
pub fn operation<'a>(
    state: &'a Desktop,
    session: &Stamp,
) -> Result<tokio::sync::RwLockReadGuard<'a, ()>, String> {
    let guard = state
        .operations
        .try_read()
        .map_err(|_| "正在切换配置或退出，请稍候")?;
    if !matches(state, session) {
        return Err("会话已改变，请重新打开当前配置的操作".into());
    }
    Ok(guard)
}
pub fn view(state: &Desktop) -> Result<View, String> {
    let s = state.session.lock().map_err(|e| e.to_string())?;
    let profiles = state
        .catalog
        .lock()
        .map_err(|e| e.to_string())?
        .profiles
        .clone();
    Ok(View {
        current: s.profile.clone(),
        profiles,
        generation: s.generation,
        connected: s.connected,
        owned_server: s.owned.is_some(),
        busy: s.busy,
        error: s.error.clone(),
        preferences: state.preferences.lock().map_err(|e| e.to_string())?.clone(),
    })
}
pub fn changed(app: &tauri::AppHandle) {
    let _ = app.emit("session-changed", ());
}
pub fn initialize(app: &tauri::AppHandle) -> Result<(), String> {
    let dir = directory(app)?;
    let store = load(&dir)?;
    if !dir.join("profiles.json").exists() {
        if store.profiles.iter().any(|p| p.id == "legacy") {
            match entry("")?.get_password() {
                Ok(token) => entry("legacy")?
                    .set_password(&token)
                    .map_err(|e| e.to_string())?,
                Err(keyring::Error::NoEntry) => {}
                Err(e) => return Err(format!("旧凭据迁移失败：{e}")),
            }
        }
        save(&dir.join("profiles.json"), &store)?;
    }
    let state = app.state::<Desktop>();
    let p = store
        .active_id
        .as_ref()
        .and_then(|id| store.profiles.iter().find(|p| &p.id == id))
        .cloned();
    *state.preferences.lock().map_err(|e| e.to_string())? = p
        .as_ref()
        .map(|p| p.preferences.clone())
        .unwrap_or_default();
    let mut runtime = state.session.lock().map_err(|e| e.to_string())?;
    runtime.profile = p;
    runtime.busy = runtime.profile.is_some();
    *state.catalog.lock().map_err(|e| e.to_string())? = store;
    Ok(())
}
pub async fn command(p: &Profile, args: Vec<String>) -> Result<Value, String> {
    let p = p.clone();
    tauri::async_runtime::spawn_blocking(move || {
        if p.mode != ConnectionMode::Local || p.executable.is_empty() {
            return Err("请为当前本机配置选择 dbmcp 程序".into());
        }
        let mut cmd = Command::new(&p.executable);
        cmd.arg("--data-dir").arg(&p.data_dir).args(args);
        hide(&mut cmd);
        let output = cmd.output().map_err(|e| e.to_string())?;
        if !output.status.success() {
            return Err(String::from_utf8_lossy(&output.stderr).trim().to_string());
        }
        serde_json::from_slice(&output.stdout).map_err(|_| "dbmcp CLI 响应无效".into())
    })
    .await
    .map_err(|e| e.to_string())?
}
pub async fn call(p: &Profile, args: &[&str]) -> Result<Value, String> {
    command(p, args.iter().map(|s| s.to_string()).collect()).await
}
async fn stop_owned(state: &Desktop) -> Result<(), String> {
    let owned = state
        .session
        .lock()
        .map_err(|e| e.to_string())?
        .owned
        .clone();
    if let Some(owned) = owned {
        let p = owned.profile.clone();
        let status = call(&p, &["run", "status"]).await?;
        if status["running"] == true {
            stop_local(&p, &status).await?;
        } else if Path::new(&p.data_dir).join("runtime.json").exists() {
            return Err("无法确认本次启动的 MCP Server 已停止，请检查本机状态后重试".into());
        }
        state.session.lock().map_err(|e| e.to_string())?.owned = None;
    }
    Ok(())
}
async fn stop_local(p: &Profile, status: &Value) -> Result<(), String> {
    check_identity(p, status)?;
    let current = Owned::from_status(p.clone(), status)?;
    if !current.instance_id.is_empty() && cli_at_least(p, 1, 0, 11)? {
        call(p, &["run", "stop", "--instance-id", &current.instance_id]).await?;
    } else {
        call(p, &["run", "stop"]).await?;
    }
    Ok(())
}
async fn start_local(p: &mut Profile) -> Result<Value, String> {
    require_managed_program(p)?;
    if !Path::new(&p.data_dir).join("deployment.toml").exists() {
        let initialized = initialize_local(p).await?;
        if let Some(admin) = initialized["admin"].as_str() {
            p.endpoint = format!("http://{admin}");
        }
    }
    let mut status = if cli_at_least(p, 1, 0, 10)? {
        let info = call(p, &["runtime", "info"]).await?;
        p.endpoint = format!(
            "http://{}",
            info["admin"].as_str().ok_or("缺少本机管理地址")?
        );
        info["status"].clone()
    } else {
        call(p, &["run", "status"]).await?
    };
    check_identity(p, &status)?;
    if status["running"] == true {
        require_running_version(&status)?;
    }
    if status["running"] != true {
        let native = call(p, &["service", "status"]).await?;
        match native["installed"].as_bool() {
            Some(true) => {
                return Err("该目录已注册为系统服务，请使用已有 MCP Server 通过 HTTP 连接".into());
            }
            Some(false) => {
                if !Path::new(&p.data_dir).join("runtime/current.json").exists()
                    && cli_at_least(p, 1, 0, 11)?
                {
                    call(p, &["runtime", "prepare"]).await?;
                }
                call(p, &["run", "start"]).await?;
            }
            None => return Err("无法确认本机系统服务状态".into()),
        }
        status = call(p, &["run", "status"]).await?;
    }
    require_running_version(&status)?;
    Ok(status)
}
pub async fn reconnect(
    app: &tauri::AppHandle,
    mut p: Profile,
    token: Option<String>,
) -> Result<Value, String> {
    let state = app.state::<Desktop>();
    if p.mode == ConnectionMode::Local {
        let status = start_local(&mut p).await?;
        let mut managed = p.clone();
        managed.server_id = status["server_id"]
            .as_str()
            .ok_or("MCP Server 缺少身份")?
            .into();
        state.session.lock().map_err(|e| e.to_string())?.owned =
            Some(Owned::from_status(managed, &status)?);
        check_identity(&p, &status)?;
    }
    let credential = if let Some(token) = token.filter(|t| !t.is_empty()) {
        token
    } else if p.mode == ConnectionMode::Local {
        let bytes = fs::read(Path::new(&p.data_dir).join("admin.token"))
            .map_err(|_| "无法读取本机管理员凭据")?;
        base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(bytes)
    } else {
        entry(&p.id)?
            .get_password()
            .map_err(|_| "请为此配置填写管理员凭据")?
    };
    let health = request(&state, &p, &credential, "GET", "/admin/v1/health", None).await?;
    if health["api_version"] != 1 {
        return Err("MCP Server API 版本不兼容".into());
    }
    let id = health["server_id"].as_str().ok_or("MCP Server 缺少身份")?;
    if !p.server_id.is_empty() && p.server_id != id {
        return Err("MCP Server 身份发生变化，未切换连接".into());
    }
    p.server_id = id.into();
    let mut next = state.catalog.lock().map_err(|e| e.to_string())?.clone();
    let item = next
        .profiles
        .iter_mut()
        .find(|x| x.id == p.id)
        .ok_or("Profile 已不存在")?;
    *item = p.clone();
    next.active_id = Some(p.id.clone());
    save_with_token(
        &directory(app)?.join("profiles.json"),
        &next,
        &p.id,
        Some(&credential),
    )?;
    *state.catalog.lock().map_err(|e| e.to_string())? = next;
    *state.preferences.lock().map_err(|e| e.to_string())? = p.preferences.clone();
    let mut s = state.session.lock().map_err(|e| e.to_string())?;
    s.profile = Some(p);
    s.token = Some(credential);
    s.connected = true;
    Ok(health)
}
pub fn require_running_version(status: &Value) -> Result<(), String> {
    if status["version"]
        .as_str()
        .and_then(|s| semver::Version::parse(s).ok())
        .is_none_or(|v| v < semver::Version::new(1, 0, 2))
    {
        return Err("当前操作需要 MCP Server 1.0.2 或更新版本的 API".into());
    }
    Ok(())
}
pub fn require_managed_program(p: &Profile) -> Result<(), String> {
    require_running_version(&crate::program_version(Path::new(&p.executable))?)
}
pub fn cli_at_least(p: &Profile, major: u64, minor: u64, patch: u64) -> Result<bool, String> {
    let build = crate::program_version(Path::new(&p.executable))?;
    let version = build["version"].as_str().ok_or("MCP Server 缺少版本")?;
    Ok(semver::Version::parse(version).map_err(|e| e.to_string())?
        >= semver::Version::new(major, minor, patch))
}
pub async fn initialize_local(p: &Profile) -> Result<Value, String> {
    if cli_at_least(p, 1, 0, 10)? {
        call(p, &["init", "--allocate-ports"]).await
    } else {
        call(p, &["init"]).await
    }
}
fn begin(app: &tauri::AppHandle) -> Result<(), String> {
    let state = app.state::<Desktop>();
    let mut s = state.session.lock().map_err(|e| e.to_string())?;
    s.busy = true;
    s.generation = s.generation.wrapping_add(1);
    s.connected = false;
    s.token = None;
    s.error = None;
    drop(s);
    *state.notifications.lock().map_err(|e| e.to_string())? =
        crate::notifications::NotificationState::default();
    crate::windows::profile_changed(app);
    let _ = state.events.send(());
    changed(app);
    Ok(())
}
fn finish(app: &tauri::AppHandle, result: &Result<(), String>) {
    let state = app.state::<Desktop>();
    if let Ok(mut s) = state.session.lock() {
        s.busy = false;
        s.generation = s.generation.wrapping_add(1);
        s.error = result.as_ref().err().cloned()
    }
    if let Ok(mut n) = state.notifications.lock() {
        n.enabled = state.preferences.lock().is_ok_and(|p| p.notifications)
    }
    changed(app);
}
#[tauri::command]
pub fn get_session(state: State<'_, Desktop>) -> Result<View, String> {
    view(&state)
}
#[tauri::command]
pub async fn switch_profile(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    id: String,
    connect_now: bool,
) -> Result<View, String> {
    let _guard = state
        .operations
        .try_write()
        .map_err(|_| "有操作正在进行，请完成后再切换配置")?;
    let p = state
        .catalog
        .lock()
        .map_err(|e| e.to_string())?
        .profiles
        .iter()
        .find(|p| p.id == id)
        .cloned()
        .ok_or("配置不存在")?;
    validate(&p)?;
    let target_id = p.id.clone();
    begin(&app)?;
    let mut released_previous = false;
    let result: Result<(), String> = async {
        stop_owned(&state).await?;
        released_previous = true;
        if connect_now || p.mode == ConnectionMode::Local {
            reconnect(&app, p, None).await?;
        } else {
            let mut next = state.catalog.lock().map_err(|e| e.to_string())?.clone();
            next.active_id = Some(p.id.clone());
            save(&directory(&app)?.join("profiles.json"), &next)?;
            *state.catalog.lock().map_err(|e| e.to_string())? = next;
            *state.preferences.lock().map_err(|e| e.to_string())? = p.preferences.clone();
            state.session.lock().map_err(|e| e.to_string())?.profile = Some(p);
        }
        Ok(())
    }
    .await;
    if released_previous
        && result.is_err()
        && state
            .session
            .lock()
            .is_ok_and(|s| s.owned.as_ref().is_some_and(|p| p.profile.id == target_id))
    {
        if let Err(cleanup) = stop_owned(&state).await {
            finish(&app, &Err(cleanup.clone()));
            return Err(cleanup);
        }
    }
    finish(&app, &result);
    result?;
    view(&state)
}
#[tauri::command]
pub async fn disconnect_session(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    session: Stamp,
) -> Result<View, String> {
    let _guard = state
        .operations
        .try_write()
        .map_err(|_| "有操作正在进行，请完成后再断开")?;
    if !matches(&state, &session) {
        return Err("会话已改变".into());
    }
    begin(&app)?;
    let result = stop_owned(&state).await;
    finish(&app, &result);
    result?;
    view(&state)
}
#[tauri::command]
pub async fn save_profile(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    mut profile: Profile,
    token: Option<String>,
) -> Result<View, String> {
    let _guard = state
        .operations
        .try_write()
        .map_err(|_| "有操作正在进行，请稍候")?;
    let fresh = profile.id.is_empty();
    if fresh {
        profile.id = uuid::Uuid::new_v4().to_string()
    }
    profile.name = profile.name.trim().into();
    if profile.mode == ConnectionMode::Existing {
        profile.data_dir.clear();
        profile.executable.clear();
    } else if profile.data_dir.is_empty() {
        profile.data_dir = app
            .path()
            .app_local_data_dir()
            .map_err(|e| e.to_string())?
            .join("servers")
            .join(&profile.id)
            .to_string_lossy()
            .into()
    }
    validate(&profile)?;
    let active_connected = {
        let current = state.session.lock().map_err(|e| e.to_string())?;
        current.profile.as_ref().is_some_and(|p| p.id == profile.id)
            && (current.connected || current.owned.is_some())
    };
    if active_connected {
        return Err("请先断开当前配置，再编辑配置".into());
    }
    let mut next = state.catalog.lock().map_err(|e| e.to_string())?.clone();
    if next.version != 1 {
        return Err("配置未成功加载，请先修复配置文件".into());
    }
    if !fresh && !next.profiles.iter().any(|p| p.id == profile.id) {
        return Err("配置不存在".into());
    }
    if next.profiles.iter().any(|p| {
        p.id != profile.id
            && profile.mode == ConnectionMode::Local
            && p.mode == ConnectionMode::Local
            && same_path(&p.data_dir, &profile.data_dir)
    }) {
        return Err("此数据目录已属于另一个本机配置".into());
    }
    if let Some(previous) = next.profiles.iter().find(|p| p.id == profile.id) {
        if profile.mode == ConnectionMode::Existing
            && (previous.endpoint != profile.endpoint || previous.mode != profile.mode)
            && token.as_ref().is_none_or(|t| t.is_empty())
        {
            return Err("连接目标改变后，请重新填写管理员凭据".into());
        }
        profile.server_id = if previous.endpoint == profile.endpoint
            && previous.mode == profile.mode
            && same_path(&previous.data_dir, &profile.data_dir)
        {
            previous.server_id.clone()
        } else {
            String::new()
        };
        profile.import_legacy_ui = previous.import_legacy_ui;
    } else {
        profile.server_id.clear();
        profile.import_legacy_ui = false;
    }
    if let Some(p) = next.profiles.iter_mut().find(|p| p.id == profile.id) {
        *p = profile.clone()
    } else {
        next.profiles.push(profile.clone())
    }
    let select = next.active_id.is_none() || next.active_id.as_ref() == Some(&profile.id);
    if select {
        next.active_id = Some(profile.id.clone())
    }
    save_with_token(
        &directory(&app)?.join("profiles.json"),
        &next,
        &profile.id,
        token.as_deref(),
    )?;
    *state.catalog.lock().map_err(|e| e.to_string())? = next;
    if select {
        let mut s = state.session.lock().map_err(|e| e.to_string())?;
        s.profile = Some(profile.clone());
        s.generation = s.generation.wrapping_add(1);
        s.connected = false;
        s.error = None;
        s.token = None;
        *state.preferences.lock().map_err(|e| e.to_string())? = profile.preferences;
    }
    changed(&app);
    view(&state)
}
fn same_path(a: &str, b: &str) -> bool {
    let path = |s: &str| {
        fs::canonicalize(s)
            .unwrap_or_else(|_| PathBuf::from(s))
            .to_string_lossy()
            .trim_end_matches(['/', '\\'])
            .to_string()
    };
    if cfg!(windows) {
        path(a).eq_ignore_ascii_case(&path(b))
    } else {
        path(a) == path(b)
    }
}
#[tauri::command]
pub fn delete_profile(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    id: String,
) -> Result<View, String> {
    let _guard = state
        .operations
        .try_write()
        .map_err(|_| "有操作正在进行，请稍候")?;
    let s = state.session.lock().map_err(|e| e.to_string())?;
    if s.profile.as_ref().is_some_and(|p| p.id == id) && (s.connected || s.owned.is_some()) {
        return Err("请先断开此配置".into());
    }
    drop(s);
    let mut next = state.catalog.lock().map_err(|e| e.to_string())?.clone();
    if !next.profiles.iter().any(|p| p.id == id) {
        return Err("配置不存在".into());
    }
    next.profiles.retain(|p| p.id != id);
    let active = next.active_id.as_ref() == Some(&id);
    if active {
        next.active_id = None
    }
    let path = directory(&app)?.join("profiles.json");
    let credential = entry(&id)?;
    let previous = match credential.get_password() {
        Ok(token) => Some(token),
        Err(keyring::Error::NoEntry) => None,
        Err(e) => return Err(e.to_string()),
    };
    match credential.delete_credential() {
        Ok(()) | Err(keyring::Error::NoEntry) => {}
        Err(e) => return Err(e.to_string()),
    }
    if let Err(error) = save(&path, &next) {
        if let Some(token) = previous {
            credential
                .set_password(&token)
                .map_err(|e| format!("{error}；凭据恢复失败：{e}"))?;
        }
        return Err(error);
    }
    *state.catalog.lock().map_err(|e| e.to_string())? = next;
    if active {
        let mut s = state.session.lock().map_err(|e| e.to_string())?;
        s.profile = None;
        s.token = None;
        s.generation += 1;
        *state.preferences.lock().map_err(|e| e.to_string())? = preferences::Preferences::default();
    }
    changed(&app);
    view(&state)
}
pub fn persist_preferences(
    app: &tauri::AppHandle,
    next: preferences::Preferences,
) -> Result<(), String> {
    let state = app.state::<Desktop>();
    let id = state
        .session
        .lock()
        .map_err(|e| e.to_string())?
        .profile
        .as_ref()
        .ok_or("请先创建配置")?
        .id
        .clone();
    let mut stored = state.catalog.lock().map_err(|e| e.to_string())?;
    let mut catalog = stored.clone();
    let p = catalog
        .profiles
        .iter_mut()
        .find(|p| p.id == id)
        .ok_or("配置不存在")?;
    p.preferences = next.clone();
    p.import_legacy_ui = false;
    save(&directory(app)?.join("profiles.json"), &catalog)?;
    *stored = catalog;
    drop(stored);
    if let Some(p) = state
        .session
        .lock()
        .map_err(|e| e.to_string())?
        .profile
        .as_mut()
    {
        p.preferences = next.clone();
        p.import_legacy_ui = false;
    }
    *state.preferences.lock().map_err(|e| e.to_string())? = next;
    changed(app);
    Ok(())
}
pub async fn boot(app: tauri::AppHandle) {
    let id = app
        .state::<Desktop>()
        .session
        .lock()
        .ok()
        .and_then(|s| s.profile.as_ref().map(|p| p.id.clone()));
    if let Some(id) = id {
        let state = app.state::<Desktop>();
        if let Ok(mut s) = state.session.lock() {
            s.busy = false;
        }
        let profile = crate::read_profile(&state);
        if profile.as_ref().is_ok_and(|p| {
            (p.mode == ConnectionMode::Existing
                && entry(&p.id).is_ok_and(|credential| {
                    matches!(credential.get_password(), Err(keyring::Error::NoEntry))
                }))
                || (p.mode == ConnectionMode::Local && p.executable.is_empty())
        }) {
            changed(&app);
            return;
        }
        let result = switch_profile(app.clone(), state, id, true).await;
        if let Err(error) = result {
            crate::diagnostics::event("session.start_failed", json!({"message":error}));
        }
    }
}
pub async fn exit(app: tauri::AppHandle, leave_running: bool) -> Result<(), String> {
    let state = app.state::<Desktop>();
    let _guard = state
        .operations
        .try_write()
        .map_err(|_| "有操作正在进行，请完成后退出")?;
    begin(&app)?;
    let result = if leave_running {
        Ok(())
    } else {
        stop_owned(&state).await
    };
    finish(&app, &result);
    result?;
    state
        .exiting
        .store(true, std::sync::atomic::Ordering::SeqCst);
    app.exit(0);
    Ok(())
}

pub async fn connect_current(
    app: &tauri::AppHandle,
    state: &Desktop,
    session: Stamp,
    token: Option<String>,
) -> Result<Value, String> {
    let _guard = state
        .operations
        .try_write()
        .map_err(|_| "有操作正在进行，请稍候")?;
    if !matches(state, &session) {
        return Err("会话已改变".into());
    }
    let p = crate::read_profile(state)?;
    begin(app)?;
    let result = reconnect(app, p, token).await;
    finish(app, &result.as_ref().map(|_| ()).map_err(Clone::clone));
    result
}
pub fn local_result(
    app: &tauri::AppHandle,
    state: &Desktop,
    p: &Profile,
    action: &str,
    value: &Value,
) -> Result<(), String> {
    if matches!(action, "status") {
        return Ok(());
    }
    let mut next = p.clone();
    if let Some(path) = value["launcher"].as_str() {
        next.executable = path.into()
    }
    if let Some(admin) = value["admin"].as_str() {
        next.endpoint = format!("http://{admin}")
    }
    if let Some(dir) = value["data_dir"].as_str() {
        next.data_dir = dir.into();
    }
    let status = if value.get("status").is_some() {
        &value["status"]
    } else {
        value
    };
    if status["running"] == true {
        if let Some(id) = status["server_id"].as_str() {
            if !p.server_id.is_empty() && p.server_id != id {
                return Err("本机进程身份与当前配置不一致".into());
            }
            next.server_id = id.into()
        }
    }
    let mut catalog = state.catalog.lock().map_err(|e| e.to_string())?.clone();
    if let Some(item) = catalog.profiles.iter_mut().find(|i| i.id == p.id) {
        *item = next.clone()
    }
    save(&directory(app)?.join("profiles.json"), &catalog)?;
    *state.catalog.lock().map_err(|e| e.to_string())? = catalog;
    let mut runtime = state.session.lock().map_err(|e| e.to_string())?;
    runtime.profile = Some(next.clone());
    if next.mode == ConnectionMode::Local
        && matches!(action, "start" | "restart" | "upgrade")
        && status["running"] == true
    {
        runtime.owned = Some(Owned::from_status(next, status)?);
    }
    if matches!(action, "stop" | "restore") {
        runtime.connected = false;
        runtime.token = None;
        runtime.owned = None;
        let _ = state.events.send(());
    }
    drop(runtime);
    changed(app);
    Ok(())
}
pub fn check_identity(p: &Profile, status: &Value) -> Result<(), String> {
    if status["running"] == true && !p.server_id.is_empty() && status["server_id"] != p.server_id {
        return Err("本机 MCP Server 身份与配置不一致".into());
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn profile(id: &str) -> Profile {
        Profile {
            id: id.into(),
            name: id.into(),
            endpoint: "http://127.0.0.1:19876".into(),
            ..Default::default()
        }
    }
    #[test]
    #[ignore = "requires DBMCP_TEST_LEGACY_SERVER and DBMCP_TEST_SERVER executables"]
    fn older_server_local_start_attach_and_stop_without_upgrade() {
        let old = std::env::var("DBMCP_TEST_LEGACY_SERVER").unwrap();
        let new = std::env::var("DBMCP_TEST_SERVER").unwrap();
        let dir = tempfile::tempdir().unwrap();
        let mut p = profile("legacy-compatibility");
        p.mode = ConnectionMode::Local;
        p.data_dir = dir.path().join("data").to_string_lossy().into();
        p.executable = new;
        tauri::async_runtime::block_on(async {
            let initialized = initialize_local(&p).await.unwrap();
            p.endpoint = format!("http://{}", initialized["admin"].as_str().unwrap());
            p.server_id = initialized["server_id"].as_str().unwrap().into();
            p.executable = old;
            let result: Result<(), String> = async {
                let status = start_local(&mut p).await?;
                if status["running"] != true {
                    return Err("Legacy MCP Server did not start".into());
                }
                let health = call(&p, &["api", "/admin/v1/health"]).await?;
                require_running_version(&health)?;
                let second = start_local(&mut p).await?;
                assert_eq!(second["instance_id"], status["instance_id"]);
                stop_local(&p, &second).await?;
                assert_eq!(call(&p, &["run", "status"]).await?["running"], false);
                assert!(!dir.path().join("data/runtime/current.json").exists());
                Ok(())
            }
            .await;
            let cleanup = call(&p, &["run", "stop"]).await;
            cleanup.unwrap();
            result.unwrap();
        });
    }
    #[test]
    fn migration_preserves_old_files_and_profile_preferences() {
        let dir = tempfile::tempdir().unwrap();
        let old =
            br#"{"endpoint":"http://127.0.0.1:19876","server_id":"original","close_to_tray":true}"#;
        fs::write(dir.path().join("profile.json"), old).unwrap();
        fs::write(
            dir.path().join("preferences.json"),
            br#"{"close_behavior":"exit","lightweight_mode":true}"#,
        )
        .unwrap();
        let mut store = load(dir.path()).unwrap();
        assert_eq!(store.active_id.as_deref(), Some("legacy"));
        assert!(store.profiles[0].import_legacy_ui);
        assert_eq!(store.profiles[0].server_id, "original");
        assert_eq!(store.profiles[0].name, "default");
        assert_eq!(
            store.profiles[0].preferences.close_behavior,
            preferences::CloseBehavior::Exit
        );
        let mut second = profile("second");
        second.preferences.theme = "dark".into();
        store.profiles.push(second);
        save(&dir.path().join("profiles.json"), &store).unwrap();
        let loaded = load(dir.path()).unwrap();
        assert_eq!(loaded.profiles[0].preferences.theme, "system");
        assert_eq!(loaded.profiles[1].preferences.theme, "dark");
        assert_eq!(fs::read(dir.path().join("profile.json")).unwrap(), old);
        fs::write(dir.path().join("profiles.json"), b"broken").unwrap();
        assert!(load(dir.path()).is_err()); // Never silently replace a broken catalog with legacy state.
    }
    #[test]
    fn first_launch_creates_one_disconnected_default_environment() {
        let dir = tempfile::tempdir().unwrap();
        let store = load(dir.path()).unwrap();
        assert_eq!(store.profiles.len(), 1);
        let p = &store.profiles[0];
        assert_eq!(p.name, "default");
        assert_eq!(store.active_id.as_ref(), Some(&p.id));
        validate(p).unwrap();
        save(&dir.path().join("profiles.json"), &store).unwrap();
        assert_eq!(load(dir.path()).unwrap().profiles[0].id, p.id);
    }
    #[test]
    fn operation_gate_rejects_stale_and_busy_sessions() {
        let state = Desktop {
            session: std::sync::Mutex::new(Runtime {
                profile: Some(profile("first")),
                generation: 4,
                ..Default::default()
            }),
            catalog: std::sync::Mutex::new(Store::default()),
            operations: tokio::sync::RwLock::new(()),
            events: tokio::sync::watch::channel(()).0,
            exiting: std::sync::atomic::AtomicBool::new(false),
            preferences: std::sync::Mutex::new(preferences::Preferences::default()),
            notifications: std::sync::Mutex::new(crate::notifications::NotificationState::default()),
            windows: std::sync::Mutex::new(crate::windows::WindowState::default()),
            http: reqwest::Client::new(),
        };
        let stamp = Stamp {
            profile_id: "first".into(),
            generation: 4,
        };
        let operation = operation(&state, &stamp).unwrap();
        assert!(state.operations.try_write().is_err());
        drop(operation);
        let transition = state.operations.try_write().unwrap();
        assert!(super::operation(&state, &stamp).is_err());
        drop(transition);
        state.session.lock().unwrap().generation += 1;
        assert!(super::operation(&state, &stamp).is_err());
        let stamp = Stamp {
            generation: 5,
            ..stamp
        };
        state.session.lock().unwrap().busy = true;
        assert!(super::operation(&state, &stamp).is_err());
    }
    #[test]
    fn local_control_requires_server_identity_and_supported_process_mode() {
        assert!(require_running_version(&json!({"version":"1.0.1"})).is_err());
        assert!(require_running_version(&json!({"version":"1.0.2"})).is_ok());
        assert!(require_running_version(&json!({"version":"1.0.10"})).is_ok());
        assert!(require_running_version(&json!({"version":"1.0.11"})).is_ok());
        let mut p = profile("first");
        p.server_id = "same-data".into();
        assert!(check_identity(
            &p,
            &json!({"running":true,"server_id":"same-data","instance_id":"replacement"})
        )
        .is_ok());
        assert!(check_identity(&p, &json!({"running":true,"server_id":"different-data"})).is_err());
        assert!(Owned::from_status(
            p.clone(),
            &json!({"instance_id":"current","mode":"foreground"})
        )
        .is_err());
        assert!(Owned::from_status(p.clone(), &json!({"mode":"background"})).is_ok());
        assert!(
            Owned::from_status(p.clone(), &json!({"mode":"background","version":"1.0.11"}))
                .is_err()
        );
        let owned =
            Owned::from_status(p, &json!({"instance_id":"replacement","mode":"background"}))
                .unwrap();
        assert_eq!(owned.instance_id, "replacement");
    }
}
#[tauri::command]
pub async fn exit_session(app: tauri::AppHandle, leave_running: bool) -> Result<(), String> {
    exit(app, leave_running).await
}

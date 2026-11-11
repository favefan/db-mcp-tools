use crate::{diagnostics, preferences, profiles, Desktop};
use serde_json::json;
use std::time::{Duration, Instant};
use tauri::menu::CheckMenuItem;
use tauri::{Emitter, Manager, State};
use tauri_plugin_dialog::DialogExt;

pub const AUTO_LIGHTWEIGHT_DELAY: Duration = Duration::from_secs(180);
pub const TRAY_ITEMS: [(&str, &str); 4] = [
    ("show", "打开主窗口"),
    ("lightweight", "轻量模式"),
    ("diagnostics", "导出诊断包"),
    ("quit", "退出"),
];

#[derive(Default)]
pub struct WindowState {
    generation: u64,
    deadline: Option<Instant>,
    active: bool,
    pub toggle: Option<CheckMenuItem<tauri::Wry>>,
}
impl WindowState {
    fn cancel(&mut self) {
        self.generation = self.generation.wrapping_add(1);
        self.deadline = None;
    }
    fn schedule(&mut self, now: Instant) -> u64 {
        self.cancel();
        self.deadline = Some(now + AUTO_LIGHTWEIGHT_DELAY);
        self.generation
    }
    fn due(&self, generation: u64, now: Instant) -> bool {
        self.generation == generation && self.deadline.is_some_and(|deadline| now >= deadline)
    }
}

fn cancel_auto(app: &tauri::AppHandle) {
    if let Ok(mut state) = app.state::<Desktop>().windows.lock() {
        if state.deadline.is_some() {
            diagnostics::event("window.lightweight_cancelled", json!({}));
        }
        state.cancel();
    }
}

fn set_active(app: &tauri::AppHandle, active: bool) -> Result<(), String> {
    let desktop = app.state::<Desktop>();
    let mut state = desktop.windows.lock().map_err(|e| e.to_string())?;
    state.active = active;
    if let Some(toggle) = &state.toggle {
        toggle.set_checked(active).map_err(|e| e.to_string())?;
    }
    Ok(())
}

pub fn runtime_info(app: &tauri::AppHandle) -> Result<serde_json::Value, String> {
    let desktop = app.state::<Desktop>();
    let state = desktop.windows.lock().map_err(|e| e.to_string())?;
    let mut info = json!({"active":state.active,"scheduled":state.deadline.is_some(),
        "delay_seconds":AUTO_LIGHTWEIGHT_DELAY.as_secs(),
        "remaining_seconds":state.deadline.map(|d|d.saturating_duration_since(Instant::now()).as_secs())});
    let toggle = state.toggle.clone();
    drop(state);
    info["tray_checked"] = json!(toggle
        .map(|t| t.is_checked())
        .transpose()
        .map_err(|e| e.to_string())?);
    Ok(info)
}

// Window transitions and timer expiry run on the main event loop, so a stale
// timer cannot destroy a window that has just been reopened.
pub fn show_main(app: &tauri::AppHandle) -> Result<(), String> {
    cancel_auto(app);
    let window = if let Some(window) = app.get_webview_window("main") {
        window
    } else {
        let config = app
            .config()
            .app
            .windows
            .iter()
            .find(|w| w.label == "main")
            .ok_or("主窗口配置缺失")?;
        tauri::WebviewWindowBuilder::from_config(app, config)
            .map_err(|e| e.to_string())?
            .build()
            .map_err(|e| e.to_string())?
    };
    window.unminimize().map_err(|e| e.to_string())?;
    window.show().map_err(|e| e.to_string())?;
    window.set_focus().map_err(|e| e.to_string())?;
    set_active(app, false)?;
    diagnostics::event("window.opened", json!({}));
    Ok(())
}

fn enter_lightweight(app: &tauri::AppHandle, source: &str) -> Result<(), String> {
    cancel_auto(app);
    if let Some(window) = app.get_webview_window("main") {
        window.destroy().map_err(|e| e.to_string())?;
    }
    set_active(app, true)?;
    diagnostics::event("window.lightweight_entered", json!({"source":source}));
    Ok(())
}

fn schedule_auto(app: &tauri::AppHandle) -> Result<(), String> {
    let desktop = app.state::<Desktop>();
    if !desktop
        .preferences
        .lock()
        .map_err(|e| e.to_string())?
        .lightweight_mode
    {
        return Ok(());
    }
    let generation = desktop
        .windows
        .lock()
        .map_err(|e| e.to_string())?
        .schedule(Instant::now());
    diagnostics::event(
        "window.lightweight_scheduled",
        json!({"delay_seconds":AUTO_LIGHTWEIGHT_DELAY.as_secs()}),
    );
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(AUTO_LIGHTWEIGHT_DELAY).await;
        let handle = app.clone();
        let result = app.run_on_main_thread(move || {
            let desktop = handle.state::<Desktop>();
            if !desktop
                .windows
                .lock()
                .is_ok_and(|s| s.due(generation, Instant::now()))
            {
                return;
            }
            let enabled = desktop.preferences.lock().is_ok_and(|p| p.lightweight_mode);
            let hidden = handle
                .get_webview_window("main")
                .is_some_and(|w| w.is_visible().is_ok_and(|v| !v));
            let result = if enabled && hidden {
                if let Ok(mut state) = desktop.windows.lock() {
                    state.deadline = None;
                }
                enter_lightweight(&handle, "automatic")
            } else {
                cancel_auto(&handle);
                Ok(())
            };
            diagnostics::result("window.lightweight_auto_result", &result);
        });
        diagnostics::result(
            "window.lightweight_dispatch",
            &result.map_err(|e| e.to_string()),
        );
    });
    Ok(())
}

#[tauri::command]
pub fn set_lightweight_mode(app: tauri::AppHandle, enabled: bool) -> Result<(), String> {
    if enabled {
        enter_lightweight(&app, "manual")
    } else {
        show_main(&app)
    }
}

fn apply(app: &tauri::AppHandle, action: preferences::CloseBehavior) -> Result<(), String> {
    if action == preferences::CloseBehavior::Exit {
        diagnostics::event("desktop.exit_requested", json!({}));
        let app = app.clone();
        tauri::async_runtime::spawn(async move {
            if let Err(error) = profiles::exit(app.clone(), false).await {
                show_exit_error(&app, &error);
            }
        });
        return Ok(());
    }
    cancel_auto(app);
    if let Some(window) = app.get_webview_window("main") {
        window.hide().map_err(|e| e.to_string())?;
        schedule_auto(app)?;
    }
    diagnostics::event("window.sent_to_tray", json!({}));
    Ok(())
}

pub fn close_requested(window: &tauri::Window) {
    let app = window.app_handle();
    let behavior = app
        .state::<Desktop>()
        .preferences
        .lock()
        .map(|p| p.close_behavior)
        .unwrap_or_default();
    if behavior == preferences::CloseBehavior::Ask {
        diagnostics::event("window.close_prompt", json!({}));
        let _ = window.emit("close-requested", ());
    } else {
        let result = apply(app, behavior);
        diagnostics::result("window.close_result", &result)
    }
}

#[tauri::command]
pub fn set_desktop_preferences(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    close_behavior: preferences::CloseBehavior,
    lightweight_mode: bool,
    theme: Option<String>,
    notifications: Option<bool>,
    session: profiles::Stamp,
) -> Result<preferences::Preferences, String> {
    let _guard = state
        .operations
        .try_write()
        .map_err(|_| "有操作正在进行，请稍候")?;
    if !profiles::matches(&state, &session) {
        return Err("会话已改变".into());
    }
    let mut next = state.preferences.lock().map_err(|e| e.to_string())?.clone();
    next.close_to_tray = close_behavior == preferences::CloseBehavior::Tray;
    next.close_behavior = close_behavior;
    next.lightweight_mode = lightweight_mode;
    if let Some(theme) = theme {
        if !matches!(theme.as_str(), "system" | "light" | "dark") {
            return Err("主题无效".into());
        }
        next.theme = theme;
    }
    if let Some(enabled) = notifications {
        next.notifications = enabled;
        state
            .notifications
            .lock()
            .map_err(|e| e.to_string())?
            .enabled = enabled;
    }
    profiles::persist_preferences(&app, next.clone())?;
    if !lightweight_mode {
        cancel_auto(&app);
    }
    diagnostics::event(
        "preferences.saved",
        json!({"close_behavior":close_behavior,"lightweight":lightweight_mode}),
    );
    Ok(next)
}

#[tauri::command]
pub async fn confirm_close(
    app: tauri::AppHandle,
    state: State<'_, Desktop>,
    action: preferences::CloseBehavior,
    remember: bool,
    session: Option<profiles::Stamp>,
) -> Result<(), String> {
    let guard = state
        .operations
        .try_write()
        .map_err(|_| "有操作正在进行，请稍候")?;
    if let Some(stamp) = session {
        if !profiles::matches(&state, &stamp) {
            return Err("会话已改变，请重新关闭窗口".into());
        }
    } else if state
        .session
        .lock()
        .map_err(|e| e.to_string())?
        .profile
        .is_some()
    {
        return Err("会话已改变，请重新关闭窗口".into());
    }
    if action == preferences::CloseBehavior::Ask {
        return Err("请选择最小化运行或直接退出".into());
    }
    if remember {
        let lightweight = state
            .preferences
            .lock()
            .map_err(|e| e.to_string())?
            .lightweight_mode;
        let mut next = state.preferences.lock().map_err(|e| e.to_string())?.clone();
        next.close_behavior = action;
        next.close_to_tray = action == preferences::CloseBehavior::Tray;
        next.lightweight_mode = lightweight;
        profiles::persist_preferences(&app, next)?;
    }
    if action == preferences::CloseBehavior::Exit {
        drop(guard);
        return profiles::exit(app, false).await;
    }
    let (send, receive) = tokio::sync::oneshot::channel();
    let handle = app.clone();
    app.run_on_main_thread(move || {
        let _ = send.send(apply(&handle, action));
    })
    .map_err(|e| e.to_string())?;
    receive.await.map_err(|e| e.to_string())?
}

pub fn tray_action(app: &tauri::AppHandle, id: &str) {
    match id {
        "show" => {
            let result = show_main(app);
            diagnostics::result("tray.open_result", &result)
        }
        "lightweight" => {
            let enabled = !app
                .state::<Desktop>()
                .windows
                .lock()
                .is_ok_and(|s| s.active);
            let result = set_lightweight_mode(app.clone(), enabled);
            diagnostics::result("tray.lightweight_result", &result)
        }
        "diagnostics" => {
            let app = app.clone();
            tauri::async_runtime::spawn(async move {
                let result = diagnostics::export(app.clone()).await;
                diagnostics::result("tray.export_result", &result);
                if let Err(error) = result {
                    app.dialog()
                        .message(error)
                        .title("导出诊断包失败")
                        .kind(tauri_plugin_dialog::MessageDialogKind::Error)
                        .show(|_| {});
                }
            });
        }
        "quit" => {
            diagnostics::event("tray.quit", json!({}));
            let app = app.clone();
            tauri::async_runtime::spawn(async move {
                if let Err(error) = profiles::exit(app.clone(), false).await {
                    show_exit_error(&app, &error);
                }
            });
        }
        _ => {}
    }
}

pub fn show_exit_error(app: &tauri::AppHandle, message: &str) {
    let _ = show_main(app);
    let _ = app.emit("exit-failed", message);
    diagnostics::event("desktop.exit_failed", json!({"message":message}));
}
pub fn profile_changed(app: &tauri::AppHandle) {
    cancel_auto(app);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reopening_disabling_and_manual_toggle_invalidate_old_deadlines() {
        let now = Instant::now();
        let mut state = WindowState::default();
        let first = state.schedule(now);
        assert!(!state.due(first, now + Duration::from_secs(179)));
        assert!(state.due(first, now + AUTO_LIGHTWEIGHT_DELAY));
        state.cancel();
        assert!(!state.due(first, now + AUTO_LIGHTWEIGHT_DELAY));
        let second = state.schedule(now + Duration::from_secs(60));
        assert!(!state.due(first, now + Duration::from_secs(180)));
        assert!(!state.due(second, now + Duration::from_secs(180)));
        assert!(state.due(second, now + Duration::from_secs(240)));
        state.cancel();
        assert!(!state.due(second, now + Duration::from_secs(300)));
    }
}

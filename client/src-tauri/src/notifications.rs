use crate::{diagnostics, request, Desktop, Profile};
use serde_json::Value;
use std::collections::HashSet;
use tauri::{Manager, State};
use tauri_plugin_notification::NotificationExt;

#[derive(Default)]
pub struct NotificationState {
    pub enabled: bool,
    seen: HashSet<String>,
}
impl NotificationState {
    fn update(&mut self, pending: HashSet<String>) -> usize {
        let added = pending.difference(&self.seen).count();
        self.seen = pending;
        if self.enabled {
            added
        } else {
            0
        }
    }
}

#[tauri::command]
pub fn sync_notifications(
    state: State<'_, Desktop>,
    enabled: bool,
    pending_ids: Vec<String>,
    session: crate::profiles::Stamp,
) -> Result<(), String> {
    let _guard = crate::profiles::operation(&state, &session)?;
    if pending_ids.len() > 1000
        || pending_ids
            .iter()
            .any(|id| id.len() != 32 || !id.bytes().all(|c| c.is_ascii_hexdigit()))
    {
        return Err("通知请求标识无效".into());
    }
    let mut current = state.notifications.lock().map_err(|e| e.to_string())?;
    current.enabled = enabled;
    current.update(pending_ids.into_iter().collect());
    Ok(())
}

pub async fn changed(
    app: &tauri::AppHandle,
    profile: &Profile,
    token: &str,
    stamp: &crate::profiles::Stamp,
) {
    if app.get_webview_window("main").is_some() {
        return;
    }
    let state = app.state::<Desktop>();
    if !state.notifications.lock().is_ok_and(|n| n.enabled) {
        return;
    }
    let mut pending = HashSet::new();
    for path in ["/admin/v1/approvals", "/admin/v1/registrations"] {
        let response = request(&state, profile, token, "GET", path, None).await;
        let values = match response {
            Ok(Value::Array(values)) => values,
            _ => {
                diagnostics::event(
                    "notification.collection_failed",
                    serde_json::json!({"route":path}),
                );
                return;
            }
        };
        for value in values {
            if value["status"] == "pending" {
                if let Some(id) = value["id"].as_str() {
                    pending.insert(id.to_string());
                }
            }
        }
    }
    if app.get_webview_window("main").is_some() {
        return;
    }
    if !crate::profiles::matches(&state, stamp) {
        return;
    }
    let added = state
        .notifications
        .lock()
        .map(|mut n| n.update(pending))
        .unwrap_or(0);
    if added > 0 {
        let result = app
            .notification()
            .builder()
            .title("DB MCP Tools · 操作待审批")
            .body(format!("{added} 条新请求等待处理，请从托盘打开主窗口。"))
            .show();
        diagnostics::event(
            "notification.sent",
            serde_json::json!({"count":added,"succeeded":result.is_ok(),"lightweight":true}),
        );
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn frontend_handoff_and_background_events_do_not_repeat_old_requests() {
        let mut state = NotificationState {
            enabled: true,
            ..NotificationState::default()
        };
        assert_eq!(state.update(HashSet::from(["known".into()])), 1);
        assert_eq!(
            state.update(HashSet::from(["known".into(), "new".into()])),
            1
        );
        assert_eq!(
            state.update(HashSet::from(["known".into(), "new".into()])),
            0
        );
        state.enabled = false;
        assert_eq!(state.update(HashSet::from(["later".into()])), 0);
        state.enabled = true;
        assert_eq!(state.update(HashSet::from(["later".into()])), 0);
    }
}

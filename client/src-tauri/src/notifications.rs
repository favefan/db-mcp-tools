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
    let _guard = crate::profiles::operation(&state, &session, "同步审批通知")?;
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
    let mut reasons = std::collections::HashMap::new();
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
                    if let Some(reason) = value["agent_reason"].as_str().filter(|r| !r.is_empty()) {
                        reasons.insert(id.to_string(), reason.to_string());
                    }
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
    let messages = {
        let Ok(mut notifications) = state.notifications.lock() else {
            return;
        };
        let messages = pending
            .difference(&notifications.seen)
            .map(|id| {
                reasons
                    .get(id)
                    .map(|reason| format!("请求说明（Agent 提供）：{reason}"))
                    .unwrap_or_else(|| "新请求等待处理，请从托盘打开主窗口。".into())
            })
            .collect::<Vec<_>>();
        if notifications.update(pending) == 0 {
            return;
        }
        messages
    };
    for message in &messages {
        let result = app
            .notification()
            .builder()
            .title("DB MCP Tools · 操作待审批")
            .body(message)
            .show();
        diagnostics::event(
            "notification.sent",
            serde_json::json!({"succeeded":result.is_ok(),"lightweight":true}),
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

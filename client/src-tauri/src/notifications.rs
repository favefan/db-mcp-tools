use crate::{diagnostics, request, Desktop, Profile};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::{HashMap, HashSet};
use std::time::{Duration, Instant};
use tauri::{Emitter, Manager, State};
use tauri_plugin_notification::NotificationExt;

#[derive(Default)]
pub struct NotificationState {
    pub enabled: bool,
    seen: HashSet<String>,
    failures: HashMap<String, Instant>,
}
impl NotificationState {
    fn reserve_failure(&mut self, instance: &str, now: Instant) -> bool {
        self.failures
            .retain(|_, time| now.duration_since(*time) < Duration::from_secs(60));
        if self.failures.contains_key(instance) {
            return false;
        }
        self.failures.insert(instance.into(), now);
        true
    }

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

#[derive(Deserialize, Serialize)]
struct DatabaseFailure {
    instance_id: String,
    instance_name: String,
    database: String,
    phase: String,
    reason: String,
    request_id: String,
}

// Foreground failures use the app's bottom-right toast; hidden/lightweight mode
// uses the same notification channel as approvals. Neither interrupts with a modal.
pub fn database_failed(app: &tauri::AppHandle, stamp: &crate::profiles::Stamp, event: &str) {
    if !event.lines().any(|line| line == "event: database-failed") {
        return;
    }
    let Some(data) = event.lines().find_map(|line| line.strip_prefix("data: ")) else {
        return;
    };
    let Ok(failure) = serde_json::from_str::<DatabaseFailure>(data) else {
        return;
    };
    let handle = app.clone();
    let stamp = stamp.clone();
    let _ = app.run_on_main_thread(move || {
        let state = handle.state::<Desktop>();
        if !crate::profiles::matches(&state, &stamp)
            || !state
                .notifications
                .lock()
                .is_ok_and(|mut n| n.reserve_failure(&failure.instance_id, Instant::now()))
        {
            return;
        }
        let stage = match failure.phase.as_str() {
            "preparation" => "连接或元数据预检查失败，尚未进入人工审批，SQL 未执行。",
            "recheck" => "执行前复查失败，SQL 未执行。该请求已失败，请恢复连接后重新提交。",
            _ => "数据库执行阶段失败，请在访问日志中检查请求结果。",
        };
        let visible = handle
            .get_webview_window("main")
            .is_some_and(|window| window.is_visible().unwrap_or(false));
        let result = if visible {
            handle
                .emit("database-failed", serde_json::json!({
                    "profile_id":stamp.profile_id,"generation":stamp.generation,
                    "instance_id":failure.instance_id,"instance_name":failure.instance_name,
                    "request_id":failure.request_id,"message":stage
                }))
                .map_err(|e| e.to_string())
        } else {
            handle
                .notification()
                .builder()
                .title(format!("DB MCP Tools · {} 数据库访问失败", failure.instance_name))
                .body(format!("{stage} 详细信息请打开访问日志。"))
                .extra("destination", "logs")
                .extra("profile_id", &stamp.profile_id)
                .extra("generation", stamp.generation)
                .show()
                .map_err(|e| e.to_string())
        };
        diagnostics::event("notification.database_failed", serde_json::json!({"request_id":failure.request_id,"succeeded":result.is_ok(),"foreground":visible}));
    });
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
    fn database_failure_toasts_coalesce_per_instance() {
        let mut state = NotificationState::default();
        let now = Instant::now();
        assert!(!state.enabled);
        assert!(state.reserve_failure("one", now));
        assert!(!state.reserve_failure("one", now + Duration::from_secs(59)));
        assert!(state.reserve_failure("one", now + Duration::from_secs(62)));
        assert!(!state.reserve_failure("one", now + Duration::from_secs(63)));
        assert!(state.reserve_failure("two", now + Duration::from_secs(63)));
    }
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

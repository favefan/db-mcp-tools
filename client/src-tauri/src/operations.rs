use std::{collections::BTreeMap, sync::Mutex, time::Duration};
use tokio::sync::{RwLock, RwLockReadGuard, RwLockWriteGuard};

#[derive(Default)]
pub struct Operations {
    lock: RwLock<()>,
    names: Mutex<BTreeMap<String, usize>>,
}
pub struct Permit<'a, T> {
    _guard: T,
    operations: &'a Operations,
    name: String,
}
impl<T> Drop for Permit<'_, T> {
    fn drop(&mut self) {
        let mut names = self
            .operations
            .names
            .lock()
            .unwrap_or_else(|e| e.into_inner());
        if let Some(count) = names.get_mut(&self.name) {
            *count -= 1;
            if *count == 0 {
                names.remove(&self.name);
            }
        }
    }
}
impl Operations {
    fn permit<T>(&self, guard: T, name: &str) -> Permit<'_, T> {
        *self
            .names
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .entry(name.into())
            .or_default() += 1;
        Permit {
            _guard: guard,
            operations: self,
            name: name.into(),
        }
    }
    fn busy(&self) -> String {
        let names = self.names.lock().unwrap_or_else(|e| e.into_inner());
        let active = names.keys().cloned().collect::<Vec<_>>().join("、");
        format!(
            "正在{}，请稍候重试",
            if active.is_empty() {
                "准备配置操作"
            } else {
                &active
            }
        )
    }
    pub fn try_read(&self, name: &str) -> Result<Permit<'_, RwLockReadGuard<'_, ()>>, String> {
        let guard = self.lock.try_read().map_err(|_| self.busy())?;
        Ok(self.permit(guard, name))
    }
    pub async fn write(&self, name: &str) -> Result<Permit<'_, RwLockWriteGuard<'_, ()>>, String> {
        // Queue behind short background reads. The writer prevents new polling
        // requests from repeatedly winning the lock while a user action waits.
        let guard = tokio::time::timeout(Duration::from_secs(3), self.lock.write())
            .await
            .map_err(|_| self.busy())?;
        Ok(self.permit(guard, name))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn user_action_waits_for_background_read_and_names_active_work() {
        let operations = Operations::default();
        let read = operations.try_read("读取 MCP Server 状态").unwrap();
        let write = operations.write("切换配置");
        tokio::pin!(write);
        assert!(tokio::time::timeout(Duration::from_millis(10), &mut write)
            .await
            .is_err());
        assert!(operations.busy().contains("读取 MCP Server 状态"));
        drop(read);
        let write = write.await.unwrap();
        assert!(operations
            .try_read("日志读取")
            .err()
            .unwrap()
            .contains("切换配置"));
        drop(write);
        assert!(operations.try_read("日志读取").is_ok());
    }
}

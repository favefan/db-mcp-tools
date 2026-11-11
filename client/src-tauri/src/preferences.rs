use serde::{Deserialize, Serialize};
use std::{fs, path::Path};

#[derive(Clone, Copy, Default, Deserialize, Serialize, PartialEq, Debug)]
#[serde(rename_all = "snake_case")]
pub enum CloseBehavior {
    #[default]
    Ask,
    Tray,
    Exit,
}

#[derive(Clone, Deserialize, Serialize)]
#[serde(default)]
pub struct Preferences {
    pub theme: String,
    pub notifications: bool,
    pub close_to_tray: bool,
    pub close_behavior: CloseBehavior,
    pub lightweight_mode: bool,
}

impl Default for Preferences {
    fn default() -> Self {
        Self {
            theme: "system".into(),
            notifications: false,
            close_to_tray: false,
            close_behavior: CloseBehavior::Ask,
            lightweight_mode: false,
        }
    }
}

pub fn load(path: &Path, legacy_close_to_tray: bool) -> Result<Preferences, String> {
    match fs::read(path) {
        Ok(bytes) => serde_json::from_slice(&bytes).map_err(|e| e.to_string()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
            let preferences = Preferences {
                close_to_tray: legacy_close_to_tray,
                ..Preferences::default()
            };
            save(path, &preferences)?;
            Ok(preferences)
        }
        Err(e) => Err(e.to_string()),
    }
}

pub fn save(path: &Path, preferences: &Preferences) -> Result<(), String> {
    let parent = path.parent().ok_or("客户端配置目录无效")?;
    let mut tmp = tempfile::NamedTempFile::new_in(parent).map_err(|e| e.to_string())?;
    serde_json::to_writer(&mut tmp, preferences).map_err(|e| e.to_string())?;
    tmp.as_file().sync_all().map_err(|e| e.to_string())?;
    tmp.persist(path).map_err(|e| e.to_string())?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn global_preference_overrides_legacy_connection_and_survives_reconnect() {
        let root = tempfile::tempdir().unwrap();
        let path = root.path().join("preferences.json");
        assert!(load(&path, true).unwrap().close_to_tray);
        assert!(load(&path, false).unwrap().close_to_tray);
        assert_eq!(
            load(&path, false).unwrap().close_behavior,
            CloseBehavior::Ask
        );
        save(
            &path,
            &Preferences {
                close_to_tray: false,
                close_behavior: CloseBehavior::Exit,
                ..Preferences::default()
            },
        )
        .unwrap();
        assert!(!load(&path, true).unwrap().close_to_tray);
        assert!(!load(&path, false).unwrap().close_to_tray);
        save(
            &path,
            &Preferences {
                close_to_tray: true,
                close_behavior: CloseBehavior::Tray,
                lightweight_mode: true,
                ..Preferences::default()
            },
        )
        .unwrap();
        assert!(load(&path, false).unwrap().close_to_tray);
        assert_eq!(
            load(&path, false).unwrap().close_behavior,
            CloseBehavior::Tray
        );
        assert!(load(&path, false).unwrap().lightweight_mode);
    }

    #[test]
    fn legacy_global_file_requires_confirmation_until_new_choice_saved() {
        let root = tempfile::tempdir().unwrap();
        let path = root.path().join("preferences.json");
        fs::write(&path, r#"{"close_to_tray":true}"#).unwrap();
        assert_eq!(
            load(&path, false).unwrap().close_behavior,
            CloseBehavior::Ask
        );
        let next = Preferences {
            close_behavior: CloseBehavior::Exit,
            lightweight_mode: true,
            ..Preferences::default()
        };
        save(&path, &next).unwrap();
        assert_eq!(
            load(&path, true).unwrap().close_behavior,
            CloseBehavior::Exit
        );
        assert!(load(&path, true).unwrap().lightweight_mode);
    }
}

use crate::{hide, managed_launcher, profiles, program_version};
use semver::Version;
use serde_json::{json, Value};
use std::{
    collections::HashSet,
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
    process::Command,
    time::{SystemTime, UNIX_EPOCH},
};

// Run before Tauri's single-instance plugin and WebView initialization. The
// per-user installer supplies its own profile identity, including in tests.
pub fn execute() -> Option<i32> {
    let args: Vec<_> = std::env::args_os().skip(1).collect();
    if args
        .first()
        .is_none_or(|arg| arg != "--install-bundled-server")
    {
        return None;
    }
    if args.len() != 5 || !cfg!(all(windows, feature = "bundled-server")) {
        return Some(2);
    }
    let log = PathBuf::from(&args[4]);
    let result = std::env::current_exe()
        .map_err(|e| e.to_string())
        .and_then(|exe| {
            upgrade(
                Path::new(&args[1]),
                exe.parent().ok_or("Invalid installation directory")?,
                Path::new(&args[2]),
                Path::new(&args[3]),
                &log,
            )
        });
    if let Err(error) = result {
        let _ = record(&log, &format!("MCP Server upgrade failed: {error}"));
        return Some(2);
    }
    Some(0)
}

fn record(path: &Path, message: &str) -> Result<(), String> {
    fs::create_dir_all(path.parent().ok_or("Invalid installer log path")?)
        .map_err(|e| e.to_string())?;
    let reset = fs::metadata(path).is_ok_and(|meta| meta.len() > 1_048_576);
    let mut file = OpenOptions::new()
        .create(true)
        .write(true)
        .append(!reset)
        .truncate(reset)
        .open(path)
        .map_err(|e| e.to_string())?;
    let event = json!({
        "component": "installer",
        "version": env!("CARGO_PKG_VERSION"),
        "time_unix_ms": SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_millis(),
        "msg": message
    });
    writeln!(file, "{event}").map_err(|e| e.to_string())
}

fn command(executable: &Path, directory: &Path, args: &[&str]) -> Result<Value, String> {
    let mut cmd = Command::new(executable);
    cmd.arg("--data-dir").arg(directory).args(args);
    hide(&mut cmd);
    let out = cmd.output().map_err(|e| e.to_string())?;
    if !out.status.success() {
        return Err(String::from_utf8_lossy(&out.stderr).trim().to_owned());
    }
    serde_json::from_slice(&out.stdout).map_err(|_| "Invalid MCP Server response".into())
}

fn version(value: &Value) -> Result<Version, String> {
    Version::parse(
        value["version"]
            .as_str()
            .ok_or("Missing MCP Server version")?,
    )
    .map_err(|e| e.to_string())
}

fn same_file(a: &Path, b: &Path) -> bool {
    matches!((a.canonicalize(), b.canonicalize()), (Ok(a), Ok(b)) if a == b)
}

fn eligible(profile: &profiles::Profile, install: &Path, previous: &Path) -> bool {
    if profile.mode != profiles::ConnectionMode::Local {
        return false;
    }
    let exe = Path::new(&profile.executable);
    managed_launcher(exe, Path::new(&profile.data_dir))
        || same_file(exe, &install.join("dbmcp.exe"))
        || (previous.is_absolute() && same_file(exe, &previous.join("dbmcp.exe")))
}

fn upgrade(
    config: &Path,
    source: &Path,
    install: &Path,
    previous: &Path,
    log: &Path,
) -> Result<(), String> {
    if !config.is_absolute() {
        return Err("Installer profile directory must be absolute".into());
    }
    if !config.join("profiles.json").exists() && !config.join("profile.json").exists() {
        return record(log, "No existing local configurations to upgrade");
    }
    let executable = source.join("dbmcp.exe");
    let bundled = program_version(&executable)?;
    let bundled_version = version(&bundled)?;
    let mut store = profiles::load(config)?;
    let mut targets = Vec::new();
    // Preflight every target before stopping or deploying any instance.
    for (index, profile) in store.profiles.iter().enumerate() {
        if !eligible(profile, install, previous) {
            continue;
        }
        let data = Path::new(&profile.data_dir);
        if !data.join("deployment.toml").is_file() {
            continue;
        }
        let status = command(&executable, data, &["run", "status"])?;
        let launcher = data.join("runtime/dbmcp.exe");
        let current = if status["running"] == true {
            status.clone()
        } else if launcher.is_file() && data.join("runtime/current.json").is_file() {
            program_version(&launcher)?
        } else {
            program_version(Path::new(&profile.executable))?
        };
        profiles::check_identity(profile, &status)?;
        let native = command(&executable, data, &["service", "status"])?;
        if native["installed"] == true
            || status["running"] == true && status["mode"] != "background"
        {
            record(
                log,
                &format!(
                    "Skipped independently managed MCP Server: {}",
                    data.display()
                ),
            )?;
            continue;
        }
        if native["installed"] != false {
            return Err(format!(
                "Cannot determine service ownership for {}",
                data.display()
            ));
        }
        if current["drivers"].as_object().is_some_and(|drivers| {
            drivers
                .iter()
                .any(|(name, enabled)| enabled == true && bundled["drivers"][name] != true)
        }) {
            record(
                log,
                &format!(
                    "Retained MCP Server with additional drivers: {}",
                    data.display()
                ),
            )?;
            continue;
        }
        targets.push((
            index,
            data.to_path_buf(),
            version(&current)? < bundled_version,
            status["running"] == true,
        ));
    }
    let mut deployed = HashSet::new();
    let mut changed = false;
    for (index, data, newer, running) in targets {
        if newer && deployed.insert(data.canonicalize().map_err(|e| e.to_string())?) {
            record(
                log,
                &format!(
                    "Upgrading MCP Server to {bundled_version}: {}",
                    data.display()
                ),
            )?;
            command(&executable, &data, &["runtime", "upgrade"])?;
            let installed = program_version(&data.join("runtime/dbmcp.exe"))?;
            if version(&installed)? != bundled_version {
                return Err("Deployed MCP Server version does not match bundled version".into());
            }
            record(
                log,
                &format!("MCP Server upgrade completed: {}", data.display()),
            )?;
        }
        if !newer && !data.join("runtime/current.json").is_file() {
            // Release a directly launched bundled executable before NSIS
            // replaces it, retaining its version in stable runtime.
            let selected = Path::new(&store.profiles[index].executable);
            if running {
                command(selected, &data, &["run", "stop"])?;
            }
            command(selected, &data, &["runtime", "prepare"])?;
            if running {
                command(selected, &data, &["run", "start"])?;
            }
        }
        let launcher = data.join("runtime/dbmcp.exe");
        let target = if launcher.is_file() {
            launcher
        } else {
            install.join("dbmcp.exe")
        };
        let path = target.to_string_lossy().to_string();
        if !same_file(Path::new(&store.profiles[index].executable), &target) {
            store.profiles[index].executable = path;
            changed = true;
        }
    }
    if changed {
        profiles::save(&config.join("profiles.json"), &store)?;
    }
    record(log, "Bundled MCP Server installation completed")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn installer_never_selects_http_or_unrelated_external_executables() {
        let dir = tempfile::tempdir().unwrap();
        let install = dir.path().join("installed");
        fs::create_dir(&install).unwrap();
        let own = install.join("dbmcp.exe");
        fs::write(&own, "fixture").unwrap();
        let mut profile = profiles::Profile {
            executable: own.to_string_lossy().into(),
            ..Default::default()
        };
        assert!(!eligible(&profile, &install, Path::new("")));
        profile.mode = profiles::ConnectionMode::Local;
        assert!(eligible(&profile, &install, Path::new("")));
        let external = dir.path().join("external.exe");
        fs::write(&external, "external").unwrap();
        profile.executable = external.to_string_lossy().into();
        assert!(!eligible(&profile, &install, Path::new("")));
    }
}

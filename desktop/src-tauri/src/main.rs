// The webview never sends a command line. It sends a query or a session ref, and this
// file builds the kioku argv itself, so a transcript cannot make the app run anything.
use serde_json::{json, Value};
use std::{env, fs, process::Command};

fn kioku(args: &[String]) -> Result<String, String> {
    let bin = env::var("KIOKU_BIN").unwrap_or_else(|_| "kioku".into());
    let out = Command::new(&bin)
        .args(args)
        .output()
        .map_err(|e| format!("cannot run {bin}: {e}"))?;
    if !out.status.success() {
        let err = String::from_utf8_lossy(&out.stderr).trim().to_string();
        return Err(if err.is_empty() { format!("{bin} exited with {}", out.status) } else { err });
    }
    String::from_utf8(out.stdout).map_err(|e| e.to_string())
}

fn kioku_json(args: &[String]) -> Result<Value, String> {
    serde_json::from_str(&kioku(args)?).map_err(|e| format!("kioku printed invalid JSON: {e}"))
}

fn s(v: &[&str]) -> Vec<String> {
    v.iter().map(|x| x.to_string()).collect()
}

// A session ref is hex, optionally followed by :<message number>.
fn valid_ref(r: &str) -> bool {
    !r.is_empty() && r.len() <= 64 && r.chars().all(|c| c.is_ascii_hexdigit() || c == ':')
}

#[tauri::command]
fn search(query: String, days: Option<u32>, project: Option<String>) -> Result<Value, String> {
    let mut a = s(&["--sessions", "--json", "--facets", "--limit", "40"]);
    if let Some(d) = days {
        a.extend(s(&["--since", &format!("{d}d")]));
    }
    if let Some(p) = project.filter(|p| !p.is_empty()) {
        a.extend(s(&["-p", &p]));
    }
    if !query.is_empty() {
        a.extend(s(&["--", &query]));
    }
    let sessions = kioku_json(&a)?;

    // Hits per week come from the message search, over all time, so the axis keeps its history.
    let mut w = s(&["--json", "--facets", "--limit", "1"]);
    if !query.is_empty() {
        w.extend(s(&["--", &query]));
    }
    let weeks = kioku_json(&w).ok().and_then(|v| v.get("facets").cloned()).unwrap_or(Value::Null);
    Ok(json!({ "sessions": sessions, "weeks": weeks }))
}

#[tauri::command]
fn show_session(reference: String, query: String) -> Result<Value, String> {
    if !valid_ref(&reference) {
        return Err("bad session ref".into());
    }
    let mut a = s(&["show", &reference, "--all", "--json"]);
    if !query.is_empty() {
        a.extend(s(&["--query", &query]));
    }
    kioku_json(&a)
}

fn sh_quote(x: &str) -> String {
    format!("'{}'", x.replace('\'', "'\\''"))
}

fn applescript_string(x: &str) -> String {
    format!("\"{}\"", x.replace('\\', "\\\\").replace('"', "\\\""))
}

#[tauri::command]
fn resume(reference: String) -> Result<(), String> {
    if !valid_ref(&reference) {
        return Err("bad session ref".into());
    }
    let v = kioku_json(&s(&["show", &reference, "--json", "--limit", "1"]))?;
    let cwd = v["cwd"].as_str().unwrap_or_default().to_string();
    let argv: Vec<String> = v["resume_argv"]
        .as_array()
        .map(|a| a.iter().filter_map(|x| x.as_str().map(String::from)).collect())
        .unwrap_or_default();
    if argv.is_empty() {
        return Err("this session has no resume command".into());
    }
    let mut cmd = String::new();
    if fs::metadata(&cwd).map(|m| m.is_dir()).unwrap_or(false) {
        cmd.push_str(&format!("cd {} && ", sh_quote(&cwd)));
    }
    cmd.push_str(&argv.iter().map(|x| sh_quote(x)).collect::<Vec<_>>().join(" "));
    let script = format!(
        "tell application \"Terminal\"\nactivate\ndo script {}\nend tell",
        applescript_string(&cmd)
    );
    Command::new("osascript").args(["-e", &script]).status().map_err(|e| e.to_string())?;
    Ok(())
}

// End-to-end hooks: inert unless the test script sets these variables.
#[tauri::command]
fn e2e_config() -> Option<Value> {
    let out = env::var("KIOKU_E2E_OUT").ok()?;
    Some(json!({ "out": out, "query": env::var("KIOKU_E2E_QUERY").unwrap_or_default() }))
}

#[tauri::command]
fn e2e_report(app: tauri::AppHandle, report: String) -> Result<(), String> {
    let out = env::var("KIOKU_E2E_OUT").map_err(|_| "not an e2e run")?;
    fs::write(out, report).map_err(|e| e.to_string())?;
    app.exit(0);
    Ok(())
}

fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![search, show_session, resume, e2e_config, e2e_report])
        .run(tauri::generate_context!())
        .expect("error while running kioku desktop");
}

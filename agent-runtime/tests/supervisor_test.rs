#[cfg(target_os = "linux")]
use agent_runtime::supervisor::Supervisor;
#[cfg(target_os = "linux")]
use std::time::Duration;

#[cfg(target_os = "linux")]
#[tokio::test]
async fn test_supervisor_start_stop() {
    let s = Supervisor::new();
    assert!(!s.is_running());

    s.start("/usr/bin/yes").unwrap();
    tokio::time::sleep(Duration::from_millis(100)).await;
    assert!(s.is_running());

    s.stop();
    assert!(!s.is_running());
}

#[cfg(target_os = "linux")]
#[tokio::test]
async fn test_supervisor_restart() {
    let s = Supervisor::new();
    s.start("/usr/bin/yes").unwrap();
    tokio::time::sleep(Duration::from_millis(50)).await;
    assert!(s.is_running());

    s.restart("/usr/bin/yes").unwrap();
    tokio::time::sleep(Duration::from_millis(50)).await;
    assert!(s.is_running());

    s.stop();
    assert!(!s.is_running());
}

#[cfg(target_os = "linux")]
#[tokio::test]
async fn test_supervisor_is_running_exited() {
    let s = Supervisor::new();
    // /bin/true exits immediately with code 0
    s.start("/bin/true").unwrap();
    tokio::time::sleep(Duration::from_millis(200)).await;
    // Process should have exited by now
    assert!(!s.is_running());
}

// On non-Linux, provide a placeholder test so the test binary compiles
#[cfg(not(target_os = "linux"))]
#[test]
fn test_supervisor_linux_only() {
    // Supervisor tests require Linux; skip on other platforms
}

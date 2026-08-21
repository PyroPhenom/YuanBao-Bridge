package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

func isYuanbaoRunning() bool {
	out, err := exec.Command("tasklist", "/NH", "/FO", "CSV").Output()
	if err != nil {
		return false
	}
	s := strings.ToLower(string(out))
	return strings.Contains(s, "yuanbao.exe") || strings.Contains(s, "yuanbao-debug.exe")
}

func cdpReachable(cfg Config) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(cfg.CDPVersionURL())
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func launchYuanbao(cfg Config) error {
	exe := cfg.DebugExe()
	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("debug exe missing: %s", exe)
	}
	cmd := exec.Command(exe)
	cmd.Dir = cfg.InstallDir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", exe, err)
	}
	fmt.Printf("[launch] started %s (pid %d)\n", exe, cmd.Process.Pid)
	return nil
}

func ensureYuanbaoRunning(cfg Config) error {
	if err := ensureDebugExe(cfg); err != nil {
		return err
	}

	if cdpReachable(cfg) {
		fmt.Println("[launch] CDP already available")
		return nil
	}

	if !isYuanbaoRunning() {
		fmt.Println("[launch] Yuanbao not running, starting...")
		if err := launchYuanbao(cfg); err != nil {
			return err
		}
	} else {
		fmt.Println("[launch] Yuanbao running without CDP, restarting with debug exe...")
		if err := stopYuanbaoProcesses(); err != nil {
			return err
		}
		time.Sleep(2 * time.Second)
		if err := launchYuanbao(cfg); err != nil {
			return err
		}
	}

	return waitForCDP(cfg, 90*time.Second)
}

func stopYuanbaoProcesses() error {
	for _, name := range []string{"yuanbao.exe", "yuanbao-debug.exe"} {
		cmd := exec.Command("taskkill", "/F", "/IM", name)
		cmd.Run() // ignore error if not running
	}
	return nil
}

func waitForCDP(cfg Config, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cdpReachable(cfg) {
			fmt.Printf("[launch] CDP ready at %s\n", cfg.CDPVersionURL())
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("CDP not ready after %s (port %d)", timeout, cfg.CDPPort)
}

func waitForChatPage(cfg Config, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, err := cdpStatus(cfg)
		if err == nil && st.ChatPageReady {
			fmt.Printf("[launch] chat page ready: %s\n", st.ChatPageURL)
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("chat page not ready after %s", timeout)
}

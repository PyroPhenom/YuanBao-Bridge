package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultCDPPort = 9333
)

type Config struct {
	InstallDir string
	CDPPort    int
	ListenHost string
	ListenPort int
	AgentID    string
	Model      string
}

func defaultConfig() Config {
	install := os.Getenv("YUANBAO_INSTALL_DIR")
	if install == "" {
		install = "C:\\Program Files\\Tencent\\Yuanbao"
	}
	return Config{
		InstallDir: install,
		CDPPort:    defaultCDPPort,
		ListenHost: "127.0.0.1",
		ListenPort: 8765,
		AgentID:    os.Getenv("YUANBAO_AGENT_ID"),
		Model:      os.Getenv("YUANBAO_MODEL"),
	}
}

func (c Config) YuanbaoExe() string {
	return filepath.Join(c.InstallDir, "yuanbao.exe")
}

func (c Config) DebugExe() string {
	return filepath.Join(c.InstallDir, "yuanbao-debug.exe")
}

func (c Config) CDPListURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/json/list", c.CDPPort)
}

func (c Config) CDPVersionURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/json/version", c.CDPPort)
}

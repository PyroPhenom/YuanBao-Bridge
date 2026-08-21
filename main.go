package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	cfg := defaultConfig()

	install := flag.String("install-dir", cfg.InstallDir, "Yuanbao install directory")
	cdpPort := flag.Int("cdp-port", cfg.CDPPort, "Chrome DevTools Protocol port")
	host := flag.String("host", cfg.ListenHost, "HTTP listen host")
	port := flag.Int("port", cfg.ListenPort, "HTTP listen port")
	agent := flag.String("agent", cfg.AgentID, "default agent ID")
	model := flag.String("model", cfg.Model, "default chat model")
	skipLaunch := flag.Bool("skip-launch", false, "do not auto-start Yuanbao")
	patchOnly := flag.Bool("patch-only", false, "only create yuanbao-debug.exe and exit")
	flag.Parse()

	cfg.InstallDir = *install
	cfg.CDPPort = *cdpPort
	cfg.ListenHost = *host
	cfg.ListenPort = *port
	cfg.AgentID = *agent
	cfg.Model = *model

	if err := ensureDebugExe(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "patch error: %v\n", err)
		os.Exit(1)
	}

	if *patchOnly {
		fmt.Println("patch ok:", cfg.DebugExe())
		return
	}

	cdp := newCDPClient(cfg)

	if !*skipLaunch {
		if err := ensureYuanbaoRunning(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "launch error: %v\n", err)
			os.Exit(1)
		}

		st, _ := cdpStatus(cfg)
		if !st.ChatPageReady {
			fmt.Println("[launch] chat page not ready, trying navigate...")
			if err := cdp.tryNavigateToChat(); err != nil {
				fmt.Printf("[launch] navigate warning: %v\n", err)
			}
			if err := waitForChatPage(cfg, 120*time.Second); err != nil {
				fmt.Printf("[launch] warning: %v (API may return 502 until chat opens)\n", err)
			}
		}
	}

	addr := fmt.Sprintf("%s:%d", cfg.ListenHost, cfg.ListenPort)
	srv := newServer(cfg, cdp)
	if err := srv.Serve(addr); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

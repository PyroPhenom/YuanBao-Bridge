package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type CDPTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type CDPStatus struct {
	CDPAvailable    bool   `json:"cdp_available"`
	ChatPageReady   bool   `json:"chat_page_ready"`
	ChatPageURL     string `json:"chat_page_url,omitempty"`
}

type CDPClient struct {
	cfg     Config
	cacheMu sync.Mutex
	cached  *contextCache
}

func newCDPClient(cfg Config) *CDPClient {
	return &CDPClient{cfg: cfg}
}

func listCDPTargets(cfg Config) ([]CDPTarget, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(cfg.CDPListURL())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var targets []CDPTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func findChatTarget(cfg Config) (*CDPTarget, error) {
	targets, err := listCDPTargets(cfg)
	if err != nil {
		return nil, err
	}
	for _, t := range targets {
		if t.Type == "page" && containsChatPath(t.URL) {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("no Yuanbao chat page on CDP %d", cfg.CDPPort)
}

func containsChatPath(url string) bool {
	return containsIgnoreCase(url, "/chat")
}

func containsIgnoreCase(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexIgnoreCase(s, sub) >= 0)
}

func indexIgnoreCase(s, sub string) int {
	// simple ASCII case fold for URL matching
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func cdpStatus(cfg Config) (CDPStatus, error) {
	targets, err := listCDPTargets(cfg)
	if err != nil {
		return CDPStatus{}, err
	}
	st := CDPStatus{CDPAvailable: len(targets) > 0}
	for _, t := range targets {
		if t.Type == "page" && containsChatPath(t.URL) {
			st.ChatPageReady = true
			st.ChatPageURL = t.URL
			break
		}
	}
	return st, nil
}

type cdpMessage struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *cdpError       `json:"error"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *CDPClient) call(ws *websocket.Conn, inbox chan cdpMessage, id int, method string, params map[string]interface{}, timeout time.Duration) (json.RawMessage, error) {
	req := map[string]interface{}{
		"id":     id,
		"method": method,
		"params": params,
	}
	if err := ws.WriteJSON(req); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		select {
		case msg := <-inbox:
			if msg.ID != id {
				continue
			}
			if msg.Error != nil {
				return nil, fmt.Errorf("CDP %s: %s", method, msg.Error.Message)
			}
			return msg.Result, nil
		case <-time.After(time.Until(deadline)):
			return nil, fmt.Errorf("CDP %s timeout", method)
		}
	}
}

func (c *CDPClient) evaluateOnTarget(target *CDPTarget, expression string, timeout time.Duration) (map[string]interface{}, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	ws, _, err := dialer.Dial(target.WebSocketDebuggerURL, nil)
	if err != nil {
		return nil, err
	}
	defer ws.Close()

	inbox := make(chan cdpMessage, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var msg cdpMessage
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}
			if msg.ID != 0 {
				inbox <- msg
			}
		}
	}()

	id := 1
	if _, err := c.call(ws, inbox, id, "Runtime.enable", nil, 30*time.Second); err != nil {
		return nil, err
	}
	id++
	resultRaw, err := c.call(ws, inbox, id, "Runtime.evaluate", map[string]interface{}{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	}, timeout)
	if err != nil {
		return nil, err
	}

	var wrapper map[string]interface{}
	if err := json.Unmarshal(resultRaw, &wrapper); err != nil {
		return nil, err
	}
	if exc, ok := wrapper["exceptionDetails"]; ok && exc != nil {
		b, _ := json.Marshal(exc)
		return map[string]interface{}{"ok": false, "error": string(b)}, nil
	}
	if result, ok := wrapper["result"].(map[string]interface{}); ok {
		if val, ok := result["value"].(map[string]interface{}); ok {
			return val, nil
		}
	}
	return map[string]interface{}{"ok": false, "error": "empty evaluate result", "raw": wrapper}, nil
}

func (c *CDPClient) tryNavigateToChat() error {
	targets, err := listCDPTargets(c.cfg)
	if err != nil {
		return err
	}
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}
		if !containsIgnoreCase(t.URL, "yuanbao") {
			continue
		}
		if containsChatPath(t.URL) {
			return nil
		}
		dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
		ws, _, err := dialer.Dial(t.WebSocketDebuggerURL, nil)
		if err != nil {
			continue
		}
		inbox := make(chan cdpMessage, 32)
		go func() {
			for {
				var msg cdpMessage
				if err := ws.ReadJSON(&msg); err != nil {
					return
				}
				if msg.ID != 0 {
					inbox <- msg
				}
			}
		}()
		id := 1
		_, err = c.call(ws, inbox, id, "Page.navigate", map[string]interface{}{
			"url": "https://tencent.yuanbao/chat",
		}, 30*time.Second)
		ws.Close()
		if err == nil {
			fmt.Println("[cdp] navigated to chat page")
			return nil
		}
	}
	return fmt.Errorf("no yuanbao page to navigate")
}

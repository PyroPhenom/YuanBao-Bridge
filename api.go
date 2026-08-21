package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	cfg   Config
	cdp   *CDPClient
	mux   *http.ServeMux
}

func newServer(cfg Config, cdp *CDPClient) *Server {
	s := &Server{cfg: cfg, cdp: cdp, mux: http.NewServeMux()}
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/chat", s.handleAPIChat)
	s.mux.HandleFunc("/v1/chat/completions", s.handleOpenAIChat)
	return s
}

func (s *Server) Serve(addr string) error {
	fmt.Printf("Yuanbao Bridge API -> http://%s\n", addr)
	fmt.Println("  GET  /health")
	fmt.Println("  POST /api/chat  (JSON: prompt, conversation_id, thinking, images_base64; or multipart)")
	fmt.Println("  POST /v1/chat/completions  (OpenAI; image_url supports http(s) and data: URLs)")
	return http.ListenAndServe(addr, s.mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	st, err := cdpStatus(s.cfg)
	resp := map[string]interface{}{
		"bridge": "dtmp-ybChatService",
	}
	if err != nil {
		resp["status"] = "degraded"
		resp["cdp_available"] = false
		resp["chat_page_ready"] = false
		resp["error"] = err.Error()
	} else {
		ready := st.CDPAvailable && st.ChatPageReady
		resp["status"] = "ok"
		if !ready {
			resp["status"] = "degraded"
		}
		resp["cdp_available"] = st.CDPAvailable
		resp["chat_page_ready"] = st.ChatPageReady
		if st.ChatPageURL != "" {
			resp["chat_page_url"] = st.ChatPageURL
		}
		if st.ChatPageReady {
			if ctx, err := s.cdp.DetectChatContext(ChatContext{AgentID: s.cfg.AgentID, Model: s.cfg.Model}); err == nil {
				resp["auto_agent_id"] = ctx.AgentID
				resp["auto_model"] = ctx.Model
				if len(ctx.Sources) > 0 {
					resp["auto_sources"] = ctx.Sources
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type chatRequest struct {
	Prompt         string   `json:"prompt"`
	ConversationID string   `json:"conversation_id"` // omit or empty to create new session
	Thinking       bool     `json:"thinking"`        // deep thinking mode (uses model subModelList)
	Images         []string `json:"images"`          // http(s) URL, data URL, or server-local path
	ImagesBase64   []string `json:"images_base64"`   // raw base64 or data:image/...;base64,...
	AgentID        string   `json:"agentId"`
	Model          string   `json:"model"`
}

func (s *Server) handleAPIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	prompt, conversationID, thinking, agentID, model, pathInputs, base64Images, uploadFiles, err := s.parseAPIChatRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(prompt) == "" && len(pathInputs) == 0 && len(base64Images) == 0 && len(uploadFiles) == 0 {
		http.Error(w, "prompt or images required (use images_base64, multipart images, or image URLs)", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = "请描述这些图片"
	}
	agentID, model, err = s.resolveAgentModel(agentID, model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	imagePaths, cleanup, err := s.resolveImagePaths(pathInputs, base64Images, uploadFiles)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	result, err := s.cdp.Chat(prompt, agentID, model, conversationID, imagePaths, thinking)
	if err != nil && !result.OK {
		writeJSON(w, http.StatusBadGateway, result)
		return
	}
	if !result.OK {
		writeJSON(w, http.StatusBadGateway, result)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":              true,
		"response":        result.Response,
		"thinking":        result.Thinking,
		"conversation_id": result.ConversationID,
	})
}

type openAIMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type openAIChatRequest struct {
	Model          string          `json:"model"`
	Messages       []openAIMessage `json:"messages"`
	AgentID        string          `json:"agentId"`
	ConversationID string          `json:"conversation_id"`
	Thinking       bool            `json:"thinking"`
}

func (s *Server) handleOpenAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req openAIChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var prompt string
	mat := newImageMaterializer(defaultMaxImageBytes)
	defer mat.cleanup()

	var imagePaths []string
	for _, m := range req.Messages {
		if m.Role != "user" {
			continue
		}
		switch c := m.Content.(type) {
		case string:
			prompt = c
		case []interface{}:
			for _, part := range c {
				pm, ok := part.(map[string]interface{})
				if !ok {
					continue
				}
				if pm["type"] == "text" {
					if t, ok := pm["text"].(string); ok {
						prompt = t
					}
				}
				if pm["type"] == "image_url" {
					iu, ok := pm["image_url"].(map[string]interface{})
					if !ok {
						continue
					}
					u, ok := iu["url"].(string)
					if !ok || strings.TrimSpace(u) == "" {
						continue
					}
					p, err := resolveOpenAIImageURL(u, mat)
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					imagePaths = append(imagePaths, p)
				}
			}
		}
	}
	if strings.TrimSpace(prompt) == "" && len(imagePaths) == 0 {
		http.Error(w, "messages must include user text or images", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = "请描述这些图片"
	}
	agentID, model, err := s.resolveAgentModel(req.AgentID, req.Model)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := s.cdp.Chat(prompt, agentID, model, req.ConversationID, imagePaths, req.Thinking)
	if err != nil && !result.OK {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": result.Error})
		return
	}
	if !result.OK {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": result.Error})
		return
	}

	now := time.Now().Unix()
	msg := map[string]string{
		"role":    "assistant",
		"content": result.Response,
	}
	if result.Thinking != "" {
		msg["reasoning_content"] = result.Thinking
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%x", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": now,
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": msg,
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
		"conversation_id": result.ConversationID,
		"thinking":        result.Thinking,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func (s *Server) resolveAgentModel(agentID, model string) (string, string, error) {
	if strings.TrimSpace(agentID) == "" {
		agentID = s.cfg.AgentID
	}
	if strings.TrimSpace(model) == "" {
		model = s.cfg.Model
	}

	if strings.TrimSpace(agentID) != "" && strings.TrimSpace(model) != "" {
		return agentID, model, nil
	}

	detected, err := s.cdp.DetectChatContext(ChatContext{AgentID: agentID, Model: model})
	if err != nil {
		if strings.TrimSpace(agentID) == "" && strings.TrimSpace(model) == "" {
			return "", "", fmt.Errorf("agentId/model not configured and CDP auto-detect failed: %w (open Yuanbao chat page or set -agent/-model)", err)
		}
		if strings.TrimSpace(agentID) == "" {
			return "", "", fmt.Errorf("agentId required: CDP auto-detect failed: %w", err)
		}
		return "", "", fmt.Errorf("model required: CDP auto-detect failed: %w", err)
	}

	if strings.TrimSpace(agentID) == "" {
		agentID = detected.AgentID
	}
	if strings.TrimSpace(model) == "" {
		model = detected.Model
	}
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(model) == "" {
		return "", "", fmt.Errorf("agentId/model still empty after CDP auto-detect")
	}
	return agentID, model, nil
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

func (s *Server) resolveImagePaths(paths, base64Images []string, files []*multipart.FileHeader) ([]string, func(), error) {
	mat := newImageMaterializer(defaultMaxImageBytes)
	out := make([]string, 0, len(paths)+len(base64Images)+len(files))

	for _, b64 := range base64Images {
		p, err := mat.fromBase64(b64)
		if err != nil {
			mat.cleanup()
			return nil, nil, fmt.Errorf("images_base64: %w", err)
		}
		out = append(out, p)
	}

	for _, fh := range files {
		p, err := saveMultipartImage(mat, fh)
		if err != nil {
			mat.cleanup()
			return nil, nil, err
		}
		out = append(out, p)
	}

	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		low := strings.ToLower(p)
		if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
			path, err := mat.fromURL(p)
			if err != nil {
				mat.cleanup()
				return nil, nil, fmt.Errorf("image URL %q: %w", p, err)
			}
			out = append(out, path)
			continue
		}
		if strings.HasPrefix(low, "data:") {
			path, err := mat.fromBase64(p)
			if err != nil {
				mat.cleanup()
				return nil, nil, fmt.Errorf("image data URL: %w", err)
			}
			out = append(out, path)
			continue
		}
		path, err := mat.fromLocalPath(p)
		if err != nil {
			mat.cleanup()
			return nil, nil, fmt.Errorf("image path %q: %w", p, err)
		}
		out = append(out, path)
	}

	return out, mat.cleanup, nil
}

func saveMultipartImage(mat *imageMaterializer, fh *multipart.FileHeader) (string, error) {
	if fh == nil {
		return "", fmt.Errorf("empty multipart file")
	}
	if fh.Size > int64(mat.maxBytes) {
		return "", fmt.Errorf("file %q exceeds max size %d bytes", fh.Filename, mat.maxBytes)
	}
	f, err := fh.Open()
	if err != nil {
		return "", fmt.Errorf("open upload %q: %w", fh.Filename, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(mat.maxBytes)+1))
	if err != nil {
		return "", fmt.Errorf("read upload %q: %w", fh.Filename, err)
	}
	if len(data) > mat.maxBytes {
		return "", fmt.Errorf("file %q exceeds max size %d bytes", fh.Filename, mat.maxBytes)
	}
	ext := extFromURLPath(fh.Filename)
	if ct := fh.Header.Get("Content-Type"); ct != "" {
		if e := extFromMime(ct); e != ".img" {
			ext = e
		}
	}
	return mat.writeBytes(data, ext)
}

func parseThinkingForm(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}

func (s *Server) parseAPIChatRequest(r *http.Request) (prompt, conversationID string, thinking bool, agentID, model string, paths, base64Images []string, files []*multipart.FileHeader, err error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return "", "", false, "", "", nil, nil, nil, fmt.Errorf("parse multipart: %w", err)
		}
		prompt = r.FormValue("prompt")
		conversationID = r.FormValue("conversation_id")
		thinking = parseThinkingForm(r.FormValue("thinking"))
		agentID = r.FormValue("agentId")
		model = r.FormValue("model")
		if r.MultipartForm != nil {
			files = append(files, r.MultipartForm.File["images"]...)
			files = append(files, r.MultipartForm.File["image"]...)
		}
		return prompt, conversationID, thinking, agentID, model, nil, nil, files, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", "", false, "", "", nil, nil, nil, err
	}
	var req chatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", "", false, "", "", nil, nil, nil, err
	}
	return req.Prompt, req.ConversationID, req.Thinking, req.AgentID, req.Model, req.Images, req.ImagesBase64, nil, nil
}

func resolveOpenAIImageURL(raw string, mat *imageMaterializer) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty image URL")
	}
	low := strings.ToLower(raw)
	if strings.HasPrefix(low, "data:") {
		return mat.fromBase64(raw)
	}
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		return mat.fromURL(raw)
	}
	if strings.HasPrefix(low, "file:") {
		return mat.fromLocalPath(strings.TrimPrefix(raw, "file://"))
	}
	return "", fmt.Errorf("unsupported image_url: use http(s) or data: URL for remote deployment")
}

package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultMaxImageBytes = 20 << 20 // 20MB per image

// prepareImagePaths copies images outside %TEMP% into the system temp dir so
// yuanbao's readFile bridge allows access (workspace paths are forbidden).
func prepareImagePaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return paths, nil
	}
	tempDir := filepath.Clean(os.TempDir())
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimPrefix(p, "file://")
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("invalid image path %q: %w", p, err)
		}
		if !fileExists(abs) {
			return nil, fmt.Errorf("image not found: %s", abs)
		}
		if strings.EqualFold(filepath.Clean(abs), tempDir) || strings.HasPrefix(strings.ToLower(abs), strings.ToLower(tempDir+string(os.PathSeparator))) {
			out = append(out, abs)
			continue
		}
		dst := filepath.Join(tempDir, fmt.Sprintf("yuanbao_bridge_%d_%s", time.Now().UnixNano(), filepath.Base(abs)))
		if err := copyFile(abs, dst); err != nil {
			return nil, fmt.Errorf("copy image to temp: %w", err)
		}
		out = append(out, dst)
	}
	return out, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

type imageMaterializer struct {
	maxBytes int
	created  []string
}

func newImageMaterializer(maxBytes int) *imageMaterializer {
	if maxBytes <= 0 {
		maxBytes = defaultMaxImageBytes
	}
	return &imageMaterializer{maxBytes: maxBytes}
}

func (m *imageMaterializer) cleanup() {
	for _, p := range m.created {
		os.Remove(p)
	}
}

func (m *imageMaterializer) writeBytes(data []byte, ext string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("empty image data")
	}
	if len(data) > m.maxBytes {
		return "", fmt.Errorf("image exceeds max size %d bytes", m.maxBytes)
	}
	ext = normalizeImageExt(ext)
	dst := filepath.Join(os.TempDir(), fmt.Sprintf("yuanbao_bridge_%d%s", time.Now().UnixNano(), ext))
	if err := os.WriteFile(dst, data, 0600); err != nil {
		return "", err
	}
	m.created = append(m.created, dst)
	return dst, nil
}

func (m *imageMaterializer) fromBase64(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty base64 image")
	}
	var mime string
	payload := raw
	if strings.HasPrefix(raw, "data:") {
		parts := strings.SplitN(raw, ",", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid data URL")
		}
		mime = parts[0]
		payload = parts[1]
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return "", fmt.Errorf("invalid base64: %w", err)
		}
	}
	ext := extFromMime(mime)
	return m.writeBytes(data, ext)
}

func (m *imageMaterializer) fromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid image URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported image URL scheme: %s", u.Scheme)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download image: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(m.maxBytes)+1))
	if err != nil {
		return "", fmt.Errorf("read image body: %w", err)
	}
	if len(data) > m.maxBytes {
		return "", fmt.Errorf("image exceeds max size %d bytes", m.maxBytes)
	}
	ext := extFromMime(resp.Header.Get("Content-Type"))
	if ext == ".img" {
		ext = extFromURLPath(u.Path)
	}
	return m.writeBytes(data, ext)
}

func (m *imageMaterializer) fromLocalPath(p string) (string, error) {
	prepared, err := prepareImagePaths([]string{p})
	if err != nil {
		return "", err
	}
	if len(prepared) == 0 {
		return "", fmt.Errorf("no image path resolved")
	}
	// prepareImagePaths may copy to temp; track copies for cleanup.
	tempDir := strings.ToLower(filepath.Clean(os.TempDir()))
	for _, path := range prepared {
		low := strings.ToLower(path)
		if strings.HasPrefix(low, tempDir+string(os.PathSeparator)) && strings.Contains(path, "yuanbao_bridge_") {
			m.created = append(m.created, path)
		}
	}
	return prepared[0], nil
}

func normalizeImageExt(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ".png"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".heic":
		return strings.ToLower(ext)
	default:
		return ".png"
	}
}

func extFromMime(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = mime[:i]
	}
	switch mime {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/heic":
		return ".heic"
	default:
		if strings.HasPrefix(mime, "data:image/") {
			return extFromMime(strings.TrimPrefix(mime, "data:"))
		}
		return ".img"
	}
}

func extFromURLPath(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return ".img"
	}
	return normalizeImageExt(ext)
}

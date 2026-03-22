package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	goqr "github.com/skip2/go-qrcode"
)

// ── Utility helpers ───────────────────────────────────────────────────────────

func newUUID() string { return uuid.New().String() }

func getLocalIP() string {
	ifaces, _ := net.Interfaces()
	var fallback string

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				// Prefer 192.168.x.x range (real network)
				if ip4[0] == 192 && ip4[1] == 168 {
					return ip4.String()
				}
				// Otherwise use 10.x.x.x
				if ip4[0] == 10 {
					if fallback == "" {
						fallback = ip4.String()
					}
					return ip4.String()
				}
				// Save as fallback but keep looking
				if fallback == "" {
					fallback = ip4.String()
				}
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	return "localhost"
}

// extractIPv4 returns the first IPv4 address from an interface
func extractIPv4(iface net.Interface) string {
	addrs, _ := iface.Addrs()
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	return ""
}

// copyDefaultTemplates seeds the on-disk templates directory from the embedded
// defaults, skipping files that already exist.
func copyDefaultTemplates() {
	entries, _ := defaultTemplatesFS.ReadDir("templates")
	for _, e := range entries {
		dest := filepath.Join(cfg.TemplatesDir, e.Name())
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		data, _ := defaultTemplatesFS.ReadFile("templates/" + e.Name())
		os.WriteFile(dest, data, 0644)
	}
}

// loadTemplateFile reads and parses a template by its file-stem name.
// Returns nil when the name is invalid or the file cannot be parsed.
func loadTemplateFile(name string) *templateFile {
	safe := safeNameRe.ReplaceAllString(name, "")
	if safe == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(cfg.TemplatesDir, safe+".json"))
	if err != nil {
		return nil
	}
	var t templateFile
	if err := json.Unmarshal(data, &t); err != nil {
		return nil
	}
	return &t
}

// ── Template API types ────────────────────────────────────────────────────────

// tmplResponse is the shape returned by GET /api/templates.
type tmplResponse struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Modules     json.RawMessage `json:"modules,omitempty"`
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

func handleQR(w http.ResponseWriter, r *http.Request) {
	ip := getLocalIP()
	url := fmt.Sprintf("http://%s:%d", ip, cfg.Port)
	png, err := goqr.Encode(url, goqr.Medium, 256)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "QR generation failed")
		return
	}
	b64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	writeJSON(w, http.StatusOK, map[string]string{"url": url, "qr": b64})
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	maxBytes := int64(cfg.MaxUploadMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or bad request")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "no image provided")
		return
	}
	defer file.Close()

	// Verify MIME type via the Content-Type header; fall back to sniffing.
	ct := header.Header.Get("Content-Type")
	if ct == "" {
		buf := make([]byte, 512)
		n, _ := file.Read(buf)
		ct = http.DetectContentType(buf[:n])
		file.Seek(0, io.SeekStart)
	}
	if !strings.HasPrefix(ct, "image/") {
		writeError(w, http.StatusBadRequest, "only image files are allowed")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
			ext = exts[0]
		} else {
			ext = ".jpg"
		}
	}
	filename := newUUID() + ext
	dst, err := os.Create(filepath.Join(cfg.UploadsDir, filename))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"filename": filename,
		"url":      "/" + cfg.UploadsDir + "/" + filename,
	})
}

func handleTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetTemplates(w, r)
	case http.MethodPost:
		handleSaveTemplate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetTemplates(w http.ResponseWriter, _ *http.Request) {
	entries, _ := os.ReadDir(cfg.TemplatesDir)
	results := make([]tmplResponse, 0)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cfg.TemplatesDir, e.Name()))
		if err != nil {
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		tr := tmplResponse{ID: strings.TrimSuffix(e.Name(), ".json")}
		if v, ok := raw["name"]; ok {
			json.Unmarshal(v, &tr.Name)
		}
		if tr.Name == "" {
			tr.Name = tr.ID
		}
		if v, ok := raw["description"]; ok {
			json.Unmarshal(v, &tr.Description)
		}
		if v, ok := raw["modules"]; ok {
			tr.Modules = v
		}
		results = append(results, tr)
	}
	writeJSON(w, http.StatusOK, results)
}

func handleSaveTemplate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Modules     json.RawMessage `json:"modules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "template name required")
		return
	}
	safeName := safeNameRe.ReplaceAllString(body.Name, "_")
	if safeName == "" {
		safeName = "custom"
	}
	payload := map[string]interface{}{
		"name":        body.Name,
		"description": body.Description,
		"modules":     body.Modules,
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(filepath.Join(cfg.TemplatesDir, safeName+".json"), data, 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save template")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"success": "true", "name": safeName})
}

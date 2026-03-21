package main

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	goqr "github.com/skip2/go-qrcode"
	"golang.org/x/time/rate"
)

//go:embed public
var publicFS embed.FS

//go:embed templates
var defaultTemplatesFS embed.FS

const (
	appPort      = 3000
	uploadsDir   = "uploads"
	templatesDir = "templates"
	maxUploadB   = 5 << 20 // 5 MB
)

// safeNameRe strips characters that are unsafe for file-system names.
var safeNameRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// ── Rate limiting ─────────────────────────────────────────────────────────────

// ipLimiter maps a client IP address to its per-IP rate limiter.
type ipLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

func newIPLimiter() *ipLimiter {
	return &ipLimiter{limiters: make(map[string]*rate.Limiter)}
}

func (l *ipLimiter) get(ip string, r rate.Limit, b int) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lim, ok := l.limiters[ip]; ok {
		return lim
	}
	lim := rate.NewLimiter(r, b)
	l.limiters[ip] = lim
	return lim
}

var (
	generalLimiter = newIPLimiter() // 200 req/min
	uploadLimiter  = newIPLimiter() // 30 req/min
)

// rateLimit wraps an HTTP handler with per-IP rate limiting.
func rateLimit(next http.HandlerFunc, lim *ipLimiter, r rate.Limit, b int) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ip, _, _ := net.SplitHostPort(req.RemoteAddr)
		if !lim.get(ip, r, b).Allow() {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next(w, req)
	}
}

// ── HTTP response helpers ─────────────────────────────────────────────────────

// writeJSON serialises v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error object with the given status code.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newUUID() string { return uuid.New().String() }

func getLocalIP() string {
	ifaces, _ := net.Interfaces()
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
				return ip4.String()
			}
		}
	}
	return "localhost"
}

// copyDefaultTemplates seeds the on-disk templates directory from the embedded
// defaults, skipping files that already exist.
func copyDefaultTemplates() {
	entries, _ := defaultTemplatesFS.ReadDir("templates")
	for _, e := range entries {
		dest := filepath.Join(templatesDir, e.Name())
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
	data, err := os.ReadFile(filepath.Join(templatesDir, safe+".json"))
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
	url := fmt.Sprintf("http://%s:%d", ip, appPort)
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
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadB)
	if err := r.ParseMultipartForm(maxUploadB); err != nil {
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
	dst, err := os.Create(filepath.Join(uploadsDir, filename))
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
		"url":      "/uploads/" + filename,
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
	entries, _ := os.ReadDir(templatesDir)
	results := make([]tmplResponse, 0)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(templatesDir, e.Name()))
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
	if err := os.WriteFile(filepath.Join(templatesDir, safeName+".json"), data, 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save template")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"success": "true", "name": safeName})
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	os.MkdirAll(uploadsDir, 0755)
	os.MkdirAll(templatesDir, 0755)
	copyDefaultTemplates()

	hub := newHub()
	game := newGameState()

	mux := http.NewServeMux()

	// Convenience wrappers for the two rate-limit tiers.
	genRL := func(h http.HandlerFunc) http.HandlerFunc {
		return rateLimit(h, generalLimiter, rate.Every(time.Minute/200), 200)
	}
	upRL := func(h http.HandlerFunc) http.HandlerFunc {
		return rateLimit(h, uploadLimiter, rate.Every(time.Minute/30), 30)
	}

	// WebSocket
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(hub, game, w, r)
	})

	// REST API
	mux.HandleFunc("/qr", genRL(handleQR))
	mux.HandleFunc("/upload", upRL(handleUpload))
	mux.HandleFunc("/api/templates", genRL(handleTemplates))

	// On-disk uploads
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsDir))))

	// Extension-free HTML routes served from the embedded public/ FS.
	pubFS, _ := fs.Sub(publicFS, "public")
	serveHTML := func(name string) http.HandlerFunc {
		return genRL(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFileFS(w, r, pubFS, name)
		})
	}
	mux.HandleFunc("/gm", serveHTML("gm.html"))
	mux.HandleFunc("/operator", serveHTML("operator.html"))
	mux.HandleFunc("/overlay", serveHTML("overlay.html"))

	// Everything else (CSS, JS, images, index.html, player.html…)
	mux.Handle("/", http.FileServer(http.FS(pubFS)))

	ip := getLocalIP()
	log.Printf("\n🎮  OmniCast Live Engine")
	log.Printf("   Local:    http://localhost:%d", appPort)
	log.Printf("   Network:  http://%s:%d", ip, appPort)
	log.Printf("   GM:       http://%s:%d/gm", ip, appPort)
	log.Printf("   Operator: http://%s:%d/operator", ip, appPort)
	log.Printf("   Overlay:  http://%s:%d/overlay", ip, appPort)
	log.Printf("   Players:  http://%s:%d  (scan QR)\n", ip, appPort)

	srv := &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%d", appPort),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

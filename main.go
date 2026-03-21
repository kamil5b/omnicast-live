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

// ── Rate limiting ─────────────────────────────────────────────────────────────

type ipEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	generalLimiters sync.Map // ip → *ipEntry  (200 req/min)
	uploadLimiters  sync.Map // ip → *ipEntry  (30 req/min)
)

func getLimiter(m *sync.Map, ip string, r rate.Limit, b int) *rate.Limiter {
	v, _ := m.LoadOrStore(ip, &ipEntry{
		limiter:  rate.NewLimiter(r, b),
		lastSeen: time.Now(),
	})
	e := v.(*ipEntry)
	e.lastSeen = time.Now()
	return e.limiter
}

func withRateLimit(next http.HandlerFunc, m *sync.Map, r rate.Limit, b int) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ip, _, _ := net.SplitHostPort(req.RemoteAddr)
		if !getLimiter(m, ip, r, b).Allow() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limit exceeded"}`))
			return
		}
		next(w, req)
	}
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

// copyDefaultTemplates copies embedded templates to disk on first run.
func copyDefaultTemplates() {
	entries, _ := defaultTemplatesFS.ReadDir("templates")
	for _, e := range entries {
		dest := filepath.Join(templatesDir, e.Name())
		if _, err := os.Stat(dest); err == nil {
			continue // already exists
		}
		data, _ := defaultTemplatesFS.ReadFile("templates/" + e.Name())
		os.WriteFile(dest, data, 0644)
	}
}

// loadTemplateFile reads a template from disk by its file-based id.
func loadTemplateFile(name string) *templateFile {
	// Sanitise: only allow safe characters
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	safe := re.ReplaceAllString(name, "")
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

// ── HTTP handlers ─────────────────────────────────────────────────────────────

func handleQR(w http.ResponseWriter, r *http.Request) {
	ip := getLocalIP()
	url := fmt.Sprintf("http://%s:%d", ip, appPort)
	png, err := goqr.Encode(url, goqr.Medium, 256)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"QR generation failed"}`))
		return
	}
	b64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": url, "qr": b64})
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadB)
	if err := r.ParseMultipartForm(maxUploadB); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"File too large or bad request"}`))
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"No image provided"}`))
		return
	}
	defer file.Close()

	// Verify MIME type (from header and content sniff)
	ct := header.Header.Get("Content-Type")
	if ct == "" {
		buf := make([]byte, 512)
		n, _ := file.Read(buf)
		ct = http.DetectContentType(buf[:n])
		file.Seek(0, io.SeekStart)
	}
	if !strings.HasPrefix(ct, "image/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"Only image files are allowed"}`))
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		// Derive from mime type
		exts, _ := mime.ExtensionsByType(ct)
		if len(exts) > 0 {
			ext = exts[0]
		} else {
			ext = ".jpg"
		}
	}
	filename := newUUID() + ext
	dst, err := os.Create(filepath.Join(uploadsDir, filename))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"Failed to save file"}`))
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"Failed to write file"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"filename": filename, "url": "/uploads/" + filename})
}

func handleTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		entries, _ := os.ReadDir(templatesDir)
		type tmplResponse struct {
			ID          string          `json:"id"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Modules     json.RawMessage `json:"modules,omitempty"`
		}
		var results []tmplResponse
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			id := strings.TrimSuffix(e.Name(), ".json")
			data, err := os.ReadFile(filepath.Join(templatesDir, e.Name()))
			if err != nil {
				continue
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				continue
			}
			tr := tmplResponse{ID: id}
			if v, ok := raw["name"]; ok {
				json.Unmarshal(v, &tr.Name)
			}
			if tr.Name == "" {
				tr.Name = id
			}
			if v, ok := raw["description"]; ok {
				json.Unmarshal(v, &tr.Description)
			}
			if v, ok := raw["modules"]; ok {
				tr.Modules = v
			}
			results = append(results, tr)
		}
		if results == nil {
			results = []tmplResponse{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)

	case http.MethodPost:
		var body struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Modules     json.RawMessage `json:"modules"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"Template name required"}`))
			return
		}
		re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
		safeName := re.ReplaceAllString(body.Name, "_")
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"Failed to save template"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"success": "true", "name": safeName})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	// Prepare runtime directories
	os.MkdirAll(uploadsDir, 0755)
	os.MkdirAll(templatesDir, 0755)
	copyDefaultTemplates()

	hub := newHub()
	game := newGameState()

	mux := http.NewServeMux()

	// Rate limit helpers
	genLimits := func(h http.HandlerFunc) http.HandlerFunc {
		return withRateLimit(h, &generalLimiters, rate.Every(time.Minute/200), 200)
	}
	upLimits := func(h http.HandlerFunc) http.HandlerFunc {
		return withRateLimit(h, &uploadLimiters, rate.Every(time.Minute/30), 30)
	}

	// WebSocket endpoint
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(hub, game, w, r)
	})

	// REST API
	mux.HandleFunc("/qr", genLimits(handleQR))
	mux.HandleFunc("/upload", upLimits(handleUpload))
	mux.HandleFunc("/api/templates", genLimits(handleTemplates))

	// Uploads on disk
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadsDir))))

	// Explicit HTML page routes (extension-free URLs → .html files in embed)
	pubFS, _ := fs.Sub(publicFS, "public")
	serveHTML := func(name string) http.HandlerFunc {
		return genLimits(func(w http.ResponseWriter, r *http.Request) {
			f, err := pubFS.Open(name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			f.Close()
			http.ServeFileFS(w, r, pubFS, name)
		})
	}
	mux.HandleFunc("/gm", serveHTML("gm.html"))
	mux.HandleFunc("/operator", serveHTML("operator.html"))
	mux.HandleFunc("/overlay", serveHTML("overlay.html"))

	// All other static assets (CSS, JS, images, player.html, index.html…)
	mux.Handle("/", http.FileServer(http.FS(pubFS)))

	addr := fmt.Sprintf("0.0.0.0:%d", appPort)
	ip := getLocalIP()
	log.Printf("\n🎮  OmniCast Live Engine (Go)")
	log.Printf("   Local:    http://localhost:%d", appPort)
	log.Printf("   Network:  http://%s:%d", ip, appPort)
	log.Printf("   GM:       http://%s:%d/gm", ip, appPort)
	log.Printf("   Operator: http://%s:%d/operator", ip, appPort)
	log.Printf("   Overlay:  http://%s:%d/overlay", ip, appPort)
	log.Printf("   Players:  http://%s:%d  (scan QR)\n", ip, appPort)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

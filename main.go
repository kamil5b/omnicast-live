package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"

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

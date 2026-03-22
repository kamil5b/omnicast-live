package main

import (
	"flag"
	"os"
	"strconv"
)

// Config holds all runtime-configurable values for the server.
// Precedence (highest → lowest): CLI flag > environment variable > default.
type Config struct {
	Port             int    // OMNICAST_PORT           --port
	UploadsDir       string // OMNICAST_UPLOADS_DIR    --uploads-dir
	TemplatesDir     string // OMNICAST_TEMPLATES_DIR  --templates-dir
	MaxUploadMB      int    // OMNICAST_MAX_UPLOAD_MB  --max-upload-mb
	GeneralRateLimit int    // OMNICAST_GENERAL_RL     --general-rate-limit
	UploadRateLimit  int    // OMNICAST_UPLOAD_RL      --upload-rate-limit
}

func loadConfig() *Config {
	cfg := &Config{}

	flag.IntVar(&cfg.Port, "port", envInt("OMNICAST_PORT", 3000), "TCP port the HTTP server listens on")
	flag.StringVar(&cfg.UploadsDir, "uploads-dir", envStr("OMNICAST_UPLOADS_DIR", "uploads"), "Directory for uploaded avatar images")
	flag.StringVar(&cfg.TemplatesDir, "templates-dir", envStr("OMNICAST_TEMPLATES_DIR", "templates"), "Directory for game template JSON files")
	flag.IntVar(&cfg.MaxUploadMB, "max-upload-mb", envInt("OMNICAST_MAX_UPLOAD_MB", 5), "Maximum upload size in megabytes")
	flag.IntVar(&cfg.GeneralRateLimit, "general-rate-limit", envInt("OMNICAST_GENERAL_RL", 200), "General endpoint rate limit (requests per minute, per IP)")
	flag.IntVar(&cfg.UploadRateLimit, "upload-rate-limit", envInt("OMNICAST_UPLOAD_RL", 30), "Upload endpoint rate limit (requests per minute, per IP)")

	flag.Parse()

	return cfg
}

// envStr returns the value of an environment variable, or fallback if unset.
func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envInt returns the integer value of an environment variable, or fallback if
// unset or unparseable.
func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

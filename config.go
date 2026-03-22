package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

// loadConfig builds a Config by parsing CLI flags (via Cobra) and environment
// variables (via Viper).  It calls os.Exit(1) on any usage or binding error so
// callers can treat the returned value as always valid.
func loadConfig() *Config {
	var cfg Config

	root := &cobra.Command{
		Use:   "omnicast-live",
		Short: "OmniCast Live Engine — real-time live-game server",
		// SilenceUsage stops Cobra printing the usage block on every error.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Bind every flag to Viper so that env vars can also override them.
			if err := viper.BindPFlags(cmd.Flags()); err != nil {
				return fmt.Errorf("binding flags: %w", err)
			}
			return nil
		},
	}

	// ── CLI flags with defaults ───────────────────────────────────────────────
	root.Flags().Int("port", 3000, "TCP port the HTTP server listens on")
	root.Flags().String("uploads-dir", "uploads", "Directory for uploaded avatar images")
	root.Flags().String("templates-dir", "templates", "Directory for game template JSON files")
	root.Flags().Int("max-upload-mb", 5, "Maximum upload size in megabytes")
	root.Flags().Int("general-rate-limit", 200, "General endpoint rate limit (requests per minute, per IP)")
	root.Flags().Int("upload-rate-limit", 30, "Upload endpoint rate limit (requests per minute, per IP)")

	// ── Viper environment variable bindings ───────────────────────────────────
	viper.SetEnvPrefix("OMNICAST")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("port", 3000)
	viper.SetDefault("uploads-dir", "uploads")
	viper.SetDefault("templates-dir", "templates")
	viper.SetDefault("max-upload-mb", 5)
	viper.SetDefault("general-rate-limit", 200)
	viper.SetDefault("upload-rate-limit", 30)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}

	// ── Populate struct ───────────────────────────────────────────────────────
	cfg.Port = viper.GetInt("port")
	cfg.UploadsDir = viper.GetString("uploads-dir")
	cfg.TemplatesDir = viper.GetString("templates-dir")
	cfg.MaxUploadMB = viper.GetInt("max-upload-mb")
	cfg.GeneralRateLimit = viper.GetInt("general-rate-limit")
	cfg.UploadRateLimit = viper.GetInt("upload-rate-limit")

	return &cfg
}

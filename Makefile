# ──────────────────────────────────────────────────────────────────────────────
# OmniCast Live — Cross-platform Build
# ──────────────────────────────────────────────────────────────────────────────

APP     := omnicast-live
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

OUT_DIR := dist

# ──────────────────────────────────────────────────────────────────────────────
# Default target
# ──────────────────────────────────────────────────────────────────────────────

.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo ""
	@echo "  OmniCast Live build targets"
	@echo ""
	@echo "  make all              Build all platforms"
	@echo "  make windows          Build Windows   x64 + arm64"
	@echo "  make linux            Build Linux     x64 + arm64"
	@echo "  make darwin           Build macOS     x64 + arm64"
	@echo ""
	@echo "  make windows-amd64    Single-platform shortcuts"
	@echo "  make windows-arm64"
	@echo "  make linux-amd64"
	@echo "  make linux-arm64"
	@echo "  make darwin-amd64"
	@echo "  make darwin-arm64"
	@echo ""
	@echo "  make run              Run locally (native)"
	@echo "  make clean            Remove $(OUT_DIR)/"
	@echo ""

# ──────────────────────────────────────────────────────────────────────────────
# Build rules
# ──────────────────────────────────────────────────────────────────────────────

.PHONY: all
all: windows linux darwin

# ── Windows ───────────────────────────────────────────────────────────────────

.PHONY: windows windows-amd64 windows-arm64

windows: windows-amd64 windows-arm64

windows-amd64:
	@mkdir -p $(OUT_DIR)
	@echo "  →  $(OUT_DIR)/$(APP)-windows-amd64.exe"
	@CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
	 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(APP)-windows-amd64.exe .

windows-arm64:
	@mkdir -p $(OUT_DIR)
	@echo "  →  $(OUT_DIR)/$(APP)-windows-arm64.exe"
	@CGO_ENABLED=0 GOOS=windows GOARCH=arm64 \
	 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(APP)-windows-arm64.exe .

# ── Linux ─────────────────────────────────────────────────────────────────────

.PHONY: linux linux-amd64 linux-arm64

linux: linux-amd64 linux-arm64

linux-amd64:
	@mkdir -p $(OUT_DIR)
	@echo "  →  $(OUT_DIR)/$(APP)-linux-amd64"
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(APP)-linux-amd64 .

linux-arm64:
	@mkdir -p $(OUT_DIR)
	@echo "  →  $(OUT_DIR)/$(APP)-linux-arm64"
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
	 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(APP)-linux-arm64 .

# ── macOS ─────────────────────────────────────────────────────────────────────

.PHONY: darwin darwin-amd64 darwin-arm64

darwin: darwin-amd64 darwin-arm64

darwin-amd64:
	@mkdir -p $(OUT_DIR)
	@echo "  →  $(OUT_DIR)/$(APP)-darwin-amd64"
	@CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 \
	 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(APP)-darwin-amd64 .

darwin-arm64:
	@mkdir -p $(OUT_DIR)
	@echo "  →  $(OUT_DIR)/$(APP)-darwin-arm64"
	@CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
	 go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(APP)-darwin-arm64 .

# ──────────────────────────────────────────────────────────────────────────────
# Dev helpers
# ──────────────────────────────────────────────────────────────────────────────

.PHONY: run
run:
	go run .

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: clean
clean:
	@rm -rf $(OUT_DIR)
	@echo "  cleaned $(OUT_DIR)/"

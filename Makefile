# ──────────────────────────────────────────────────────────────────────────────
# OmniCast Live — Cross-platform Build
# ──────────────────────────────────────────────────────────────────────────────

APP     := omnicast-live
MODULE  := $(shell go env GOMODULE 2>/dev/null || head -1 go.mod | awk '{print $$2}')
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

OUT_DIR := dist

# Supported targets: OS/ARCH pairs
TARGETS := \
	windows/amd64 \
	windows/arm64 \
	linux/amd64   \
	linux/arm64   \
	darwin/amd64  \
	darwin/arm64

# ──────────────────────────────────────────────────────────────────────────────
# Helpers
# ──────────────────────────────────────────────────────────────────────────────

# bin_name <os> <arch>  → e.g. omnicast-live-linux-arm64  (+ .exe on windows)
define bin_name
$(OUT_DIR)/$(APP)-$(1)-$(2)$(if $(filter windows,$(1)),.exe,)
endef

# ──────────────────────────────────────────────────────────────────────────────
# Default target
# ──────────────────────────────────────────────────────────────────────────────

.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo ""
	@echo "  OmniCast Live build targets"
	@echo ""
	@echo "  make all              Build all platforms ($(words $(TARGETS)) binaries)"
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
all: $(TARGETS:%=build-%)

# Generic pattern rule:  build-<os>/<arch>
.PHONY: build-%
build-%:
	$(eval OS   := $(word 1,$(subst /, ,$*)))
	$(eval ARCH := $(word 2,$(subst /, ,$*)))
	$(eval BIN  := $(call bin_name,$(OS),$(ARCH)))
	@mkdir -p $(OUT_DIR)
	@echo "  →  $(BIN)"
	@CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) \
		go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) .

# ── OS-group shortcuts ────────────────────────────────────────────────────────

.PHONY: windows
windows: build-windows/amd64 build-windows/arm64

.PHONY: linux
linux: build-linux/amd64 build-linux/arm64

.PHONY: darwin
darwin: build-darwin/amd64 build-darwin/arm64

# ── Single-target shortcuts ───────────────────────────────────────────────────

.PHONY: windows-amd64
windows-amd64: build-windows/amd64

.PHONY: windows-arm64
windows-arm64: build-windows/arm64

.PHONY: linux-amd64
linux-amd64: build-linux/amd64

.PHONY: linux-arm64
linux-arm64: build-linux/arm64

.PHONY: darwin-amd64
darwin-amd64: build-darwin/amd64

.PHONY: darwin-arm64
darwin-arm64: build-darwin/arm64

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

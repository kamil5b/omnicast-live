package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

func newUUID() string { return uuid.New().String() }

// extractIPv4 returns the first non-loopback IPv4 address from an interface,
// or an empty string if none is found.
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

func getLocalIP() string {
	ifaces, _ := net.Interfaces()
	var fallback string

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ip4str := extractIPv4(iface)
		if ip4str == "" {
			continue
		}
		ip4 := net.ParseIP(ip4str).To4()
		// Prefer 192.168.x.x (typical home/office LAN)
		if ip4[0] == 192 && ip4[1] == 168 {
			return ip4str
		}
		// Accept 10.x.x.x as a good candidate
		if ip4[0] == 10 {
			if fallback == "" {
				fallback = ip4str
			}
			return ip4str
		}
		// Any other routable IPv4 kept as last-resort fallback
		if fallback == "" {
			fallback = ip4str
		}
	}
	if fallback != "" {
		return fallback
	}
	return "localhost"
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

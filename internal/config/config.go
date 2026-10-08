package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime settings for kit-portal.
type Config struct {
	ConfigEnvPath string
	XUIEnvPath    string
	DBPath        string
	StaticDir     string

	PortalPort   int
	PortalListen string
	Domain       string
	PortalURL    string
	WSDomain     string

	SubBase     string
	SubPath     string
	SubInternal string
	Single      string

	XUIPanelPort    string
	XUIWebBasePath  string
	XUIAPIToken     string
}

// Load reads environment variables and system env files (/etc/kit/kit.env, /etc/x-ui/install-result.env).
func Load() *Config {
	cfgEnvPath := getEnvDefault("KIT_ENV", "/etc/kit/kit.env")
	xuiEnvPath := getEnvDefault("XUI_ENV", "/etc/x-ui/install-result.env")
	dbPath := getEnvDefault("PORTAL_DB", "/etc/kit/portal.db")
	staticDir := getEnvDefault("PORTAL_STATIC", "")

	envMap := map[string]string{
		"HOST":              "127.0.0.1",
		"PORTAL_DOMAIN":     "",
		"PORTAL_PORT":       "10465",
		"PORTAL_LISTEN":     "127.0.0.1",
		"WS_DOMAIN":         "",
		"SUB_BASE":          "",
		"SUB_PATH":          "/sub/",
		"SUB_INTERNAL":      "2097",
		"SINGLE":            "yes",
		"XUI_PANEL_PORT":    "2053",
		"XUI_WEB_BASE_PATH": "xui",
		"XUI_API_TOKEN":     "",
	}

	readEnvFile(cfgEnvPath, envMap)
	readEnvFile(xuiEnvPath, envMap)

	// Environment variable overrides
	for k := range envMap {
		if val := os.Getenv(k); val != "" {
			envMap[k] = val
		}
	}

	portalPort := 10465
	if p, err := strconv.Atoi(envMap["PORTAL_PORT"]); err == nil && p > 0 {
		portalPort = p
	}

	portalListen := envMap["PORTAL_LISTEN"]
	if portalListen == "" {
		portalListen = "127.0.0.1"
	}

	domain := envMap["PORTAL_DOMAIN"]
	if domain == "" {
		domain = envMap["DOMAIN"]
	}
	if domain == "" {
		domain = envMap["HOST"]
	}
	if domain == "" {
		domain = "127.0.0.1"
	}

	portalURL := "https://" + domain

	return &Config{
		ConfigEnvPath:   cfgEnvPath,
		XUIEnvPath:      xuiEnvPath,
		DBPath:          dbPath,
		StaticDir:       staticDir,
		PortalPort:      portalPort,
		PortalListen:    portalListen,
		Domain:          domain,
		PortalURL:       portalURL,
		WSDomain:        envMap["WS_DOMAIN"],
		SubBase:         envMap["SUB_BASE"],
		SubPath:         envMap["SUB_PATH"],
		SubInternal:     envMap["SUB_INTERNAL"],
		Single:          envMap["SINGLE"],
		XUIPanelPort:    envMap["XUI_PANEL_PORT"],
		XUIWebBasePath:  envMap["XUI_WEB_BASE_PATH"],
		XUIAPIToken:     envMap["XUI_API_TOKEN"],
	}
}

func readEnvFile(path string, target map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		v = strings.Trim(v, `"'`)
		target[k] = v
	}
}

func getEnvDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

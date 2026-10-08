package server

import (
	"crypto/tls"
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
	"strconv"
	"strings"
	"time"

	"github.com/itsnotkubrick/3X-UI_KIT/internal/auth"
	"github.com/itsnotkubrick/3X-UI_KIT/internal/config"
	"github.com/itsnotkubrick/3X-UI_KIT/internal/db"
	"github.com/itsnotkubrick/3X-UI_KIT/internal/proxy"
	"github.com/itsnotkubrick/3X-UI_KIT/internal/singbox"
	"github.com/itsnotkubrick/3X-UI_KIT/internal/sub"
	"github.com/itsnotkubrick/3X-UI_KIT/portal"
)

// Server implements HTTP handlers for kit-portal and subscriptions.
type Server struct {
	cfg        *config.Config
	database   *db.DB
	subClient  *sub.Client
	httpClient *http.Client
	httpServer *http.Server
}

// New creates a new Server instance.
func New(cfg *config.Config, database *db.DB) *Server {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{
		Timeout:   10 * time.Second,
		Transport: tr,
	}

	return &Server{
		cfg:        cfg,
		database:   database,
		subClient:  sub.NewClient(15 * time.Second),
		httpClient: httpClient,
	}
}

// Handler returns the configured HTTP handler with middleware and routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return s.securityMiddleware(mux)
}

// ListenAndServe runs the HTTP server.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.PortalListen, s.cfg.PortalPort)
	log.Printf("[*] kit-portal слушает http://%s (домен: %s)", addr, s.cfg.Domain)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s.httpServer.ListenAndServe()
}

func (s *Server) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "nginx")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/portal/info", s.handleMe)
	mux.HandleFunc("/api/sub/token/rotate", s.handleRotateToken)

	mux.HandleFunc("/sub/singbox", s.handleSingboxSub)
	subPrefix := "/" + strings.Trim(s.cfg.SubPath, "/") + "/"
	mux.HandleFunc(subPrefix, s.handleClientSub)

	mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) getClientIP(r *http.Request) string {
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return strings.TrimSpace(realIP)
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) getAuthUser(r *http.Request) *db.User {
	cookie, err := r.Cookie("kit_session")
	if err != nil || cookie.Value == "" {
		return nil
	}
	user, err := s.database.GetUserFromSession(cookie.Value)
	if err != nil {
		return nil
	}
	return user
}

func (s *Server) sendJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) sendError(w http.ResponseWriter, code int, message string) {
	s.sendJSON(w, code, map[string]interface{}{"error": message})
}

// handleLogin handles POST /api/login.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ip := s.getClientIP(r)

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		s.sendError(w, http.StatusBadRequest, "Заполните логин и пароль")
		return
	}

	if s.database.IsRateLimited(ip, req.Username) {
		s.sendError(w, http.StatusTooManyRequests, "Слишком много неудачных попыток. Подождите 5 минут.")
		return
	}

	user, err := s.database.GetUser(req.Username)
	if err != nil || user == nil || user.IsActive != 1 || !auth.VerifyPassword(req.Password, user.Salt, user.PasswordHash) {
		s.database.RecordLoginAttempt(ip, req.Username)
		s.sendError(w, http.StatusUnauthorized, "Неверный логин или пароль")
		return
	}

	s.database.ClearLoginAttempts(ip, req.Username)
	sessionID, err := s.database.CreateSession(user.Username)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "Ошибка создания сессии")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "kit_session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   86400 * 30,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"username": user.Username,
	})
}

// handleLogout handles POST /api/logout.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("kit_session"); err == nil && cookie.Value != "" {
		_ = s.database.DeleteSession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "kit_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	s.sendJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// handleMe handles GET /api/me and /api/portal/info.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil {
		s.sendError(w, http.StatusUnauthorized, "Требуется авторизация")
		return
	}

	stats, _ := s.get3XUIClientInfo(user.Username)
	rawSub := s.fetchRawSubscription(user.SubID)

	subPath := "/" + strings.Trim(s.cfg.SubPath, "/") + "/"
	universalSubURL := fmt.Sprintf("https://%s%s%s", s.cfg.Domain, subPath, user.SubID)
	singboxSubURL := fmt.Sprintf("https://%s/sub/singbox?token=%s", s.cfg.Domain, user.SubToken)

	var parsedProxies []proxy.ProxyInfo
	var cleanLines []string
	lines := strings.Split(rawSub, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "tg://") {
			continue
		}
		cleanLines = append(cleanLines, l)
		if info := proxy.ParseProxyInfo(l); info != nil {
			parsedProxies = append(parsedProxies, *info)
		}
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"user": map[string]interface{}{
			"username":   user.Username,
			"sub_id":     user.SubID,
			"sub_token":  user.SubToken,
			"created_at": user.CreatedAt,
		},
		"stats":             stats,
		"domain":            s.cfg.Domain,
		"universal_sub_url": universalSubURL,
		"singbox_sub_url":   singboxSubURL,
		"sub_url":           universalSubURL,
		"raw_subscription":  strings.Join(cleanLines, "\n"),
		"proxies":           parsedProxies,
	})
}

// handleRotateToken handles POST /api/sub/token/rotate.
func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	user := s.getAuthUser(r)
	if user == nil {
		s.sendError(w, http.StatusUnauthorized, "Требуется авторизация")
		return
	}

	newToken, err := s.database.RotateToken(user.Username)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "Ошибка ротации токена")
		return
	}

	singboxSubURL := fmt.Sprintf("https://%s/sub/singbox?token=%s", s.cfg.Domain, newToken)
	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"sub_token": newToken,
		"sub_url":   singboxSubURL,
	})
}

// handleSingboxSub handles GET /sub/singbox?token=...
func (s *Server) handleSingboxSub(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	var user *db.User
	var err error

	if token != "" {
		user, err = s.database.GetUserByToken(token)
		if err != nil || user == nil {
			s.sendError(w, http.StatusForbidden, "Недействительный или отозванный токен подписки")
			return
		}
	} else {
		user = s.getAuthUser(r)
		if user == nil {
			s.sendError(w, http.StatusForbidden, "Требуется токен подписки")
			return
		}
	}

	rawSub := s.fetchRawSubscription(user.SubID)
	cfg, err := singbox.GenerateConfig(rawSub)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка генерации конфига: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Profile-Title", fmt.Sprintf("%s (Sing-Box)", s.cfg.Domain))
	w.Header().Set("Profile-Update-Interval", "24")

	if stats, err := s.get3XUIClientInfo(user.Username); err == nil && len(stats) > 0 {
		up, _ := stats["up"].(float64)
		down, _ := stats["down"].(float64)
		totalGB, _ := stats["totalGB"].(float64)
		expiryTime, _ := stats["expiryTime"].(float64)

		total := int64(totalGB * 1073741824)
		exp := int64(expiryTime) / 1000
		if exp < 0 {
			exp = 0
		}
		w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", int64(up), int64(down), total, exp))
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(cfg)
}

// handleClientSub handles universal/clash subscription requests (/sub/<id>).
func (s *Server) handleClientSub(w http.ResponseWriter, r *http.Request) {
	subPrefix := "/" + strings.Trim(s.cfg.SubPath, "/") + "/"
	subID := strings.TrimPrefix(r.URL.Path, subPrefix)
	subID = strings.Trim(subID, "/")

	if !sub.ReSubID.MatchString(subID) {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	}

	ua := r.Header.Get("User-Agent")
	host := r.Host
	if host == "" {
		host = s.cfg.Domain
	}
	accept := r.Header.Get("Accept")

	baseURL := fmt.Sprintf("http://127.0.0.1:%s", s.cfg.SubInternal)
	resp, err := s.subClient.FetchUpstream(baseURL, s.cfg.SubPath, subID, ua, host, accept)
	if err != nil {
		http.Error(w, "subscription backend is unavailable", http.StatusBadGateway)
		return
	}

	body := resp.Body
	contentType := resp.Headers["content-type"]
	clash := sub.IsClashUA(ua) && strings.Contains(contentType, "yaml")
	canAWG := clash && sub.CanAmneziaWG(ua)

	if resp.StatusCode == http.StatusOK {
		if clash && !canAWG {
			if cleaned, err := sub.StripAWG(body); err == nil {
				body = cleaned
			}
		} else if clash && canAWG && !strings.HasSuffix(subID, "-awg") && !strings.HasSuffix(subID, "-tg") {
			awgResp, err := s.subClient.FetchUpstream(baseURL, s.cfg.SubPath, subID+"-awg", ua, host, accept)
			if err == nil && awgResp.StatusCode == http.StatusOK && len(awgResp.Body) > 0 {
				if merged, err := sub.MergeAWG(body, awgResp.Body); err == nil {
					body = merged
				}
			}
		} else if strings.Contains(contentType, "text/plain") {
			body = sub.StripLinks(body)
			if s.cfg.WSDomain != "" {
				body = sub.FixWSLinks(body, s.cfg.WSDomain)
			}
		}
	}

	for _, k := range sub.PassHeaders {
		if v, ok := resp.Headers[k]; ok {
			if k == "subscription-userinfo" {
				v = sub.FixUserinfo(v)
			}
			if v != "" && !strings.ContainsAny(v, "\r\n") {
				w.Header().Set(http.CanonicalHeaderKey(k), v)
			}
		}
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

// handleStatic serves portal SPA frontend.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/")
	if relPath == "" || relPath == "index.html" {
		relPath = "index.html"
	}

	// 1. Try disk directory if specified
	if s.cfg.StaticDir != "" {
		diskFile := filepath.Join(s.cfg.StaticDir, relPath)
		if fi, err := os.Stat(diskFile); err == nil && !fi.IsDir() {
			s.serveDiskFile(w, r, diskFile)
			return
		}
		// If requesting unknown path, fallback to index.html for SPA
		indexDisk := filepath.Join(s.cfg.StaticDir, "index.html")
		if _, err := os.Stat(indexDisk); err == nil {
			s.serveDiskFile(w, r, indexDisk)
			return
		}
	}

	// 2. Try embedded files
	data, err := fs.ReadFile(portal.FS, relPath)
	if err == nil {
		s.serveBytes(w, relPath, data)
		return
	}

	// SPA fallback
	data, err = fs.ReadFile(portal.FS, "index.html")
	if err == nil {
		s.serveBytes(w, "index.html", data)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) serveDiskFile(w http.ResponseWriter, r *http.Request, path string) {
	ctype := mime.TypeByExtension(filepath.Ext(path))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	http.ServeFile(w, r, path)
}

func (s *Server) serveBytes(w http.ResponseWriter, path string, data []byte) {
	ctype := mime.TypeByExtension(filepath.Ext(path))
	if ctype == "" {
		if strings.HasSuffix(path, ".html") {
			ctype = "text/html; charset=utf-8"
		} else if strings.HasSuffix(path, ".css") {
			ctype = "text/css; charset=utf-8"
		} else if strings.HasSuffix(path, ".js") {
			ctype = "application/javascript; charset=utf-8"
		} else {
			ctype = "application/octet-stream"
		}
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// get3XUIClientInfo fetches user traffic and expiry stats from 3X-UI API.
func (s *Server) get3XUIClientInfo(username string) (map[string]interface{}, error) {
	token := s.cfg.XUIAPIToken
	port := s.cfg.XUIPanelPort
	if port == "" {
		port = "2053"
	}
	basePath := strings.Trim(s.cfg.XUIWebBasePath, "/")
	prefix := ""
	if basePath != "" {
		prefix = "/" + basePath
	}

	for _, scheme := range []string{"https", "http"} {
		apiURL := fmt.Sprintf("%s://127.0.0.1:%s%s/panel/api/clients/list", scheme, port, prefix)
		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			continue
		}
		if token != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		var data struct {
			Success bool            `json:"success"`
			Obj     json.RawMessage `json:"obj"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || !data.Success {
			continue
		}

		var clients []map[string]interface{}
		if err := json.Unmarshal(data.Obj, &clients); err != nil {
			var wrapped struct {
				Clients []map[string]interface{} `json:"clients"`
			}
			if err := json.Unmarshal(data.Obj, &wrapped); err == nil {
				clients = wrapped.Clients
			}
		}

		for _, c := range clients {
			if email, ok := c["email"].(string); ok && email == username {
				traffic, _ := c["traffic"].(map[string]interface{})
				up := 0.0
				down := 0.0
				if traffic != nil {
					if u, ok := traffic["up"].(float64); ok {
						up = u
					}
					if d, ok := traffic["down"].(float64); ok {
						down = d
					}
				}
				totalGB, _ := c["totalGB"].(float64)
				expTime, _ := c["expiryTime"].(float64)
				enable, _ := c["enable"].(bool)
				subID, _ := c["subId"].(string)

				return map[string]interface{}{
					"email":      email,
					"subId":      subID,
					"enable":     enable,
					"totalGB":    totalGB,
					"expiryTime": expTime,
					"up":         up,
					"down":       down,
					"used":       up + down,
				}, nil
			}
		}
	}
	return nil, nil
}

func (s *Server) fetchSingleSub(subID string) string {
	if subID == "" {
		return ""
	}
	port := s.cfg.SubInternal
	if port == "" {
		port = "2097"
	}
	subPath := "/" + strings.Trim(s.cfg.SubPath, "/") + "/"
	host := s.cfg.Domain

	for _, scheme := range []string{"http", "https"} {
		reqURL := fmt.Sprintf("%s://127.0.0.1:%s%s%s", scheme, port, subPath, subID)
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "v2rayN/7.0")
		req.Host = host
		req.Header.Set("Accept", "*/*")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		text := strings.TrimSpace(string(body))
		if !strings.Contains(text, "://") {
			if decoded, err := decodeBase64Safe(text); err == nil {
				text = strings.TrimSpace(decoded)
			}
		}
		if text != "" {
			return text
		}
	}
	return ""
}

func (s *Server) fetchRawSubscription(subID string) string {
	var parts []string
	main := s.fetchSingleSub(subID)
	if main != "" {
		parts = append(parts, main)
	}

	if !strings.HasSuffix(subID, "-tg") && !strings.HasSuffix(subID, "-awg") {
		tg := s.fetchSingleSub(subID + "-tg")
		if tg != "" {
			parts = append(parts, tg)
		}
		awg := s.fetchSingleSub(subID + "-awg")
		if awg != "" {
			parts = append(parts, awg)
		}
	}

	full := strings.Join(parts, "\n")
	var fixed []string
	for _, l := range strings.Split(full, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "tg://") {
			l = proxy.FixTGLink(l, s.cfg.Domain)
		}
		if s.cfg.WSDomain != "" {
			l = proxy.FixWSLink(l, s.cfg.WSDomain)
		}
		fixed = append(fixed, l)
	}
	return strings.Join(fixed, "\n")
}

func decodeBase64Safe(s string) (string, error) {
	s = strings.TrimSpace(s)
	if rem := len(s) % 4; rem > 0 {
		s += strings.Repeat("=", 4-rem)
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(s)
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

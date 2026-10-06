package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsnotkubrick/3X-UI_KIT/internal/config"
	"github.com/itsnotkubrick/3X-UI_KIT/internal/db"
	"github.com/itsnotkubrick/3X-UI_KIT/internal/server"
)

func setupTestServer(t *testing.T) (*server.Server, *db.DB, *config.Config, func()) {
	tmpDir, err := os.MkdirTemp("", "kit-portal-server-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "portal.db")
	database, err := db.Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open test database: %v", err)
	}

	cfg := &config.Config{
		DBPath:       dbPath,
		Domain:       "vpn.example.com",
		PortalURL:    "https://vpn.example.com",
		PortalPort:   10465,
		PortalListen: "127.0.0.1",
		SubPath:      "/sub/",
		SubInternal:  "2097",
	}

	srv := server.New(cfg, database)

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return srv, database, cfg, cleanup
}

func TestStaticDelivery(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. GET / (should return index.html from embedded FS)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler := srv.Handler()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for root static, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") && !strings.Contains(body, "<html") {
		t.Errorf("expected HTML content in root response, got: %s", body[:min(len(body), 200)])
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("missing security header nosniff")
	}

	// 2. GET /style.css
	reqCSS := httptest.NewRequest("GET", "/style.css", nil)
	wCSS := httptest.NewRecorder()
	handler.ServeHTTP(wCSS, reqCSS)
	if wCSS.Code != http.StatusOK {
		t.Errorf("expected 200 for /style.css, got %d", wCSS.Code)
	}
	if !strings.Contains(wCSS.Header().Get("Content-Type"), "text/css") {
		t.Errorf("expected text/css content type, got %s", wCSS.Header().Get("Content-Type"))
	}
}

func TestAuthAndAPIWorkflow(t *testing.T) {
	srv, database, _, cleanup := setupTestServer(t)
	defer cleanup()

	handler := srv.Handler()

	// Seed user
	userRes, err := database.AddUser("alex", "sub-alex-123", "password123")
	if err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	// 1. Bad login attempt
	badLoginJSON := `{"username":"alex","password":"wrongPassword"}`
	reqBad := httptest.NewRequest("POST", "/api/login", bytes.NewBufferString(badLoginJSON))
	wBad := httptest.NewRecorder()
	handler.ServeHTTP(wBad, reqBad)
	if wBad.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for bad password, got %d", wBad.Code)
	}

	// 2. Good login attempt
	goodLoginJSON := `{"username":"alex","password":"password123"}`
	reqGood := httptest.NewRequest("POST", "/api/login", bytes.NewBufferString(goodLoginJSON))
	wGood := httptest.NewRecorder()
	handler.ServeHTTP(wGood, reqGood)
	if wGood.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid login, got %d", wGood.Code)
	}

	// Extract session cookie
	var sessionCookie *http.Cookie
	for _, c := range wGood.Result().Cookies() {
		if c.Name == "kit_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("kit_session cookie was not set")
	}

	// 3. GET /api/me without cookie (should return 401)
	reqMeUnauthorized := httptest.NewRequest("GET", "/api/me", nil)
	wMeUnauthorized := httptest.NewRecorder()
	handler.ServeHTTP(wMeUnauthorized, reqMeUnauthorized)
	if wMeUnauthorized.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated /api/me, got %d", wMeUnauthorized.Code)
	}

	// 4. GET /api/me with session cookie
	reqMe := httptest.NewRequest("GET", "/api/me", nil)
	reqMe.AddCookie(sessionCookie)
	wMe := httptest.NewRecorder()
	handler.ServeHTTP(wMe, reqMe)
	if wMe.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated /api/me, got %d", wMe.Code)
	}

	var meResp struct {
		Success bool `json:"success"`
		User    struct {
			Username string `json:"username"`
			SubID    string `json:"sub_id"`
			SubToken string `json:"sub_token"`
		} `json:"user"`
		UniversalSubURL string `json:"universal_sub_url"`
		SingboxSubURL   string `json:"singbox_sub_url"`
	}
	if err := json.Unmarshal(wMe.Body.Bytes(), &meResp); err != nil {
		t.Fatalf("failed to decode /api/me response: %v", err)
	}
	if !meResp.Success || meResp.User.Username != "alex" {
		t.Errorf("unexpected meResp: %+v", meResp)
	}

	// 5. Rotate sub token
	reqRotate := httptest.NewRequest("POST", "/api/sub/token/rotate", nil)
	reqRotate.AddCookie(sessionCookie)
	wRotate := httptest.NewRecorder()
	handler.ServeHTTP(wRotate, reqRotate)
	if wRotate.Code != http.StatusOK {
		t.Fatalf("expected 200 for rotate token, got %d", wRotate.Code)
	}

	var rotResp struct {
		Success  bool   `json:"success"`
		SubToken string `json:"sub_token"`
	}
	_ = json.Unmarshal(wRotate.Body.Bytes(), &rotResp)
	if !rotResp.Success || rotResp.SubToken == userRes.SubToken {
		t.Errorf("token rotation failed or returned old token: %+v", rotResp)
	}

	// 6. Sing-box subscription with invalid token
	reqBadSub := httptest.NewRequest("GET", "/sub/singbox?token=invalidToken", nil)
	wBadSub := httptest.NewRecorder()
	handler.ServeHTTP(wBadSub, reqBadSub)
	if wBadSub.Code != http.StatusForbidden {
		t.Errorf("expected 403 for invalid sub token, got %d", wBadSub.Code)
	}

	// 7. Logout
	reqLogout := httptest.NewRequest("POST", "/api/logout", nil)
	reqLogout.AddCookie(sessionCookie)
	wLogout := httptest.NewRecorder()
	handler.ServeHTTP(wLogout, reqLogout)
	if wLogout.Code != http.StatusOK {
		t.Errorf("expected 200 for logout, got %d", wLogout.Code)
	}

	// After logout, session should be invalid
	reqMeAfter := httptest.NewRequest("GET", "/api/me", nil)
	reqMeAfter.AddCookie(sessionCookie)
	wMeAfter := httptest.NewRecorder()
	handler.ServeHTTP(wMeAfter, reqMeAfter)
	if wMeAfter.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after logout, got %d", wMeAfter.Code)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

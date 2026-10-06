package db_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/itsnotkubrick/3X-UI_KIT/internal/auth"
	"github.com/itsnotkubrick/3X-UI_KIT/internal/db"
)

func TestDatabaseCRUD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kit-portal-db-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "portal.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// 1. Add user
	res, err := database.AddUser("alex", "sub123", "secret123")
	if err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}
	if res.Username != "alex" || res.SubID != "sub123" {
		t.Fatalf("unexpected AddUser result: %+v", res)
	}

	// 2. Add duplicate user
	_, err = database.AddUser("Alex", "sub456", "secret456")
	if err != db.ErrUserAlreadyExists {
		t.Fatalf("expected ErrUserAlreadyExists, got %v", err)
	}

	// 3. Get user
	user, err := database.GetUser("ALEX")
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if !auth.VerifyPassword("secret123", user.Salt, user.PasswordHash) {
		t.Errorf("password verification failed")
	}

	// 4. Get by token
	userByToken, err := database.GetUserByToken(res.SubToken)
	if err != nil {
		t.Fatalf("GetUserByToken failed: %v", err)
	}
	if userByToken.Username != "alex" {
		t.Errorf("expected username alex, got %s", userByToken.Username)
	}

	// 5. Sessions
	sessionID, err := database.CreateSession("alex")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessUser, err := database.GetUserFromSession(sessionID)
	if err != nil {
		t.Fatalf("GetUserFromSession failed: %v", err)
	}
	if sessUser.Username != "alex" {
		t.Errorf("expected alex from session, got %s", sessUser.Username)
	}

	// 6. Rotate token
	newToken, err := database.RotateToken("alex")
	if err != nil {
		t.Fatalf("RotateToken failed: %v", err)
	}
	if newToken == res.SubToken {
		t.Errorf("expected new token to differ from old token")
	}

	// 7. Toggle
	if err := database.ToggleUser("alex", false); err != nil {
		t.Fatalf("ToggleUser failed: %v", err)
	}
	disabledUser, err := database.GetUser("alex")
	if err != nil || disabledUser.IsActive != 0 {
		t.Errorf("expected user to be inactive, got active=%d", disabledUser.IsActive)
	}

	// Inactive user cannot be retrieved by token
	_, err = database.GetUserByToken(newToken)
	if err != db.ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound for inactive user token, got %v", err)
	}

	// 8. Delete user
	if err := database.DeleteUser("alex"); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}
	_, err = database.GetUser("alex")
	if err != db.ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound after deletion, got %v", err)
	}
}

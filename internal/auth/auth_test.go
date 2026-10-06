package auth_test

import (
	"encoding/hex"
	"testing"

	"github.com/itsnotkubrick/3X-UI_KIT/internal/auth"
)

func TestHashPasswordMatchesPython(t *testing.T) {
	password := "password123"
	saltHex := "0102030405060708090a0b0c0d0e0f10"
	expectedHash := "fe97eac321b225c50d4a7b19c7bcd33722262d819a80a2145d73ad513c7704f1"

	saltBytes, err := hex.DecodeString(saltHex)
	if err != nil {
		t.Fatalf("failed to decode salt: %v", err)
	}

	computed := auth.HashPassword(password, saltBytes)
	if computed != expectedHash {
		t.Errorf("expected hash %s, got %s", expectedHash, computed)
	}

	if !auth.VerifyPassword(password, saltHex, expectedHash) {
		t.Errorf("VerifyPassword returned false for valid password")
	}

	if auth.VerifyPassword("wrongPassword", saltHex, expectedHash) {
		t.Errorf("VerifyPassword returned true for wrong password")
	}
}

func TestGenerateSecurePassword(t *testing.T) {
	pwd, err := auth.GenerateSecurePassword(16)
	if err != nil {
		t.Fatalf("error generating password: %v", err)
	}
	if len(pwd) != 16 {
		t.Errorf("expected length 16, got %d", len(pwd))
	}
}

func TestGenerateToken(t *testing.T) {
	token, err := auth.GenerateToken(32)
	if err != nil {
		t.Fatalf("error generating token: %v", err)
	}
	if len(token) != 32 {
		t.Errorf("expected length 32, got %d", len(token))
	}
}

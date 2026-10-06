package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

const (
	PBKDF2Iterations = 600000
	KeyLength        = 32
	DefaultSaltBytes = 16
)

// HashPassword hashes password using PBKDF2-HMAC-SHA256 with 600,000 iterations.
func HashPassword(password string, salt []byte) string {
	dk := pbkdf2.Key([]byte(password), salt, PBKDF2Iterations, KeyLength, sha256.New)
	return hex.EncodeToString(dk)
}

// VerifyPassword checks if plain password matches stored hash with given hex-encoded salt.
func VerifyPassword(password, saltHex, storedHash string) bool {
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	computed := HashPassword(password, salt)
	if len(computed) != len(storedHash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}

// GenerateSalt creates random cryptographically secure salt.
func GenerateSalt(length int) (string, []byte, error) {
	if length <= 0 {
		length = DefaultSaltBytes
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(buf), buf, nil
}

// GenerateSecurePassword generates a strong random password with letters, digits, and symbols.
func GenerateSecurePassword(length int) (string, error) {
	if length < 8 {
		length = 16
	}
	const (
		lowerLetters = "abcdefghijklmnopqrstuvwxyz"
		upperLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		digits       = "0123456789"
		specials     = "!@#$%^&*-_=+"
	)
	allChars := lowerLetters + upperLetters + digits + specials

	for {
		var sb strings.Builder
		var hasLower, hasUpper, hasDigit, hasSpecial bool

		for i := 0; i < length; i++ {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(allChars))))
			if err != nil {
				return "", err
			}
			ch := allChars[n.Int64()]
			sb.WriteByte(ch)

			if strings.ContainsRune(lowerLetters, rune(ch)) {
				hasLower = true
			} else if strings.ContainsRune(upperLetters, rune(ch)) {
				hasUpper = true
			} else if strings.ContainsRune(digits, rune(ch)) {
				hasDigit = true
			} else if strings.ContainsRune(specials, rune(ch)) {
				hasSpecial = true
			}
		}

		if hasLower && hasUpper && hasDigit && hasSpecial {
			return sb.String(), nil
		}
	}
}

// GenerateToken generates an alphanumeric random token of given length.
func GenerateToken(length int) (string, error) {
	if length <= 0 {
		length = 32
	}
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var sb strings.Builder
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", err
		}
		sb.WriteByte(chars[n.Int64()])
	}
	return sb.String(), nil
}

// GenerateSessionID generates a 32-byte hex-encoded session token (64 chars).
func GenerateSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", errors.New("failed to generate random bytes for session")
	}
	return hex.EncodeToString(buf), nil
}

package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/itsnotkubrick/3X-UI_KIT/internal/auth"
	_ "modernc.org/sqlite"
)

var (
	ErrUserNotFound      = errors.New("пользователь не найден")
	ErrUserAlreadyExists = errors.New("пользователь с таким именем уже существует")
	ErrInvalidPassword   = errors.New("пароль не может быть пустым")
)

// User represents a portal user account.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Salt         string `json:"-"`
	SubID        string `json:"sub_id"`
	SubToken     string `json:"sub_token"`
	IsActive     int    `json:"is_active"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

// UserCreateResult contains details returned upon user creation.
type UserCreateResult struct {
	Success        bool   `json:"success"`
	Username       string `json:"username"`
	Password       string `json:"password,omitempty"`
	SubID          string `json:"sub_id"`
	SubToken       string `json:"sub_token"`
	IsActive       int    `json:"is_active"`
	PortalURL      string `json:"portal_url,omitempty"`
	SingboxSubURL  string `json:"singbox_sub_url,omitempty"`
}

// DB wraps standard database/sql DB with mutex for thread-safe SQLite operations.
type DB struct {
	conn *sql.DB
	mu   sync.RWMutex
}

// Open initializes the SQLite database with WAL mode and schema definitions.
func Open(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(time.Hour)

	d := &DB{conn: conn}
	if err := d.initSchema(dbPath); err != nil {
		conn.Close()
		return nil, err
	}

	return d, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) initSchema(dbPath string) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL COLLATE NOCASE,
		password_hash TEXT NOT NULL,
		salt TEXT NOT NULL,
		sub_id TEXT NOT NULL,
		sub_token TEXT UNIQUE NOT NULL,
		is_active INTEGER DEFAULT 1,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS sessions (
		session_id TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS login_attempts (
		ip TEXT NOT NULL,
		attempt_time INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
	CREATE INDEX IF NOT EXISTS idx_users_sub_token ON users(sub_token);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
	CREATE INDEX IF NOT EXISTS idx_attempts_ip_time ON login_attempts(ip, attempt_time);
	`
	if _, err := d.conn.Exec(schema); err != nil {
		return fmt.Errorf("failed to initialize db schema: %w", err)
	}

	_ = os.Chmod(dbPath, 0600)
	return nil
}

// AddUser creates a new user with generated or provided credentials.
func (d *DB) AddUser(username, subID, password string) (*UserCreateResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	username = strings.TrimSpace(username)
	if username == "" {
		return nil, errors.New("имя пользователя не может быть пустым")
	}

	var exists int
	err := d.conn.QueryRow("SELECT COUNT(*) FROM users WHERE username = ? COLLATE NOCASE", username).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if exists > 0 {
		return nil, ErrUserAlreadyExists
	}

	plainPassword := password
	if plainPassword == "" {
		generated, err := auth.GenerateSecurePassword(16)
		if err != nil {
			return nil, err
		}
		plainPassword = generated
	}

	saltHex, saltBytes, err := auth.GenerateSalt(16)
	if err != nil {
		return nil, err
	}
	pwdHash := auth.HashPassword(plainPassword, saltBytes)

	if subID == "" {
		sid, err := auth.GenerateToken(16)
		if err != nil {
			return nil, err
		}
		subID = sid
	}

	subToken, err := auth.GenerateToken(32)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	_, err = d.conn.Exec(`
		INSERT INTO users (username, password_hash, salt, sub_id, sub_token, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)
	`, username, pwdHash, saltHex, subID, subToken, now, now)
	if err != nil {
		return nil, fmt.Errorf("ошибка вставки пользователя: %w", err)
	}

	return &UserCreateResult{
		Success:  true,
		Username: username,
		Password: plainPassword,
		SubID:    subID,
		SubToken: subToken,
		IsActive: 1,
	}, nil
}

// DeleteUser removes a user and their sessions.
func (d *DB) DeleteUser(username string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM sessions WHERE username = ? COLLATE NOCASE", username); err != nil {
		return err
	}
	res, err := tx.Exec("DELETE FROM users WHERE username = ? COLLATE NOCASE", username)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrUserNotFound
	}
	return tx.Commit()
}

// ToggleUser enables or disables a user account.
func (d *DB) ToggleUser(username string, active bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().Unix()
	val := 0
	if active {
		val = 1
	}

	res, err := d.conn.Exec("UPDATE users SET is_active = ?, updated_at = ? WHERE username = ? COLLATE NOCASE", val, now, username)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrUserNotFound
	}

	if !active {
		_, _ = d.conn.Exec("DELETE FROM sessions WHERE username = ? COLLATE NOCASE", username)
	}
	return nil
}

// SetPassword updates user password and clears active sessions.
func (d *DB) SetPassword(username, newPassword string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if strings.TrimSpace(newPassword) == "" {
		return ErrInvalidPassword
	}

	saltHex, saltBytes, err := auth.GenerateSalt(16)
	if err != nil {
		return err
	}
	pwdHash := auth.HashPassword(newPassword, saltBytes)
	now := time.Now().Unix()

	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec("UPDATE users SET password_hash = ?, salt = ?, updated_at = ? WHERE username = ? COLLATE NOCASE", pwdHash, saltHex, now, username)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrUserNotFound
	}

	if _, err := tx.Exec("DELETE FROM sessions WHERE username = ? COLLATE NOCASE", username); err != nil {
		return err
	}
	return tx.Commit()
}

// RotateToken regenerates sub_token for a user.
func (d *DB) RotateToken(username string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	newToken, err := auth.GenerateToken(32)
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()

	res, err := d.conn.Exec("UPDATE users SET sub_token = ?, updated_at = ? WHERE username = ? COLLATE NOCASE", newToken, now, username)
	if err != nil {
		return "", err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return "", ErrUserNotFound
	}
	return newToken, nil
}

// GetUser fetches user by username (case-insensitive).
func (d *DB) GetUser(username string) (*User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.conn.QueryRow(`
		SELECT id, username, password_hash, salt, sub_id, sub_token, is_active, created_at, updated_at
		FROM users WHERE username = ? COLLATE NOCASE
	`, username)

	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Salt, &u.SubID, &u.SubToken, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByToken finds an active user by their sub_token.
func (d *DB) GetUserByToken(token string) (*User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.conn.QueryRow(`
		SELECT id, username, password_hash, salt, sub_id, sub_token, is_active, created_at, updated_at
		FROM users WHERE sub_token = ? AND is_active = 1
	`, token)

	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Salt, &u.SubID, &u.SubToken, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ListUsers returns all users sorted by username.
func (d *DB) ListUsers() ([]User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.conn.Query(`
		SELECT id, username, sub_id, sub_token, is_active, created_at, updated_at
		FROM users ORDER BY username COLLATE NOCASE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.SubID, &u.SubToken, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, nil
}

// CreateSession generates a new 30-day session and removes expired sessions.
func (d *DB) CreateSession(username string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().Unix()
	_, _ = d.conn.Exec("DELETE FROM sessions WHERE expires_at < ?", now)

	sessionID, err := auth.GenerateSessionID()
	if err != nil {
		return "", err
	}

	expiresAt := now + 86400*30 // 30 days
	_, err = d.conn.Exec(`
		INSERT INTO sessions (session_id, username, created_at, expires_at)
		VALUES (?, ?, ?, ?)
	`, sessionID, username, now, expiresAt)
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

// GetUserFromSession returns user if session is valid and not expired.
func (d *DB) GetUserFromSession(sessionID string) (*User, error) {
	if sessionID == "" {
		return nil, ErrUserNotFound
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	now := time.Now().Unix()
	row := d.conn.QueryRow(`
		SELECT u.id, u.username, u.password_hash, u.salt, u.sub_id, u.sub_token, u.is_active, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON s.username = u.username COLLATE NOCASE
		WHERE s.session_id = ? AND s.expires_at > ? AND u.is_active = 1
	`, sessionID, now)

	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Salt, &u.SubID, &u.SubToken, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// DeleteSession invalidates a session by session ID.
func (d *DB) DeleteSession(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.conn.Exec("DELETE FROM sessions WHERE session_id = ?", sessionID)
	return err
}

// IsRateLimited checks if IP/user exceeded 5 login attempts in the last 5 minutes.
func (d *DB) IsRateLimited(ip, username string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	now := time.Now().Unix()
	cutoff := now - 300 // 5 minutes

	key := ip
	if (ip == "127.0.0.1" || ip == "::1") && username != "" {
		key = fmt.Sprintf("%s:%s", ip, strings.ToLower(username))
	}

	var count int
	err := d.conn.QueryRow("SELECT COUNT(*) FROM login_attempts WHERE ip = ? AND attempt_time > ?", key, cutoff).Scan(&count)
	if err != nil {
		return false
	}
	return count >= 5
}

// RecordLoginAttempt records a failed login attempt.
func (d *DB) RecordLoginAttempt(ip, username string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now().Unix()
	_, _ = d.conn.Exec("DELETE FROM login_attempts WHERE attempt_time < ?", now-600)

	key := ip
	if (ip == "127.0.0.1" || ip == "::1") && username != "" {
		key = fmt.Sprintf("%s:%s", ip, strings.ToLower(username))
	}

	_, _ = d.conn.Exec("INSERT INTO login_attempts (ip, attempt_time) VALUES (?, ?)", key, now)
}

// ClearLoginAttempts cleans attempts after successful login.
func (d *DB) ClearLoginAttempts(ip, username string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := ip
	if (ip == "127.0.0.1" || ip == "::1") && username != "" {
		key = fmt.Sprintf("%s:%s", ip, strings.ToLower(username))
	}
	_, _ = d.conn.Exec("DELETE FROM login_attempts WHERE ip = ?", key)
}

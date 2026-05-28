package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	cookieName    = "pa_session"
	sessionTTL    = 30 * 24 * time.Hour
	bcryptCost    = 12
)

type credentials struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
}

type session struct {
	Username  string
	ExpiresAt time.Time
}

// Manager handles setup, login, sessions, and auth middleware.
type Manager struct {
	credFile string
	creds    *credentials
	sessions map[string]*session
	mu       sync.RWMutex
}

func New(credFile string) (*Manager, error) {
	m := &Manager{
		credFile: credFile,
		sessions: make(map[string]*session),
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

// IsSetup returns true if credentials have been configured.
func (m *Manager) IsSetup() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.creds != nil
}

// Setup stores the initial username and hashed password.
func (m *Manager) Setup(username, password string) error {
	if username == "" || len(password) < 8 {
		return errors.New("username required and password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return err
	}
	creds := &credentials{Username: username, PasswordHash: string(hash)}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.credFile, data, 0600); err != nil {
		return err
	}
	m.mu.Lock()
	m.creds = creds
	m.mu.Unlock()
	return nil
}

// Login validates credentials and returns a session token on success.
func (m *Manager) Login(username, password string) (string, error) {
	m.mu.RLock()
	creds := m.creds
	m.mu.RUnlock()

	if creds == nil {
		return "", errors.New("not set up")
	}
	if creds.Username != username {
		return "", errors.New("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(creds.PasswordHash), []byte(password)); err != nil {
		return "", errors.New("invalid credentials")
	}

	token, err := randomToken()
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	m.sessions[token] = &session{Username: username, ExpiresAt: time.Now().Add(sessionTTL)}
	m.mu.Unlock()

	return token, nil
}

// Logout removes the session token.
func (m *Manager) Logout(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

// Middleware protects all routes. Redirects to /setup or /login as needed.
// Routes starting with /mcp are exempt (they use TokenMiddleware instead).
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Auth pages, MCP, webhook, and signed file endpoints are always exempt
		if path == "/setup" || path == "/login" || path == "/logout" ||
			path == "/mcp" || len(path) > 4 && path[:5] == "/mcp/" ||
			path == "/api/webhook" ||
			len(path) > 6 && path[:7] == "/files/" {
			next.ServeHTTP(w, r)
			return
		}

		if !m.IsSetup() {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}

		cookie, err := r.Cookie(cookieName)
		if err != nil || !m.validateToken(cookie.Value) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// TokenMiddleware protects a route with a static Bearer token.
func TokenMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			expected := "Bearer " + token
			if auth != expected {
				w.Header().Set("WWW-Authenticate", `Bearer realm="paperless-aissist"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SetCookie writes a session cookie to the response.
func (m *Manager) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearCookie removes the session cookie.
func (m *Manager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// TokenFromRequest extracts the session token from the request cookie.
func (m *Manager) TokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func (m *Manager) validateToken(token string) bool {
	m.mu.RLock()
	s, ok := m.sessions[token]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(s.ExpiresAt) {
		m.mu.Lock()
		delete(m.sessions, token)
		m.mu.Unlock()
		return false
	}
	return true
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.credFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil // not set up yet
	}
	if err != nil {
		return err
	}
	var creds credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return err
	}
	m.creds = &creds
	return nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

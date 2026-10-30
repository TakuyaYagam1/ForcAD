package usecase

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	cachekit "github.com/wahrwelt-kit/go-cachekit"
)

const SessionTTL = 8 * time.Hour

var ErrUnauthorized = errors.New("invalid credentials or session")
var ErrInvalidCSRF = errors.New("invalid CSRF token")

type SessionStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

type Auth struct {
	store              SessionStore
	username, password string
}
type sessionData struct {
	Username        string `json:"username"`
	PasswordVersion string `json:"password_version"`
	CSRFToken       string `json:"csrf_token"`
}

func NewAuth(store SessionStore, username, password string) *Auth {
	return &Auth{store: store, username: username, password: password}
}

func equal(a, b string) bool {
	aa, bb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aa[:], bb[:]) == 1
}

// The marker binds a session to its username and configured password.
// Rotating the password invalidates previously issued sessions.
func marker(password, session, username string) string {
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode([]string{session, username})
	mac := hmac.New(sha256.New, []byte(password))
	_, _ = mac.Write(bytes.TrimSuffix(data.Bytes(), []byte("\n")))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *Auth) Login(ctx context.Context, username, password string) (string, error) {
	const location = "usecase.Auth.Login"
	userOK, passwordOK := equal(username, a.username), equal(password, a.password)
	if !userOK || !passwordOK || a.username == "" || a.password == "" {
		return "", fmt.Errorf("%s - authenticate administrator: %w", location, ErrUnauthorized)
	}
	var random [64]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("%s - generate session token: %w", location, err)
	}
	token := hex.EncodeToString(random[:32])
	data, err := json.Marshal(sessionData{Username: a.username, PasswordVersion: marker(a.password, token, a.username), CSRFToken: hex.EncodeToString(random[32:])})
	if err != nil {
		return "", fmt.Errorf("%s - encode session: %w", location, err)
	}
	if err = a.store.Set(ctx, "session:"+token, data, SessionTTL); err != nil {
		return "", fmt.Errorf("%s - store session: %w", location, err)
	}
	return token, nil
}

func (a *Auth) Status(ctx context.Context, token string) (string, error) {
	username, _, err := a.StatusDetails(ctx, token)
	return username, err
}

// StatusDetails reads one authenticated session, including its independent
// random synchronizer token. Old sessions without that token require re-login.
func (a *Auth) StatusDetails(ctx context.Context, token string) (string, string, error) {
	const location = "usecase.Auth.Status"
	if !validToken(token) {
		return "", "", fmt.Errorf("%s - validate session token: %w", location, ErrUnauthorized)
	}
	data, err := a.store.Get(ctx, "session:"+token)
	if err != nil {
		if errors.Is(err, cachekit.ErrNotFound) {
			return "", "", fmt.Errorf("%s - session is unavailable: %w", location, ErrUnauthorized)
		}
		return "", "", fmt.Errorf("%s - load session: %w", location, err)
	}
	var session sessionData
	if json.Unmarshal(data, &session) != nil || !validToken(session.CSRFToken) || !equal(session.Username, a.username) || !equal(session.PasswordVersion, marker(a.password, token, a.username)) {
		_ = a.store.Del(ctx, "session:"+token)
		return "", "", fmt.Errorf("%s - validate stored session: %w", location, ErrUnauthorized)
	}
	return a.username, session.CSRFToken, nil
}

func MatchCSRFToken(provided, expected string) bool {
	return validToken(provided) && validToken(expected) && equal(provided, expected)
}

func (a *Auth) Logout(ctx context.Context, token string) error {
	const location = "usecase.Auth.Logout"
	if !validToken(token) {
		return nil
	}
	if err := a.store.Del(ctx, "session:"+token); err != nil {
		return fmt.Errorf("%s - delete session: %w", location, err)
	}
	return nil
}

func validToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	for _, c := range token {
		if c >= '0' && c <= '9' {
			continue
		}
		if c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}

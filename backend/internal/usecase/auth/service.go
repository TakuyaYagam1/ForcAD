package auth

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

type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

type Service struct {
	store              Store
	username, password string
}
type sessionData struct {
	Username        string `json:"username"`
	PasswordVersion string `json:"password_version"`
}

func New(store Store, username, password string) *Service {
	return &Service{store: store, username: username, password: password}
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

func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	const location = "auth.Service.Login"
	userOK, passwordOK := equal(username, s.username), equal(password, s.password)
	if !userOK || !passwordOK || s.username == "" || s.password == "" {
		return "", fmt.Errorf("%s - authenticate administrator: %w", location, ErrUnauthorized)
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("%s - generate session token: %w", location, err)
	}
	token := hex.EncodeToString(random[:])
	data, err := json.Marshal(sessionData{Username: s.username, PasswordVersion: marker(s.password, token, s.username)})
	if err != nil {
		return "", fmt.Errorf("%s - encode session: %w", location, err)
	}
	if err = s.store.Set(ctx, "session:"+token, data, SessionTTL); err != nil {
		return "", fmt.Errorf("%s - store session: %w", location, err)
	}
	return token, nil
}

func (s *Service) Status(ctx context.Context, token string) (string, error) {
	const location = "auth.Service.Status"
	if !validToken(token) {
		return "", fmt.Errorf("%s - validate session token: %w", location, ErrUnauthorized)
	}
	data, err := s.store.Get(ctx, "session:"+token)
	if err != nil {
		if errors.Is(err, cachekit.ErrNotFound) {
			return "", fmt.Errorf("%s - session is unavailable: %w", location, ErrUnauthorized)
		}
		return "", fmt.Errorf("%s - load session: %w", location, err)
	}
	var session sessionData
	if json.Unmarshal(data, &session) != nil || !equal(session.Username, s.username) || !equal(session.PasswordVersion, marker(s.password, token, s.username)) {
		_ = s.store.Del(ctx, "session:"+token)
		return "", fmt.Errorf("%s - validate stored session: %w", location, ErrUnauthorized)
	}
	return s.username, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	const location = "auth.Service.Logout"
	if !validToken(token) {
		return nil
	}
	if err := s.store.Del(ctx, "session:"+token); err != nil {
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

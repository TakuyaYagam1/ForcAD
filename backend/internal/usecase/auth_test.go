package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cachekit "github.com/wahrwelt-kit/go-cachekit"
)

type memoryStore struct {
	values map[string][]byte
	ttl    time.Duration
}

func (m *memoryStore) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := m.values[key]
	if !ok {
		return nil, cachekit.ErrNotFound
	}
	return v, nil
}
func (m *memoryStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	m.values[key] = value
	m.ttl = ttl
	return nil
}
func (m *memoryStore) Del(_ context.Context, keys ...string) error {
	for _, key := range keys {
		delete(m.values, key)
	}
	return nil
}

func TestSessionLifecycleAndPasswordRotation(t *testing.T) {
	ctx := context.Background()
	store := &memoryStore{values: map[string][]byte{}}
	service := NewAuth(store, "admin", "original-password")
	if _, err := service.Login(ctx, "admin", "wrong"); !errors.Is(err, ErrUnauthorized) || !strings.HasPrefix(err.Error(), "usecase.Auth.Login - ") {
		t.Fatal("accepted wrong password")
	}
	token, err := service.Login(ctx, "admin", "original-password")
	if err != nil {
		t.Fatal(err)
	}
	if !validToken(token) || store.ttl != SessionTTL {
		t.Fatal("incorrect session token/ttl")
	}
	if username, err := service.Status(ctx, token); err != nil || username != "admin" {
		t.Fatalf("session rejected: %v", err)
	}
	rotated := NewAuth(store, "admin", "changed-password")
	if _, err := rotated.Status(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("password rotation didn't revoke session")
	}
	if len(store.values) != 0 {
		t.Fatal("revoked session retained")
	}
	token, err = service.Login(ctx, "admin", "original-password")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Status(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("logout didn't revoke session")
	}
}

func TestMalformedSessionsFailClosed(t *testing.T) {
	ctx := context.Background()
	store := &memoryStore{values: map[string][]byte{}}
	service := NewAuth(store, "admin", "password")
	for _, token := range []string{"", "session:*", "../secret", "ffffffff"} {
		if _, err := service.Status(ctx, token); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("malformed token accepted")
		}
	}
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, payload := range []string{"null", "[]", "{}", `{"username":"admin","password_version":"x"}`} {
		store.values["session:"+token] = []byte(payload)
		if _, err := service.Status(ctx, token); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("malformed payload accepted")
		}
	}
}

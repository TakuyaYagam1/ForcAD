package usecase

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCSRFTokenIsIndependentAndBoundToSessionLifetime(t *testing.T) {
	store := &memoryStore{values: make(map[string][]byte)}
	auth := NewAuth(store, "admin", "test-password")
	first, err := auth.Login(t.Context(), "admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := auth.Login(t.Context(), "admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	_, csrf1, err := auth.StatusDetails(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	_, csrf2, err := auth.StatusDetails(t.Context(), second)
	if err != nil {
		t.Fatal(err)
	}
	if !validToken(csrf1) || !validToken(csrf2) || csrf1 == first || csrf1 == csrf2 {
		t.Fatal("CSRF token not independent per session")
	}
	for _, provided := range []string{"", "short", strings.Repeat("z", 64), first, csrf2} {
		if MatchCSRFToken(provided, csrf1) {
			t.Fatal("accepted missing, malformed, bearer or another session's token")
		}
	}
	if !MatchCSRFToken(csrf1, csrf1) {
		t.Fatal("own synchronizer token rejected")
	}
	_, again, err := auth.StatusDetails(t.Context(), first)
	if err != nil || again != csrf1 {
		t.Fatal("reading status rotated an active session token")
	}
	if err := auth.Logout(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.StatusDetails(t.Context(), first); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("logout retained token access")
	}
}

func TestLegacySessionWithoutCSRFRequiresLogin(t *testing.T) {
	store := &memoryStore{values: make(map[string][]byte)}
	auth := NewAuth(store, "admin", "test-password")
	token, err := auth.Login(t.Context(), "admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(sessionData{Username: "admin", PasswordVersion: marker("test-password", token, "admin")})
	if err != nil {
		t.Fatal(err)
	}
	store.values["session:"+token] = legacy
	if _, _, err := auth.StatusDetails(t.Context(), token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("legacy session retained a CSRF-free path")
	}
	if _, ok := store.values["session:"+token]; ok {
		t.Fatal("invalid legacy session was not removed")
	}
}

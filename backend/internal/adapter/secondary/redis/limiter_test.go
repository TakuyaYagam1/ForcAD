package redisadapter

import (
	"context"
	"strings"
	"testing"
)

func TestAllowReportsInvalidRateLimitContext(t *testing.T) {
	store := &Store{}
	_, err := store.Allow(context.Background(), "test", -1, 1, 1)
	if err == nil || !strings.HasPrefix(err.Error(), "redisadapter.Store.Allow - ") {
		t.Fatalf("Allow() error = %v, want local context", err)
	}
}

package redisadapter

import (
	"testing"
	"time"
)

func TestConnectionSettingsPreserveBoundedSharedClient(t *testing.T) {
	settings := connectionSettings(ConnectionOptions{
		Addr:     "cache.example:6380",
		Password: "private",
		DB:       4,
	})
	if settings.Addr != "cache.example:6380" || settings.Password != "private" || settings.DB != 4 {
		t.Fatal("connection options were not passed through")
	}
	if settings.PoolSize != 16 || settings.MaxActiveConns != 32 || settings.PoolTimeout != time.Second ||
		settings.DialTimeout != 2*time.Second || settings.ReadTimeout != 2*time.Second || settings.WriteTimeout != 2*time.Second ||
		!settings.ContextTimeoutEnabled || settings.MaxRetries != 1 {
		t.Fatal("shared Redis client limits, timeouts or retry policy changed")
	}
}

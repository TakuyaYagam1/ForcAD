package bootstrap

import (
	"net/http"
	"time"
)

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:             handler,
		ReadHeaderTimeout:   5 * time.Second,
		ReadTimeout:         15 * time.Second,
		IdleTimeout:         time.Minute,
		MaxHeaderBytes:      16 << 10,
		MaxHeaderValueCount: 100,
	}
}

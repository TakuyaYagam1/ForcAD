package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
)

func AdminOrigin(publicOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/admin/") && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
				origin := r.Header.Get("Origin")
				allowed := publicOrigin
				if allowed == "" {
					scheme := "http"
					if IsSecureRequest(r.Context()) {
						scheme = "https"
					}
					allowed = scheme + "://" + r.Host
				}
				if r.Header.Get("Sec-Fetch-Site") == "cross-site" || origin != "" && !sameOrigin(origin, allowed) {
					WriteError(w, http.StatusForbidden, api.ApiError{Error: "Cross-origin administrative request rejected"})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func sameOrigin(a, b string) bool {
	x, err := url.Parse(a)
	if err != nil {
		return false
	}
	y, err := url.Parse(b)
	if err != nil {
		return false
	}
	return x.User == nil && x.RawQuery == "" && x.Fragment == "" && (x.Path == "" || x.Path == "/") && strings.EqualFold(x.Scheme, y.Scheme) && strings.EqualFold(x.Host, y.Host)
}

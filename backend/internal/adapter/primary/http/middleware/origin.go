package middleware

import (
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
)

func AdminOrigin(publicOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/admin/") && unsafeMethod(r.Method) {
				w.Header().Add("Vary", "Origin, Sec-Fetch-Site, Referer")
				if !adminRequestOrigin(r, publicOrigin) {
					WriteError(w, http.StatusForbidden, api.ApiError{Error: "Cross-origin administrative request rejected"})
					return
				}
				// Login has no authenticated session/token yet. JSON excludes
				// browser simple form submissions; Origin/Fetch Metadata remains
				// required defense when a browser supplies those headers.
				if r.URL.Path == "/api/admin/login/" {
					kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
					if err != nil || kind != "application/json" {
						WriteError(w, http.StatusUnsupportedMediaType, api.ApiError{Error: "Login requires application/json"})
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func unsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func adminRequestOrigin(r *http.Request, allowed string) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return false
	}
	if allowed == "" {
		scheme := "http"
		if IsSecureRequest(r.Context()) {
			scheme = "https"
		}
		allowed = scheme + "://" + r.Host
	}
	if origins := r.Header.Values("Origin"); len(origins) != 0 {
		return len(origins) == 1 && sameOrigin(origins[0], allowed)
	}
	if refs := r.Header.Values("Referer"); len(refs) != 0 {
		if len(refs) != 1 {
			return false
		}
		ref, err := url.Parse(refs[0])
		if err != nil || ref.User != nil || ref.Opaque != "" {
			return false
		}
		return sameOrigin(ref.Scheme+"://"+ref.Host, allowed)
	}
	// Non-browser clients may omit Origin/Fetch Metadata. Authenticated
	// mutations still require their session-bound token in a custom header.
	return true
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
	return x.User == nil && x.Opaque == "" && x.Host != "" && !x.ForceQuery && x.RawQuery == "" && x.Fragment == "" && (x.Path == "" || x.Path == "/") && strings.EqualFold(x.Scheme, y.Scheme) && strings.EqualFold(x.Host, y.Host)
}

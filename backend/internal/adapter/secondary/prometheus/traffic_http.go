package prometheus

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/wahrwelt-kit/go-httpkit/httputil"
)

type trafficKey struct{}
type requestTraffic struct {
	team     atomic.Value
	upgraded atomic.Bool
}

// Middleware receives a bounded route classifier, never a raw client path.
func (t *TeamTraffic) Middleware(route func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope := &requestTraffic{}
			scope.team.Store(t.source(httputil.GetClientIPWithNets(r, t.trusted)))
			r = r.WithContext(context.WithValue(r.Context(), trafficKey{}, scope))
			start := time.Now()
			response := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				status := response.Status()
				if scope.upgraded.Load() || status == http.StatusSwitchingProtocols {
					return
				}
				if status == 0 {
					status = http.StatusOK
				}
				id, path := scope.team.Load().(string), route(r)
				t.requests.WithLabelValues(id, path, strconv.Itoa(status)).Inc()
				t.duration.WithLabelValues(id, path).Observe(time.Since(start).Seconds())
			}()
			next.ServeHTTP(response, r)
		})
	}
}

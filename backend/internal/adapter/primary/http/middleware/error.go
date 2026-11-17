package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
)

func WriteError(w http.ResponseWriter, status int, body api.ApiError) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

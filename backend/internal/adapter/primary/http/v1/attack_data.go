package v1

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const attackDataCacheControl = "public, no-cache"

func writeAttackData(
	ctx context.Context,
	game GameService,
	service *string,
	ifNoneMatch string,
	w http.ResponseWriter,
) error {
	const location = "httpv1.gameHandler.GetClientAttackData"
	data, err := game.AttackData(ctx, service)
	if err != nil {
		return fmt.Errorf("%s - load public attack data: %w", location, err)
	}
	if data == nil {
		data = map[string]map[string][]string{}
	}
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("%s - encode public attack data: %w", location, err)
	}
	checksum := sha256.Sum256(body)
	etag := fmt.Sprintf("\"%x\"", checksum)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", attackDataCacheControl)
	if matchesIfNoneMatch(ifNoneMatch, etag) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err = w.Write(body); err != nil {
		return fmt.Errorf("%s - write public attack data: %w", location, err)
	}
	return nil
}

func matchesIfNoneMatch(header, etag string) bool {
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}

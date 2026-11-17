package response

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/go-faster/jx"
)

// AttackData encodes the public representation with stable keys and HTML escaping.
// The encoder owns the resulting bytes until it is returned to its pool.
func AttackData(e *jx.Encoder, data map[string]map[string][]string) error {
	const location = "httpv1.response.AttackData"
	w := jx.Writer{
		Buf: e.Bytes()[:0],
	}
	if appendAttackData(&w, data) {
		e.SetBytes(w.Buf)
		return nil
	}
	// JX and encoding/json use different byte representations for some control
	// characters and invalid UTF-8. Preserve the existing bytes and ETags there.
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("%s - encode public attack data: %w", location, err)
	}
	e.SetBytes(append(w.Buf[:0], body...))
	return nil
}

func appendAttackData(w *jx.Writer, data map[string]map[string][]string) bool {
	w.ObjStart()
	for i, service := range sortedAttackDataKeys(data) {
		if i > 0 {
			w.RawStr(",")
		}
		if !appendAttackDataString(w, service) {
			return false
		}
		w.RawStr(":")
		if data[service] == nil {
			w.Null()
			continue
		}
		if !appendAttackDataTeams(w, data[service]) {
			return false
		}
	}
	w.ObjEnd()
	return true
}

func appendAttackDataTeams(w *jx.Writer, teams map[string][]string) bool {
	w.ObjStart()
	for i, team := range sortedAttackDataKeys(teams) {
		if i > 0 {
			w.RawStr(",")
		}
		if !appendAttackDataString(w, team) {
			return false
		}
		w.RawStr(":")
		if teams[team] == nil {
			w.Null()
			continue
		}
		w.ArrStart()
		for j, hint := range teams[team] {
			if j > 0 {
				w.RawStr(",")
			}
			if !appendAttackDataString(w, hint) {
				return false
			}
		}
		w.ArrEnd()
	}
	w.ObjEnd()
	return true
}

func appendAttackDataString(w *jx.Writer, value string) bool {
	if strings.ContainsAny(value, "\b\f") || !utf8.ValidString(value) {
		return false
	}
	w.StrEscape(value)
	return true
}

func sortedAttackDataKeys[V any](data map[string]V) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

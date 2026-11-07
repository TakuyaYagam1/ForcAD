package nftables

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
)

func TestJSONAndCounterHelpersPreserveErrorTypesAndLocations(t *testing.T) {
	err := consumeJSONValue(json.NewDecoder(strings.NewReader("")))
	if !errors.Is(err, io.EOF) || !strings.HasPrefix(err.Error(), "nftables.consumeJSONValue - ") {
		t.Fatalf("JSON helper lost read error: %v", err)
	}
	_, err = parseCounterNumber(json.Number("-1"))
	if _, ok := errors.AsType[*strconv.NumError](err); !ok || !strings.HasPrefix(err.Error(), "nftables.parseCounterNumber - ") {
		t.Fatalf("counter parser lost typed error: %v", err)
	}
}

func TestDuplicateJSONKeyErrorDoesNotExposeKey(t *testing.T) {
	err := rejectDuplicateJSONKeys([]byte(`{"private-key":1,"private-key":2}`))
	if err == nil || !strings.Contains(err.Error(), "nftables.rejectDuplicateJSONKeys - ") || !strings.Contains(err.Error(), "nftables.consumeJSONObject - ") {
		t.Fatalf("duplicate key lost nested attribution: %v", err)
	}
	if strings.Contains(err.Error(), "private-key") {
		t.Fatalf("duplicate key error exposed its input: %v", err)
	}
}

package checker

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProcessDiagnosticMarshalErrorKeepsJSONErrorType(t *testing.T) {
	diagnostic := ProcessDiagnostic{
		RequestedAt: time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	_, err := diagnostic.MarshalJSON()
	if _, ok := errors.AsType[*jsonv2.SemanticError](err); !ok || !strings.HasPrefix(err.Error(), "checker.ProcessDiagnostic.MarshalJSON - ") {
		t.Fatalf("diagnostic encoding lost attribution or JSON error type: %v", err)
	}
}

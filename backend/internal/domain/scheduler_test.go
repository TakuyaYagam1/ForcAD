package domain

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestSnapshotJSONPreservesEncodingError(t *testing.T) {
	run := DispatchRun{Snapshot: &RoundSnapshot{
		TeamTasks: []PublicCell{{Score: math.NaN()}},
	}}
	data, err := run.SnapshotJSON()
	if _, ok := errors.AsType[*json.UnsupportedValueError](err); !ok {
		t.Fatalf("SnapshotJSON() error = %v, want an encoding error", err)
	}
	if data != nil || !strings.HasPrefix(err.Error(), "domain.DispatchRun.SnapshotJSON - ") {
		t.Fatalf("SnapshotJSON() = %q, %v, want nil data and source context", data, err)
	}
}

func TestSnapshotJSONWithoutSnapshot(t *testing.T) {
	data, err := (DispatchRun{}).SnapshotJSON()
	if data != nil || err != nil {
		t.Fatalf("SnapshotJSON() = %q, %v, want nil, nil", data, err)
	}
}

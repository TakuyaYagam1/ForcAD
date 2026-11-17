package load_test

import (
	"encoding/json"
	"testing"
)

func TestReportCheckerResultScopePreservesZeroAndUnknownCounts(t *testing.T) {
	var report suiteReport
	assertScope := func(wantFields bool) {
		t.Helper()
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"checker_results_before", "checker_results_total"} {
			value, present := fields[name]
			if present != wantFields || (present && string(value) != "0") {
				t.Fatalf("%s: present=%t value=%s; want present=%t and zero", name, present, value, wantFields)
			}
		}
	}
	assertScope(false)
	before, total := int64(0), int64(0)
	report.CheckerResultsBefore = &before
	report.CheckerResultsTotal = &total
	assertScope(true)
}

package load_test

import (
	"testing"
	"time"
)

func healthyCheckerReport() *checkerReport {
	cells, puts := teamCount*3, teamCount*35
	return &checkerReport{RoundSeconds: 60, ExpectedCells: cells, ExpectedPuts: puts,
		Rounds: []checkerRoundReport{{Round: 1, Jobs: cells, Cells: cells,
			FinishedJobs: cells, UpJobs: cells, PutActions: puts, StoredFlags: puts,
			Complete: true, CompletionSeconds: 59.9}}}
}

func TestCheckerAuditRejectsIncompleteAndLateWork(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*checkerReport)
	}{
		{"missing cell", func(r *checkerReport) { r.Rounds[0].Cells-- }},
		{"duplicate job", func(r *checkerReport) { r.Rounds[0].Jobs++ }},
		{"missing round", func(r *checkerReport) { r.Rounds[0].Round = 2 }},
		{"missing result", func(r *checkerReport) { r.Rounds[0].FinishedJobs-- }},
		{"bad result", func(r *checkerReport) { r.Rounds[0].UpJobs-- }},
		{"missing PUT", func(r *checkerReport) { r.Rounds[0].PutActions-- }},
		{"missing flag", func(r *checkerReport) { r.Rounds[0].StoredFlags-- }},
		{"wrong distribution with correct totals", func(r *checkerReport) { r.Rounds[0].MismatchedCells = 2 }},
		{"round exceeds schedule but within grace", func(r *checkerReport) { r.Rounds[0].CompletionSeconds = 60.001 }},
		{"never completed old round", func(r *checkerReport) { r.Rounds[0].Complete = false; r.PastRoundOpen = 1 }},
		{"expired job", func(r *checkerReport) { r.ExpiredJobs = 1 }},
		{"overdue job", func(r *checkerReport) { r.OverdueJobs = 1 }},
		{"historical failed job", func(r *checkerReport) { r.BadJobs = 1 }},
		{"failed action", func(r *checkerReport) { r.BadActions = 1 }},
		{"action timeout", func(r *checkerReport) { r.ActionTimeouts = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := healthyCheckerReport()
			if err := report.validate(); err != nil {
				t.Fatal(err)
			}
			test.change(report)
			if err := report.validate(); err == nil {
				t.Fatal("checker correctness failure accepted")
			}
		})
	}
}

func TestCheckerAuditAllowsOnlyCurrentInflightWork(t *testing.T) {
	report := healthyCheckerReport()
	report.Rounds = append(report.Rounds, checkerRoundReport{
		Round: 2, Jobs: report.ExpectedCells, Cells: report.ExpectedCells,
		MismatchedCells: report.ExpectedCells,
	})
	if err := report.validate(); err != nil || report.completedRounds() != 1 {
		t.Fatalf("current partial round rejected or counted as complete: %v", err)
	}
	for _, test := range []struct {
		duration           time.Duration
		roundSeconds, want int
	}{
		{30 * time.Second, 60, 0}, {time.Minute, 60, 0},
		{2 * time.Minute, 60, 1}, {5 * time.Minute, 60, 4},
		{20 * time.Second, 10, 1},
	} {
		if got := minimumCompletedRounds(test.duration, test.roundSeconds); got != test.want {
			t.Fatalf("minimum completed rounds for %s/%ds = %d, want %d", test.duration, test.roundSeconds, got, test.want)
		}
	}
}

func TestCheckerProfileUsesRequestedRESTThroughout(t *testing.T) {
	t.Setenv("LOAD_PROFILE", "checker")
	t.Setenv("LOAD_REST_RPS", "50")
	t.Setenv("LOAD_DURATION", "10m")
	t.Setenv("LOAD_CHECKPOINT_INTERVAL", "5m")
	_, stages, err := loadProfile()
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 4 {
		t.Fatalf("unexpected stages: %+v", stages)
	}
	for _, s := range stages {
		if s.ReadRPS != 50 || s.FlagRPS != 2 || s.Overload {
			t.Fatalf("checker stage changed the requested pressure: %+v", s)
		}
	}
}

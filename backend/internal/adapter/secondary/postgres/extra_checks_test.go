package postgres

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestExtraChecksHaveDurableTimesAndLeaveMainWorkflowUnchanged(t *testing.T) {
	payload := domain.JobPayload{Workflow: "classic", Task: domain.Task{CheckerTimeout: 10, Puts: 2, Gets: 2}}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	original := schedulerJobRecord{RunID: uuid.NewV7(), TeamID: uuid.NewV7(), TaskID: uuid.NewV7(), Round: 1, Action: "classic", Payload: body}
	start := time.Unix(1000, 0)
	jobs, err := addExtraChecks([]schedulerJobRecord{original}, start, 60, 2, strings.NewReader(strings.Repeat("\x00", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 3 || string(jobs[0].Payload) != string(body) || jobs[0].NotBefore != nil || jobs[0].ExpiresAt != nil {
		t.Fatalf("main workflow changed: %+v", jobs)
	}
	for index, job := range jobs[1:] {
		if !job.AggregateRound || job.Action != "check_only" || job.RunID != original.RunID ||
			job.TeamID != original.TeamID || job.TaskID != original.TaskID || job.Round != 1 {
			t.Fatalf("extra check identity changed: %+v", job)
		}
		lower := start.Add(time.Second + time.Duration(index)*23500*time.Millisecond)
		upper := lower.Add(23500 * time.Millisecond)
		if job.NotBefore.Before(lower) || !job.NotBefore.Before(upper) || !job.ExpiresAt.Equal(start.Add(time.Minute)) {
			t.Fatalf("check outside its reserved window: %+v", job)
		}
	}
	if _, err := addExtraChecks([]schedulerJobRecord{original}, start, 13, 2, strings.NewReader("")); err == nil {
		t.Fatal("accepted a round without execution margin")
	}
	if _, err := addExtraChecks([]schedulerJobRecord{original}, start, 60, 2, strings.NewReader("")); err == nil {
		t.Fatal("accepted failed random source")
	}
}

func TestAggregateResultsRequiresAllChecksAndKeepsFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		statuses []int32
		want     int32
		counted  bool
	}{
		{"all up", []int32{101, 101, 101}, 101, true},
		{"down then up", []int32{104, 101, 101}, 104, true},
		{"up then down", []int32{101, 101, 104}, 104, true},
		{"infrastructure failure", []int32{101, 110, 101}, 110, false},
		{"down and infrastructure failure", []int32{110, 104, 101}, 104, true},
		{"down before infrastructure failure", []int32{104, 110, 101}, 104, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := make([]sqlc.GroupedCheckerResultsRow, 0, len(test.statuses))
			for _, status := range test.statuses {
				body, err := json.Marshal(domain.CheckerResult{Status: status})
				if err != nil {
					t.Fatal(err)
				}
				rows = append(rows, sqlc.GroupedCheckerResultsRow{FinishedAt: timestamp(time.Now()), Result: body})
			}
			got, ready, counted, err := aggregateResults(rows)
			if err != nil || !ready || got.Status != test.want || counted != test.counted {
				t.Fatalf("aggregate = %+v, %t, %t, %v", got, ready, counted, err)
			}
			rows[2].FinishedAt.Valid = false
			if _, ready, _, err := aggregateResults(rows); err != nil || ready {
				t.Fatalf("unfinished group was accounted: ready=%t err=%v", ready, err)
			}
		})
	}
}

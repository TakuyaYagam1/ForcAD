package postgres_test

import (
	"errors"
	"math"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestSubmissionRollsBackScoresWhenEventDeltaWriteFails(t *testing.T) {
	for _, fault := range []struct {
		name string
		body string
	}{
		{
			name: "database error",
			body: `BEGIN RAISE EXCEPTION 'event delta storage unavailable'; END`,
		},
		// The forced partial branch below updates exactly one new event.
		{
			name: "one row skipped",
			body: `BEGIN RETURN NULL; END`,
		},
	} {
		t.Run(fault.name, func(t *testing.T) {
			f := newSubmissionFixture(t)
			seedAsymmetricSubmissionScores(t, f)
			values := f.flags(t, 1, 1, 2)
			var before string
			var first domain.SubmissionResult
			// A commits only after B's flags have been read. B must take the
			// partial-conflict branch; this is not a pre-existing duplicate test.
			results, trace, err := submitWithStaleFlagRead(t, f, values, func() {
				winner, winnerErr := f.repo.SubmitBatch(t.Context(), f.request(0, values[:1]))
				if winnerErr != nil || len(winner) != 1 || winner[0].Err != nil {
					t.Fatalf("A before event-storage fault: %+v, %v", winner, winnerErr)
				}
				first = winner[0]
				before = submissionScoreSnapshot(t, f)
				_, setupErr := f.pool.Exec(t.Context(), `CREATE FUNCTION fail_event_delta() RETURNS trigger LANGUAGE plpgsql AS $$ `+fault.body+` $$;
CREATE TRIGGER fail_event_delta BEFORE UPDATE ON StolenFlags FOR EACH ROW EXECUTE FUNCTION fail_event_delta()`)
				if setupErr != nil {
					t.Fatal(setupErr)
				}
			})
			if err == nil || results != nil {
				t.Fatalf("failed event storage produced committed verdicts: %+v, %v", results, err)
			}
			if after := submissionScoreSnapshot(t, f); after != before {
				t.Fatalf("failed event storage committed score changes: before=%s after=%s", before, after)
			}
			var records int
			if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM StolenFlags`).Scan(&records); err != nil {
				t.Fatal(err)
			}
			if records != 1 {
				t.Fatalf("failed fallback changed A's sole committed record count: %d", records)
			}
			assertSubmissionEventDeltas(t, f, map[string]float64{
				first.Attack.EventID: first.Attack.AttackerDelta,
			})
			if trace.fast.Load() != 1 || trace.fallback.Load() != 1 || trace.legacy.Load() != 0 || trace.commits.Load() != 0 {
				t.Fatalf("event fault did not exercise partial fallback: fast/fallback/legacy/commit=%d/%d/%d/%d", trace.fast.Load(), trace.fallback.Load(), trace.legacy.Load(), trace.commits.Load())
			}

			if _, err := f.pool.Exec(t.Context(), `DROP TRIGGER fail_event_delta ON StolenFlags`); err != nil {
				t.Fatal(err)
			}
			results, err = f.repo.SubmitBatch(t.Context(), f.request(0, values))
			if err != nil || len(results) != 2 {
				t.Fatalf("retry after storage recovery: %+v, %v", results, err)
			}
			if !errors.Is(results[0].Err, domain.ErrFlagAlreadyStolen) || results[1].Err != nil {
				t.Fatalf("failed fallback consumed its new flag before retry: %+v", results)
			}
			want := map[string]float64{
				first.Attack.EventID:      first.Attack.AttackerDelta,
				results[1].Attack.EventID: results[1].Attack.AttackerDelta,
			}
			assertSubmissionEventDeltas(t, f, want)
		})
	}
}

func TestPartialSubmissionPersistsOnlyNewEventDeltas(t *testing.T) {
	f := newSubmissionFixture(t)
	foreign := f.flags(t, 1, 1, 2)
	own := f.flags(t, 0, 3, 1)
	initial, err := f.repo.SubmitBatch(t.Context(), f.request(0, foreign[:1]))
	if err != nil || len(initial) != 1 || initial[0].Err != nil {
		t.Fatalf("initial submission: %+v, %v", initial, err)
	}
	values := []string{foreign[0], foreign[1], foreign[1], own[0], "unknown"}
	results, err := f.repo.SubmitBatch(t.Context(), f.request(0, values))
	if err != nil || len(results) != len(values) {
		t.Fatalf("partial submission: %+v, %v", results, err)
	}
	for i, want := range []error{domain.ErrFlagAlreadyStolen, nil, domain.ErrFlagAlreadyStolen, domain.ErrOwnFlag, domain.ErrFlagNotFound} {
		if !errors.Is(results[i].Err, want) {
			t.Fatalf("partial verdict %d = %v, want %v", i, results[i].Err, want)
		}
	}
	want := map[string]float64{
		initial[0].Attack.EventID: initial[0].Attack.AttackerDelta,
		results[1].Attack.EventID: results[1].Attack.AttackerDelta,
	}
	assertSubmissionEventDeltas(t, f, want)
	beforeRetry := submissionScoreSnapshot(t, f)
	results, err = f.repo.SubmitBatch(t.Context(), f.request(0, values))
	if err != nil || len(results) != len(values) {
		t.Fatalf("duplicate retry: %+v, %v", results, err)
	}
	for i, want := range []error{domain.ErrFlagAlreadyStolen, domain.ErrFlagAlreadyStolen, domain.ErrFlagAlreadyStolen, domain.ErrOwnFlag, domain.ErrFlagNotFound} {
		if !errors.Is(results[i].Err, want) {
			t.Fatalf("retry verdict %d = %v, want %v", i, results[i].Err, want)
		}
	}
	if afterRetry := submissionScoreSnapshot(t, f); afterRetry != beforeRetry {
		t.Fatalf("duplicate retry changed scores: before=%s after=%s", beforeRetry, afterRetry)
	}
	assertSubmissionEventDeltas(t, f, want)
}

func submissionScoreSnapshot(t *testing.T, f submissionFixture) string {
	t.Helper()
	var snapshot string
	err := f.pool.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(
		jsonb_build_array(team_id, task_id, score, stolen, lost) ORDER BY team_id, task_id), '[]'::JSONB)::TEXT
		FROM TeamTasks`).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertSubmissionEventDeltas(t *testing.T, f submissionFixture, want map[string]float64) {
	t.Helper()
	events, err := f.repo.ListEvents(t.Context(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(want) {
		t.Fatalf("stored event count = %d, want %d: %+v", len(events), len(want), events)
	}
	for _, event := range events {
		delta, ok := want[event.ID]
		if !ok || event.Kind != domain.EventFlagStolen || event.AttackerDelta == nil || math.Abs(*event.AttackerDelta-delta) > 1e-10 {
			t.Fatalf("persisted event differs from committed result: %+v, expected deltas=%v", event, want)
		}
	}
}

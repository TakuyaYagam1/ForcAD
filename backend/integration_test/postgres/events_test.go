package postgres_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestPublicEventsSurviveReconnectAndSupportCursorPaging(t *testing.T) {
	f := newSubmissionFixture(t)
	values := f.flags(t, 1, 1, 2)
	firstReceived := time.Now().Add(-time.Second)
	first := f.request(0, values[:1])
	first.ReceivedAt = firstReceived
	results, err := f.repo.SubmitBatch(t.Context(), first)
	if err != nil || results[0].Err != nil {
		t.Fatalf("submit first flag: result=%+v err=%v", results, err)
	}

	// A fresh repository instance models a reader reconnecting after process loss.
	reconnected := newSubmissionFixtureRepository(f)
	firstPage, err := reconnected.ListEvents(t.Context(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage) != 1 {
		t.Fatalf("first event page has %d events, want one", len(firstPage))
	}
	event := firstPage[0]
	if event.Kind != domain.EventFlagStolen || event.ID == "" || event.CreatedAt.IsZero() {
		t.Fatalf("stored flag event is incomplete: %+v", event)
	}
	if event.AttackerID == nil || *event.AttackerID != f.teams[0] || event.VictimID == nil || *event.VictimID != f.teams[1] || event.TaskID == nil || *event.TaskID != f.task {
		t.Fatalf("public flag event identities: %+v", event)
	}
	if event.AttackerDelta == nil || math.Abs(*event.AttackerDelta-results[0].Attack.AttackerDelta) > 1e-10 {
		t.Fatalf("stored attacker delta = %v, want %v", event.AttackerDelta, results[0].Attack.AttackerDelta)
	}
	if event.CreatedAt.Sub(firstReceived).Abs() >= time.Microsecond {
		t.Fatalf("stored event time = %v, want receipt time %v", event.CreatedAt, firstReceived)
	}

	again, err := f.repo.ListEvents(t.Context(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].ID != event.ID || !again[0].CreatedAt.Equal(event.CreatedAt) {
		t.Fatalf("event identity changed after reconnect: first=%+v again=%+v", event, again)
	}

	second := f.request(0, values[1:])
	second.ReceivedAt = time.Now()
	if _, err := f.repo.SubmitBatch(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	next, err := reconnected.ListEvents(t.Context(), event.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 || next[0].ID == event.ID || next[0].Kind != domain.EventFlagStolen {
		t.Fatalf("events after cursor = %+v, want the second accepted flag", next)
	}
	if _, err := reconnected.ListEvents(t.Context(), "missing-event", 100); !errors.Is(err, domain.ErrEventCursorInvalid) {
		t.Fatalf("unknown cursor error = %v, want ErrEventCursorInvalid", err)
	}
}

func TestPublicAnnouncementIsStoredAndReturnedInFeed(t *testing.T) {
	f := newSubmissionFixture(t)
	created, err := f.repo.CreateAnnouncement(t.Context(), "Round update", "The round starts soon.", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if created.Kind != domain.EventAdminMessage || created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatalf("created announcement is incomplete: %+v", created)
	}
	if created.Title == nil || *created.Title != "Round update" || created.Markdown == nil || *created.Markdown != "The round starts soon." || created.Author == nil || *created.Author != "admin" {
		t.Fatalf("created announcement content: %+v", created)
	}

	feed, err := f.repo.ListEvents(t.Context(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed) != 1 || feed[0].ID != created.ID || feed[0].Kind != domain.EventAdminMessage {
		t.Fatalf("announcement missing from durable feed: %+v", feed)
	}
}

func newSubmissionFixtureRepository(f submissionFixture) *postgres.Repository {
	return postgres.New(f.pool)
}

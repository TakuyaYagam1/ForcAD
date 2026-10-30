package v1

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/prometheus/client_golang/prometheus"
)

type eventServiceFixture struct {
	events      []domain.Event
	after       string
	title       string
	markdown    string
	author      string
	announceErr error
}

func (s *eventServiceFixture) List(_ context.Context, after string) ([]domain.Event, error) {
	s.after = after
	return s.events, nil
}

func (s *eventServiceFixture) Announce(_ context.Context, title, markdown, author string) (domain.Event, error) {
	s.title, s.markdown, s.author = title, markdown, author
	if s.announceErr != nil {
		return domain.Event{}, s.announceErr
	}
	return domain.Event{
		ID:   domain.AnnouncementEventID(uuid.MustParse("01912345-6789-7000-8000-000000000009")),
		Kind: domain.EventAdminMessage, CreatedAt: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC),
		Title: &s.title, Markdown: &s.markdown, Author: &s.author,
	}, nil
}

func TestPublicEventsAndAdminAnnouncementRoute(t *testing.T) {
	attacker, victim, task := httpTeamID, uuid.MustParse("01912345-6789-7000-8000-000000000003"), httpTaskID
	attackerName, victimName, taskName := "Red", "Blue", "Web"
	delta, round := 12.5, int32(7)
	events := &eventServiceFixture{events: []domain.Event{{
		ID:   domain.FlagEventID(uuid.MustParse("01912345-6789-7000-8000-000000000004"), attacker),
		Kind: domain.EventFlagStolen, CreatedAt: time.Date(2026, 10, 28, 11, 59, 0, 0, time.UTC),
		AttackerID: &attacker, AttackerName: &attackerName, AttackerDelta: &delta,
		VictimID: &victim, VictimName: &victimName, TaskID: &task, TaskName: &taskName, Round: &round,
	}}}
	cfg := config.Config{Role: "api", HTTPMaxInflight: 1, RequestTimeout: time.Second}
	noop := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	router, err := NewRouter(cfg, NewHandler(&testGame{}, testAuth{}, &testFlags{}, events), noop, noop, prometheus.NewRegistry(), testLimiter{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	feed := request(router, http.MethodGet, "/api/client/events/", "", nil)
	if feed.Code != http.StatusOK || events.after != "" {
		t.Fatalf("public event feed: status=%d after=%q body=%s", feed.Code, events.after, feed.Body.String())
	}
	for _, publicField := range []string{`"kind":"flag_stolen"`, attacker.String(), victim.String(), task.String(), `"attacker_delta":12.5`, `"round":7`} {
		if !strings.Contains(feed.Body.String(), publicField) {
			t.Errorf("public event response lacks %s: %s", publicField, feed.Body.String())
		}
	}
	for _, privateField := range []string{"flag_value", "private_data", "private_message", "SECRET-FLAG"} {
		if strings.Contains(feed.Body.String(), privateField) {
			t.Errorf("public event response contains private field %q: %s", privateField, feed.Body.String())
		}
	}
	page := request(router, http.MethodGet, "/api/client/events/?after=cursor-id", "", nil)
	if page.Code != http.StatusOK || events.after != "cursor-id" {
		t.Fatalf("event cursor: status=%d after=%q body=%s", page.Code, events.after, page.Body.String())
	}

	body := `{"title":"Round update","markdown":"Starts soon."}`
	if response := request(router, http.MethodPost, "/api/admin/events/", body, nil); response.Code != http.StatusForbidden {
		t.Fatalf("anonymous announcement: %d %s", response.Code, response.Body.String())
	}
	created := request(router, http.MethodPost, "/api/admin/events/", body, map[string]string{"Cookie": "session=valid", "X-CSRF-Token": strings.Repeat("c", 64)})
	if created.Code != http.StatusCreated {
		t.Fatalf("admin announcement: %d %s", created.Code, created.Body.String())
	}
	if events.title != "Round update" || events.markdown != "Starts soon." || events.author != "admin" {
		t.Fatalf("announcement inputs title=%q markdown=%q author=%q", events.title, events.markdown, events.author)
	}
	if !strings.Contains(created.Body.String(), `"kind":"admin_message"`) || !strings.Contains(created.Body.String(), `"author":"admin"`) {
		t.Fatalf("announcement response is incomplete: %s", created.Body.String())
	}
}

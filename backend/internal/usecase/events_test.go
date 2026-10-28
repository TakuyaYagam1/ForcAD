package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type eventRepositoryFixture struct {
	after       string
	limit       int
	created     domain.Event
	title       string
	markdown    string
	author      string
	listErr     error
	createErr   error
	listCalls   int
	createCalls int
}

func (r *eventRepositoryFixture) ListEvents(_ context.Context, after string, limit int) ([]domain.Event, error) {
	r.after, r.limit = after, limit
	r.listCalls++
	if r.listErr != nil {
		return nil, r.listErr
	}
	return []domain.Event{r.created}, nil
}

func (r *eventRepositoryFixture) CreateAnnouncement(_ context.Context, title, markdown, author string) (domain.Event, error) {
	r.title, r.markdown, r.author = title, markdown, author
	r.createCalls++
	if r.createErr != nil {
		return domain.Event{}, r.createErr
	}
	return r.created, nil
}

type eventPublisherFixture struct {
	event   string
	payload any
	err     error
	calls   int
}

func (p *eventPublisherFixture) Publish(_ context.Context, event string, payload any) error {
	p.event, p.payload = event, payload
	p.calls++
	return p.err
}

func TestEventListUsesStableCursorAndHardPageLimit(t *testing.T) {
	repository := &eventRepositoryFixture{}
	service := NewEvents(repository, nil, slog.New(slog.DiscardHandler))
	if _, err := service.List(t.Context(), "flag_stolen:flag:team"); err != nil {
		t.Fatal(err)
	}
	if repository.after != "flag_stolen:flag:team" || repository.limit != EventPageSize {
		t.Fatalf("event query after=%q limit=%d", repository.after, repository.limit)
	}
	if _, err := service.List(t.Context(), strings.Repeat("x", maxEventCursorLength+1)); !errors.Is(err, domain.ErrEventCursorInvalid) {
		t.Fatalf("oversized cursor error = %v, want ErrEventCursorInvalid", err)
	}
	if repository.listCalls != 1 {
		t.Fatalf("invalid cursor reached storage: calls=%d", repository.listCalls)
	}
}

func TestAnnouncementIsSavedBeforeBestEffortNotification(t *testing.T) {
	announcementID := uuid.MustParse("01912345-6789-7000-8000-000000000001")
	created := domain.Event{
		ID: domain.AnnouncementEventID(announcementID), Kind: domain.EventAdminMessage,
		CreatedAt: time.Now(),
	}
	repository := &eventRepositoryFixture{created: created}
	publisher := &eventPublisherFixture{err: errors.New("event transport unavailable")}
	service := NewEvents(repository, publisher, slog.New(slog.DiscardHandler))
	got, err := service.Announce(t.Context(), "  Round update  ", "  Starts soon.  ", " admin ")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || repository.createCalls != 1 || repository.title != "Round update" || repository.markdown != "Starts soon." || repository.author != "admin" {
		t.Fatalf("saved announcement = %+v, stored title=%q markdown=%q author=%q calls=%d", got, repository.title, repository.markdown, repository.author, repository.createCalls)
	}
	if publisher.calls != 1 || publisher.event != string(domain.EventAdminMessage) {
		t.Fatalf("announcement publish event=%q calls=%d", publisher.event, publisher.calls)
	}
	payload, ok := publisher.payload.(map[string]any)
	if !ok || payload["data"] != created {
		t.Fatalf("announcement publish payload = %#v", publisher.payload)
	}
}

func TestAnnouncementRejectsEmptyAndOversizedTextBeforeStorage(t *testing.T) {
	repository := &eventRepositoryFixture{}
	service := NewEvents(repository, nil, slog.New(slog.DiscardHandler))
	for _, input := range []struct {
		title, markdown, author string
	}{
		{" ", "Body", "admin"},
		{"Title", "\n", "admin"},
		{strings.Repeat("a", maxAnnouncementTitleRunes+1), "Body", "admin"},
		{"Title", strings.Repeat("a", maxAnnouncementBodyRunes+1), "admin"},
	} {
		if _, err := service.Announce(t.Context(), input.title, input.markdown, input.author); err == nil {
			t.Errorf("invalid announcement accepted: title length=%d markdown length=%d", utf8.RuneCountInString(input.title), utf8.RuneCountInString(input.markdown))
		}
	}
	if repository.createCalls != 0 {
		t.Fatalf("invalid announcement reached storage %d times", repository.createCalls)
	}
}

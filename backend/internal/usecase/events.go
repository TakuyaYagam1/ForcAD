package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

const (
	EventPageSize              = 100
	maxEventCursorLength       = 128
	maxAnnouncementTitleRunes  = 160
	maxAnnouncementBodyRunes   = 4000
	maxAnnouncementAuthorRunes = 160
)

type EventRepository interface {
	ListEvents(ctx context.Context, after string, limit int) ([]domain.Event, error)
	CreateAnnouncement(ctx context.Context, title, markdown, author string) (domain.Event, error)
}

type EventPublisher interface {
	Publish(ctx context.Context, event string, payload any) error
}

type Events struct {
	repository EventRepository
	publisher  EventPublisher
	logger     *slog.Logger
}

func NewEvents(repository EventRepository, publisher EventPublisher, logger *slog.Logger) *Events {
	return &Events{repository: repository, publisher: publisher, logger: logger}
}

func (e *Events) List(ctx context.Context, after string) ([]domain.Event, error) {
	const location = "usecase.Events.List"
	if len(after) > maxEventCursorLength {
		return nil, fmt.Errorf("%s - validate cursor: %w", location, domain.ErrEventCursorInvalid)
	}
	rows, err := e.repository.ListEvents(ctx, after, EventPageSize)
	if err != nil {
		return nil, fmt.Errorf("%s - read public events: %w", location, err)
	}
	return rows, nil
}

func (e *Events) Announce(ctx context.Context, title, markdown, author string) (domain.Event, error) {
	const location = "usecase.Events.Announce"
	title = strings.TrimSpace(title)
	markdown = strings.TrimSpace(markdown)
	author = strings.TrimSpace(author)
	if utf8.RuneCountInString(title) == 0 || utf8.RuneCountInString(title) > maxAnnouncementTitleRunes {
		return domain.Event{}, fmt.Errorf("%s - validate title: %w", location, &domain.ValidationError{Message: "Title must contain 1 to 160 characters"})
	}
	if utf8.RuneCountInString(markdown) == 0 || utf8.RuneCountInString(markdown) > maxAnnouncementBodyRunes {
		return domain.Event{}, fmt.Errorf("%s - validate markdown: %w", location, &domain.ValidationError{Message: "Markdown must contain 1 to 4000 characters"})
	}
	if utf8.RuneCountInString(author) == 0 || utf8.RuneCountInString(author) > maxAnnouncementAuthorRunes {
		return domain.Event{}, fmt.Errorf("%s - validate author: %w", location, &domain.ValidationError{Message: "Author must contain 1 to 160 characters"})
	}
	created, err := e.repository.CreateAnnouncement(ctx, title, markdown, author)
	if err != nil {
		return domain.Event{}, fmt.Errorf("%s - save announcement: %w", location, err)
	}
	if e.publisher != nil {
		if err := e.publisher.Publish(ctx, string(domain.EventAdminMessage), map[string]any{"data": created}); err != nil && ctx.Err() == nil {
			e.logger.Warn("announcement notification unavailable", "error", err)
		}
	}
	return created, nil
}

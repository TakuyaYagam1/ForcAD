package postgres

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres/sqlc"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const maxPublicEventPageSize = 100

func (r *Repository) ListEvents(ctx context.Context, after string, limit int) ([]domain.Event, error) {
	const location = "postgres.Repository.ListEvents"
	if limit <= 0 {
		return []domain.Event{}, nil
	}
	limit = min(limit, maxPublicEventPageSize)
	if after == "" {
		rows, err := r.queries.ListLatestPublicEvents(ctx, int32(limit))
		if err != nil {
			return nil, fmt.Errorf("%s - query latest events: %w", location, err)
		}
		return mapEvents(rows)
	}
	if len(after) > 128 {
		return nil, fmt.Errorf("%s - validate cursor: %w", location, domain.ErrEventCursorInvalid)
	}
	cursor, err := r.eventCursor(ctx, after)
	if err != nil {
		return nil, err
	}
	rows, err := r.queries.ListPublicEventsAfter(ctx, sqlc.ListPublicEventsAfterParams{
		EventID: after, CursorTime: cursor, PageSize: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("%s - query events after cursor: %w", location, err)
	}
	return mapEvents(rows)
}

func (r *Repository) eventCursor(ctx context.Context, value string) (pgtype.Timestamptz, error) {
	const location = "postgres.Repository.eventCursor"
	key, err := parseEventCursor(value)
	if err != nil {
		return pgtype.Timestamptz{}, err
	}
	if key.kind == domain.EventFlagStolen {
		cursor, err := r.queries.ReadFlagEventCursor(ctx, sqlc.ReadFlagEventCursorParams{
			FlagID: key.flagID, AttackerID: key.attackerID,
		})
		return validateEventCursorRead(location, "flag", cursor, err)
	}
	cursor, err := r.queries.ReadAnnouncementEventCursor(ctx, key.announcementID)
	return validateEventCursorRead(location, "announcement", cursor, err)
}

type eventCursorKey struct {
	kind           domain.EventKind
	flagID         uuid.UUID
	attackerID     uuid.UUID
	announcementID uuid.UUID
}

func parseEventCursor(value string) (eventCursorKey, error) {
	parts := strings.Split(value, ":")
	switch {
	case len(parts) == 3 && parts[0] == string(domain.EventFlagStolen):
		flagID, err := uuid.Parse(parts[1])
		if err != nil {
			return eventCursorKey{}, invalidEventCursor()
		}
		attackerID, err := uuid.Parse(parts[2])
		if err != nil || domain.FlagEventID(flagID, attackerID) != value {
			return eventCursorKey{}, invalidEventCursor()
		}
		return eventCursorKey{kind: domain.EventFlagStolen, flagID: flagID, attackerID: attackerID}, nil
	case len(parts) == 2 && parts[0] == string(domain.EventAdminMessage):
		id, err := uuid.Parse(parts[1])
		if err != nil || domain.AnnouncementEventID(id) != value {
			return eventCursorKey{}, invalidEventCursor()
		}
		return eventCursorKey{kind: domain.EventAdminMessage, announcementID: id}, nil
	default:
		return eventCursorKey{}, invalidEventCursor()
	}
}

func validateEventCursorRead(location, source string, cursor pgtype.Timestamptz, err error) (pgtype.Timestamptz, error) {
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !cursor.Valid {
		return pgtype.Timestamptz{}, invalidEventCursor()
	}
	if err != nil {
		return pgtype.Timestamptz{}, fmt.Errorf("%s - read %s event cursor: %w", location, source, err)
	}
	return cursor, nil
}

func invalidEventCursor() error {
	return fmt.Errorf("postgres.Repository.eventCursor - validate cursor: %w", domain.ErrEventCursorInvalid)
}

func (r *Repository) CreateAnnouncement(ctx context.Context, title, markdown, author string) (domain.Event, error) {
	const location = "postgres.Repository.CreateAnnouncement"
	row, err := r.queries.InsertPublicAnnouncement(ctx, sqlc.InsertPublicAnnouncementParams{
		Title: title, Markdown: markdown, Author: author,
	})
	if err != nil {
		return domain.Event{}, fmt.Errorf("%s - insert public announcement: %w", location, gameDBError(err))
	}
	id := domain.AnnouncementEventID(row.ID)
	return domain.Event{
		ID: id, Kind: domain.EventAdminMessage, CreatedAt: row.CreatedAt.Time,
		Title: new(row.Title), Markdown: new(row.Markdown), Author: new(row.Author),
	}, nil
}

func mapEvents(payloads []string) ([]domain.Event, error) {
	result := make([]domain.Event, 0, len(payloads))
	for _, payload := range payloads {
		event, err := decodeEvent(payload)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, nil
}

func decodeEvent(data string) (domain.Event, error) {
	const location = "postgres.decodeEvent"
	var event domain.Event
	// The SQL query builds a fixed shape, so reject fields the domain cannot represent.
	if err := jsonv2.Unmarshal([]byte(data), &event, jsonv2.RejectUnknownMembers(true)); err != nil {
		return domain.Event{}, fmt.Errorf("%s - decode public event: %w", location, err)
	}
	if event.ID == "" || event.CreatedAt.IsZero() {
		return domain.Event{}, fmt.Errorf("%s - validate stored event: %w", location, domain.ErrNotFound)
	}
	return event, nil
}

var _ outbound.EventRepository = (*Repository)(nil)

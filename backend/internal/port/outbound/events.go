package outbound

import (
	"context"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type EventRepository interface {
	ListEvents(ctx context.Context, after string, limit int) ([]domain.Event, error)
	CreateAnnouncement(
		ctx context.Context,
		title, markdown, author string,
	) (domain.Event, error)
}

package outbound

import (
	"context"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type CheckerLogRepository interface {
	CheckerLogs(
		ctx context.Context,
		teamID, taskID uuid.UUID,
		beforeID *uuid.UUID,
		limit int32,
	) ([]domain.CheckerLog, error)
}

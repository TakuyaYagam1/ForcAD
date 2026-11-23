package request

import (
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
)

// DomainUUIDs converts API UUIDs to domain UUIDs without filtering or reordering them.
func DomainUUIDs(ids []api.UUID) []uuid.UUID {
	result := make([]uuid.UUID, len(ids))
	for i, id := range ids {
		result[i] = uuid.UUID(id)
	}
	return result
}

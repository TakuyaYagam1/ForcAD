package request

import (
	"uuid"
)

func CheckerLogCursor(value string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(value)
	return parsed, err == nil
}

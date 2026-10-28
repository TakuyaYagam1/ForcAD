package postgres

import (
	"errors"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestEventCursorRejectsUnknownAndNoncanonicalIDsBeforeQuery(t *testing.T) {
	repository := &Repository{}
	for _, cursor := range []string{
		"unknown-event",
		"flag_stolen:not-a-uuid:also-invalid",
		"admin_message:01912345-6789-7000-8000-00000000000A",
		"flag_stolen:01912345-6789-7000-8000-000000000001:01912345-6789-7000-8000-000000000002:extra",
	} {
		if _, err := repository.eventCursor(t.Context(), cursor); !errors.Is(err, domain.ErrEventCursorInvalid) {
			t.Errorf("eventCursor(%q) error = %v, want ErrEventCursorInvalid", cursor, err)
		}
	}
}

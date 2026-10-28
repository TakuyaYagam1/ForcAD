package postgres

import (
	"testing"
	"time"
)

func TestDecodeEventUsesExactNamesAndTypedJSONValues(t *testing.T) {
	const payload = `{"id":"flag_stolen:flag:attacker","kind":"flag_stolen","created_at":"2025-03-04T05:06:07.123456789Z","attacker_id":"01912345-6789-7000-8000-000000000001","attacker_name":"red","attacker_delta":-1.25,"victim_id":"01912345-6789-7000-8000-000000000002","victim_name":"blue","task_id":"01912345-6789-7000-8000-000000000003","task_name":"web","round":9}`
	event, err := decodeEvent(payload)
	if err != nil {
		t.Fatal(err)
	}
	wantTime := time.Date(2025, time.March, 4, 5, 6, 7, 123456789, time.UTC)
	if event.ID != "flag_stolen:flag:attacker" || event.Kind != "flag_stolen" || !event.CreatedAt.Equal(wantTime) {
		t.Fatalf("event identity or timestamp = %+v", event)
	}
	if event.AttackerDelta == nil || *event.AttackerDelta != -1.25 || event.Round == nil || *event.Round != 9 {
		t.Fatalf("event numeric fields = %+v", event)
	}
	if event.AttackerID == nil || event.AttackerID.String() != "01912345-6789-7000-8000-000000000001" ||
		event.VictimID == nil || event.VictimID.String() != "01912345-6789-7000-8000-000000000002" ||
		event.TaskID == nil || event.TaskID.String() != "01912345-6789-7000-8000-000000000003" {
		t.Fatalf("event identity fields = %+v", event)
	}

	announcement, err := decodeEvent(`{"id":"admin_message:announcement","kind":"admin_message","created_at":"2025-03-04T05:06:07Z","title":"Notice","markdown":"Round starts soon","author":"admin"}`)
	if err != nil {
		t.Fatal(err)
	}
	if announcement.Title == nil || *announcement.Title != "Notice" ||
		announcement.Markdown == nil || *announcement.Markdown != "Round starts soon" ||
		announcement.Author == nil || *announcement.Author != "admin" {
		t.Fatalf("announcement fields = %+v", announcement)
	}
}

func TestDecodeEventRejectsAmbiguousOrUnknownJSON(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
	}{
		{name: "unknown field", payload: `{"id":"event-1","kind":"admin_message","created_at":"2025-03-04T05:06:07Z","extra":true}`},
		{name: "duplicate field", payload: `{"id":"event-1","id":"event-2","kind":"admin_message","created_at":"2025-03-04T05:06:07Z"}`},
		{name: "wrong field case", payload: `{"id":"event-1","kind":"admin_message","created_at":"2025-03-04T05:06:07Z","Created_At":"2025-03-04T05:06:08Z"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeEvent(test.payload); err == nil {
				t.Fatalf("decodeEvent(%s) succeeded", test.payload)
			}
		})
	}
}

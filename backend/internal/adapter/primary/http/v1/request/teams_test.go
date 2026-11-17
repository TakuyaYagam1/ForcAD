package request

import (
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestCreateTeamKindDefaultsToPlayer(t *testing.T) {
	team := CreateTeam(&api.AdminTeamCreate{})
	if team.Kind != domain.TeamKindPlayer {
		t.Fatalf("default team kind = %q, want %q", team.Kind, domain.TeamKindPlayer)
	}

	team = CreateTeam(&api.AdminTeamCreate{
		Kind: api.NewOptTeamKind(api.TeamKindNpc),
	})
	if team.Kind != domain.TeamKindNPC {
		t.Fatalf("explicit team kind = %q, want %q", team.Kind, domain.TeamKindNPC)
	}
}

func TestUpdateTeamKindOmissionIsPreserved(t *testing.T) {
	id := uuid.MustParse("01912345-6789-7000-8000-000000000001")
	team := UpdateTeam(&api.AdminTeamUpdate{}, id)
	if team.Kind != "" {
		t.Fatalf("omitted update kind = %q, want preservation sentinel", team.Kind)
	}

	team = UpdateTeam(&api.AdminTeamUpdate{
		Kind: api.NewOptTeamKind(api.TeamKindNpc),
	}, id)
	if team.Kind != domain.TeamKindNPC {
		t.Fatalf("explicit update kind = %q, want %q", team.Kind, domain.TeamKindNPC)
	}
}

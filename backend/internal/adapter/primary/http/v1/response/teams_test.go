package response

import (
	"testing"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestTeamResponsesExposeKind(t *testing.T) {
	team := domain.Team{Kind: domain.TeamKindNPC}
	if got := PublicTeam(team.Public()).Kind; got != api.TeamKindNpc {
		t.Fatalf("public team kind = %q, want %q", got, api.TeamKindNpc)
	}
	if got := AdminTeam(team).Kind; got != api.TeamKindNpc {
		t.Fatalf("admin team kind = %q, want %q", got, api.TeamKindNpc)
	}
	if got := PublicTeam(domain.PublicTeam{}).Kind; got != api.TeamKindPlayer {
		t.Fatalf("default public team kind = %q, want %q", got, api.TeamKindPlayer)
	}
}

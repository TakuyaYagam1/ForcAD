package response

import (
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func PublicTeam(t domain.PublicTeam) api.ParticipantTeam {
	return api.ParticipantTeam{
		ID:          api.UUID(t.ID),
		Name:        t.Name,
		IP:          t.IP,
		Kind:        responseTeamKind(t.Kind),
		Highlighted: t.Highlighted,
		Active:      t.Active,
		LogoPath:    t.LogoPath,
	}
}

func AdminTeam(t domain.Team) *api.AdminTeam {
	return &api.AdminTeam{
		ID:          api.UUID(t.ID),
		Name:        t.Name,
		IP:          t.IP,
		Token:       t.Token,
		Kind:        responseTeamKind(t.Kind),
		Highlighted: t.Highlighted,
		Active:      t.Active,
		LogoPath:    t.LogoPath,
	}
}

func responseTeamKind(kind string) api.TeamKind {
	if kind == "" {
		return api.TeamKindPlayer
	}
	return api.TeamKind(kind)
}

func AdminTeams(teams []domain.Team) *api.GetAdminTeamsOKApplicationJSON {
	result := make(api.GetAdminTeamsOKApplicationJSON, 0, len(teams))
	for _, team := range teams {
		result = append(result, *AdminTeam(team))
	}
	return &result
}

func PublicTeams(teams []domain.Team) []api.ParticipantTeam {
	result := make([]api.ParticipantTeam, 0, len(teams))
	for _, team := range teams {
		result = append(result, PublicTeam(team.Public()))
	}
	return result
}

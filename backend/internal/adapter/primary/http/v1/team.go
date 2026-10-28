package v1

import (
	"context"
	"fmt"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type teamsHandler struct {
	game GameService
}

func (h *teamsHandler) GetAdminTeams(ctx context.Context) (api.GetAdminTeamsRes, error) {
	const location = "httpv1.teamsHandler.GetAdminTeams"
	teams, err := h.game.Teams(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("%s - list teams: %w", location, err)
	}
	result := make(api.GetAdminTeamsOKApplicationJSON, 0, len(teams))
	for _, team := range teams {
		result = append(result, *adminTeam(team))
	}
	return &result, nil
}

func (h *teamsHandler) GetAdminTeam(ctx context.Context, params api.GetAdminTeamParams) (api.GetAdminTeamRes, error) {
	const location = "httpv1.teamsHandler.GetAdminTeam"
	teams, err := h.game.Teams(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("%s - list teams: %w", location, err)
	}
	for _, team := range teams {
		if team.ID == params.TeamID {
			return adminTeam(team), nil
		}
	}
	return nil, fmt.Errorf("%s - find team: %w", location, domain.ErrNotFound)
}

func (h *teamsHandler) CreateAdminTeam(ctx context.Context, req *api.AdminTeamCreate) (api.CreateAdminTeamRes, error) {
	const location = "httpv1.teamsHandler.CreateAdminTeam"
	team, err := h.game.SaveTeam(ctx, domain.Team{Name: req.Name, IP: req.IP, Highlighted: req.Highlighted.Or(false), Active: req.Active.Or(true), LogoPath: req.LogoPath.Or("")}, true)
	if err != nil {
		return nil, fmt.Errorf("%s - create team: %w", location, err)
	}
	return adminTeam(team), nil
}

func (h *teamsHandler) UpdateAdminTeam(ctx context.Context, req *api.AdminTeamUpdate, params api.UpdateAdminTeamParams) (api.UpdateAdminTeamRes, error) {
	const location = "httpv1.teamsHandler.UpdateAdminTeam"
	team, err := h.game.SaveTeam(ctx, domain.Team{ID: params.TeamID, Name: req.Name, IP: req.IP, Token: req.Token, Highlighted: req.Highlighted.Or(false), Active: req.Active.Or(true), LogoPath: req.LogoPath.Or("")}, false)
	if err != nil {
		return nil, fmt.Errorf("%s - update team: %w", location, err)
	}
	return adminTeam(team), nil
}

func (h *teamsHandler) DeleteAdminTeam(ctx context.Context, params api.DeleteAdminTeamParams) (api.DeleteAdminTeamRes, error) {
	const location = "httpv1.teamsHandler.DeleteAdminTeam"
	if err := h.game.DeleteTeam(ctx, params.TeamID); err != nil {
		return nil, fmt.Errorf("%s - deactivate team: %w", location, err)
	}
	return new(api.DeleteAdminTeamOK("ok")), nil
}

func (h *teamsHandler) GetClientTeams(ctx context.Context) ([]api.ParticipantTeam, error) {
	const location = "httpv1.teamsHandler.GetClientTeams"
	teams, err := h.game.Teams(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("%s - list public teams: %w", location, err)
	}
	result := make([]api.ParticipantTeam, 0, len(teams))
	for _, team := range teams {
		result = append(result, publicTeam(team.Public()))
	}
	return result, nil
}

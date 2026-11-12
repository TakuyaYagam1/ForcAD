package request

import (
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func CreateTeam(req *api.AdminTeamCreate) domain.Team {
	return domain.Team{
		Name:        req.Name,
		IP:          req.IP,
		Kind:        string(req.Kind.Or(api.TeamKindPlayer)),
		Highlighted: req.Highlighted.Or(false),
		Active:      req.Active.Or(true),
		LogoPath:    req.LogoPath.Or(""),
	}
}

func UpdateTeam(req *api.AdminTeamUpdate, id uuid.UUID) domain.Team {
	return domain.Team{
		ID:          id,
		Name:        req.Name,
		IP:          req.IP,
		Token:       req.Token,
		Kind:        string(req.Kind.Or(api.TeamKind(""))),
		Highlighted: req.Highlighted.Or(false),
		Active:      req.Active.Or(true),
		LogoPath:    req.LogoPath.Or(""),
	}
}

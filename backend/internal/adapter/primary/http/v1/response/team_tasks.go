package response

import (
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func PublicHistory(c domain.PublicCell) api.ParticipantTeamTaskHistory {
	return api.ParticipantTeamTaskHistory{
		TeamID:       api.UUID(c.TeamID),
		TaskID:       api.UUID(c.TaskID),
		Round:        c.Round,
		Status:       c.Status,
		Stolen:       c.Stolen,
		Lost:         c.Lost,
		Score:        c.Score,
		Checks:       c.Checks,
		ChecksPassed: c.ChecksPassed,
		Timestamp:    c.Timestamp,
		Message:      api.NewNilString(c.Message),
	}
}

func AdminHistory(c domain.Cell) api.AdminTeamTaskLogEntry {
	return api.AdminTeamTaskLogEntry{
		ID:             api.UUID(c.ID),
		TeamID:         api.UUID(c.TeamID),
		TaskID:         api.UUID(c.TaskID),
		Round:          c.Round,
		Status:         c.Status,
		Stolen:         c.Stolen,
		Lost:           c.Lost,
		Score:          c.Score,
		Checks:         c.Checks,
		ChecksPassed:   c.ChecksPassed,
		Ts:             float64(c.CreatedAt.UnixMicro()) / 1e6,
		PublicMessage:  api.NewNilString(c.PublicMessage),
		PrivateMessage: api.NewNilString(c.PrivateMessage),
		Command:        api.NewNilString(c.Command),
	}
}

func AdminTeamTaskHistory(rows []domain.Cell) *api.GetAdminTeamTaskHistoryOKApplicationJSON {
	result := make(api.GetAdminTeamTaskHistoryOKApplicationJSON, 0, len(rows))
	for _, row := range rows {
		result = append(result, AdminHistory(row))
	}
	return &result
}

func PublicTeamHistory(rows []domain.PublicCell) []api.ParticipantTeamTaskHistory {
	result := make([]api.ParticipantTeamTaskHistory, 0, len(rows))
	for _, row := range rows {
		result = append(result, PublicHistory(row))
	}
	return result
}

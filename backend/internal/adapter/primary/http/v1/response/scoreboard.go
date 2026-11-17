package response

import (
	"fmt"
	"math"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase"
)

func Scoreboard(b domain.Scoreboard) *api.Scoreboard {
	result := &api.Scoreboard{
		Config:  GameConfig(b.Config),
		Runtime: *RuntimeStatus(b.Runtime),
		Teams:   make([]api.ParticipantTeam, 0, len(b.Teams)),
		Tasks:   make([]api.ParticipantTask, 0, len(b.Tasks)),
		State: api.GameState{
			Round:      b.State.Round,
			RoundStart: b.State.RoundStart,
			TeamTasks:  make([]api.ScoreboardTeamTask, 0, len(b.State.TeamTasks)),
		},
	}
	for _, t := range b.Teams {
		result.Teams = append(result.Teams, PublicTeam(t))
	}
	for _, t := range b.Tasks {
		result.Tasks = append(result.Tasks, PublicTask(t))
	}
	for _, c := range b.State.TeamTasks {
		result.State.TeamTasks = append(result.State.TeamTasks, api.ScoreboardTeamTask{
			TeamID:       api.UUID(c.TeamID),
			TaskID:       api.UUID(c.TaskID),
			Status:       c.Status,
			Stolen:       c.Stolen,
			Lost:         c.Lost,
			Score:        c.Score,
			Checks:       c.Checks,
			ChecksPassed: c.ChecksPassed,
			Message:      api.NewNilString(c.Message),
		})
	}
	return result
}

func CtftimeStandings(rows []usecase.Standing) (*api.GetClientCtftimeScoreboardOKApplicationJSON, error) {
	const location = "httpv1.response.CtftimeStandings"
	result := make(api.GetClientCtftimeScoreboardOKApplicationJSON, 0, len(rows))
	for _, row := range rows {
		if row.Pos < 1 || row.Pos > math.MaxInt32 {
			return nil, fmt.Errorf("%s - standings position exceeds API range", location)
		}
		result = append(result, api.CtftimeStanding{
			Pos:   int32(row.Pos),
			Team:  row.Team,
			Score: row.Score,
		})
	}
	return &result, nil
}

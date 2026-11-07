package response

import (
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func AdminTask(t domain.Task) *api.AdminTask {
	return &api.AdminTask{
		ID:             api.UUID(t.ID),
		Name:           t.Name,
		Checker:        t.Checker,
		Gets:           t.Gets,
		Puts:           t.Puts,
		Places:         t.Places,
		CheckerTimeout: t.CheckerTimeout,
		CheckerType:    t.CheckerType,
		EnvPath:        t.EnvPath,
		DefaultScore:   float64(t.DefaultScore),
		GetPeriod:      t.GetPeriod,
		Active:         t.Active,
	}
}

func PublicTask(t domain.PublicTask) api.ParticipantTask {
	return api.ParticipantTask{
		ID:   api.UUID(t.ID),
		Name: t.Name,
	}
}

func AdminTasks(tasks []domain.Task) *api.GetAdminTasksOKApplicationJSON {
	result := make(api.GetAdminTasksOKApplicationJSON, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, *AdminTask(task))
	}
	return &result
}

func PublicTasks(tasks []domain.Task) []api.ParticipantTask {
	result := make([]api.ParticipantTask, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, PublicTask(task.Public()))
	}
	return result
}

package v1

import (
	"context"
	"fmt"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1/response"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/v1/request"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type tasksHandler struct {
	game GameService
}

func (h *tasksHandler) GetAdminTasks(ctx context.Context) (api.GetAdminTasksRes, error) {
	const location = "httpv1.tasksHandler.GetAdminTasks"
	tasks, err := h.game.Tasks(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("%s - list tasks: %w", location, err)
	}
	return response.AdminTasks(tasks), nil
}

func (h *tasksHandler) GetAdminTask(ctx context.Context, params api.GetAdminTaskParams) (api.GetAdminTaskRes, error) {
	const location = "httpv1.tasksHandler.GetAdminTask"
	tasks, err := h.game.Tasks(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("%s - list tasks: %w", location, err)
	}
	for _, task := range tasks {
		if task.ID == params.TaskID {
			return response.AdminTask(task), nil
		}
	}
	return nil, fmt.Errorf("%s - find task: %w", location, domain.ErrNotFound)
}

func (h *tasksHandler) CreateAdminTask(ctx context.Context, req *api.AdminTaskWrite) (api.CreateAdminTaskRes, error) {
	const location = "httpv1.tasksHandler.CreateAdminTask"
	input, err := request.Task(req, uuid.Nil())
	if err != nil {
		return nil, fmt.Errorf("%s - validate task input: %w", location, err)
	}
	task, err := h.game.SaveTask(ctx, input, true)
	if err != nil {
		return nil, fmt.Errorf("%s - create task: %w", location, err)
	}
	return response.AdminTask(task), nil
}

func (h *tasksHandler) UpdateAdminTask(ctx context.Context, req *api.AdminTaskWrite, params api.UpdateAdminTaskParams) (api.UpdateAdminTaskRes, error) {
	const location = "httpv1.tasksHandler.UpdateAdminTask"
	input, err := request.Task(req, params.TaskID)
	if err != nil {
		return nil, fmt.Errorf("%s - validate task input: %w", location, err)
	}
	task, err := h.game.SaveTask(ctx, input, false)
	if err != nil {
		return nil, fmt.Errorf("%s - update task: %w", location, err)
	}
	return response.AdminTask(task), nil
}

func (h *tasksHandler) DeleteAdminTask(ctx context.Context, params api.DeleteAdminTaskParams) (api.DeleteAdminTaskRes, error) {
	const location = "httpv1.tasksHandler.DeleteAdminTask"
	if err := h.game.DeleteTask(ctx, params.TaskID); err != nil {
		return nil, fmt.Errorf("%s - deactivate task: %w", location, err)
	}
	return new(api.DeleteAdminTaskOK("ok")), nil
}

func (h *tasksHandler) GetClientTasks(ctx context.Context) ([]api.ParticipantTask, error) {
	const location = "httpv1.tasksHandler.GetClientTasks"
	tasks, err := h.game.Tasks(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("%s - list public tasks: %w", location, err)
	}
	return response.PublicTasks(tasks), nil
}

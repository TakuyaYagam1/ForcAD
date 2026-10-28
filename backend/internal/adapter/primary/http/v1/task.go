package v1

import (
	"context"
	"fmt"
	"math"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
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
	result := make(api.GetAdminTasksOKApplicationJSON, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, *adminTask(task))
	}
	return &result, nil
}

func (h *tasksHandler) GetAdminTask(ctx context.Context, params api.GetAdminTaskParams) (api.GetAdminTaskRes, error) {
	const location = "httpv1.tasksHandler.GetAdminTask"
	tasks, err := h.game.Tasks(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("%s - list tasks: %w", location, err)
	}
	for _, task := range tasks {
		if task.ID == params.TaskID {
			return adminTask(task), nil
		}
	}
	return nil, fmt.Errorf("%s - find task: %w", location, domain.ErrNotFound)
}

func validateTaskInput(req *api.AdminTaskWrite) error {
	const location = "httpv1.validateTaskInput"
	if req.DefaultScore < 0 || req.DefaultScore > math.MaxInt32 {
		return fmt.Errorf("%s - invalid default score: %w", location, &domain.ValidationError{Message: "Invalid default score"})
	}
	return nil
}

func (h *tasksHandler) CreateAdminTask(ctx context.Context, req *api.AdminTaskWrite) (api.CreateAdminTaskRes, error) {
	const location = "httpv1.tasksHandler.CreateAdminTask"
	if err := validateTaskInput(req); err != nil {
		return nil, fmt.Errorf("%s - validate task input: %w", location, err)
	}
	task, err := h.game.SaveTask(ctx, taskInput(req, uuid.Nil()), true)
	if err != nil {
		return nil, fmt.Errorf("%s - create task: %w", location, err)
	}
	return adminTask(task), nil
}

func (h *tasksHandler) UpdateAdminTask(ctx context.Context, req *api.AdminTaskWrite, params api.UpdateAdminTaskParams) (api.UpdateAdminTaskRes, error) {
	const location = "httpv1.tasksHandler.UpdateAdminTask"
	if err := validateTaskInput(req); err != nil {
		return nil, fmt.Errorf("%s - validate task input: %w", location, err)
	}
	task, err := h.game.SaveTask(ctx, taskInput(req, params.TaskID), false)
	if err != nil {
		return nil, fmt.Errorf("%s - update task: %w", location, err)
	}
	return adminTask(task), nil
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
	result := make([]api.ParticipantTask, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, api.ParticipantTask{ID: api.UUID(task.ID), Name: task.Name})
	}
	return result, nil
}

package v1

import (
	"context"
	"fmt"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
)

type flagsHandler struct {
	flags FlagService
}

func (h *flagsHandler) HandleTeamToken(ctx context.Context, _ api.OperationName, token api.TeamToken) (context.Context, error) {
	return context.WithValue(ctx, teamTokenKey, token.APIKey), nil
}

func (h *flagsHandler) SubmitFlags(ctx context.Context, req []string) (api.SubmitFlagsRes, error) {
	const location = "httpv1.flagsHandler.SubmitFlags"
	token, _ := ctx.Value(teamTokenKey).(string)
	rows, err := h.flags.Submit(ctx, token, req)
	if err != nil {
		return nil, fmt.Errorf("%s - submit flags: %w", location, err)
	}
	result := make(api.SubmitFlagsOKApplicationJSON, 0, len(rows))
	for _, row := range rows {
		result = append(result, api.FlagSubmissionResult{Msg: row.Message, Flag: row.Flag})
	}
	return &result, nil
}

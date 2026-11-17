package response

import (
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/usecase"
)

func FlagSubmissions(rows []usecase.FlagResponse) *api.SubmitFlagsOKApplicationJSON {
	result := make(api.SubmitFlagsOKApplicationJSON, 0, len(rows))
	for _, row := range rows {
		result = append(result, api.FlagSubmissionResult{
			Msg:  row.Message,
			Flag: row.Flag,
		})
	}
	return &result
}

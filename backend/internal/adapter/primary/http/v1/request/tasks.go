package request

import (
	"fmt"
	"math"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func Task(t *api.AdminTaskWrite, id uuid.UUID) (domain.Task, error) {
	const location = "httpv1.request.Task"
	if t.DefaultScore < 0 || t.DefaultScore > math.MaxInt32 {
		return domain.Task{}, fmt.Errorf("%s - invalid default score: %w", location, &domain.ValidationError{
			Message: "Invalid default score",
		})
	}
	defaultScore := int32(t.DefaultScore)
	return domain.Task{
		ID:             id,
		Name:           t.Name,
		Checker:        t.Checker,
		Gets:           t.Gets,
		Puts:           t.Puts,
		Places:         t.Places,
		CheckerTimeout: t.CheckerTimeout,
		CheckerType:    t.CheckerType,
		EnvPath:        t.EnvPath.Or(""),
		DefaultScore:   defaultScore,
		GetPeriod:      t.GetPeriod,
		Active:         t.Active.Or(true),
	}, nil
}

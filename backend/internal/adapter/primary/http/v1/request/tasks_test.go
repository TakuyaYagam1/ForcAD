package request

import (
	"errors"
	"math"
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func TestTaskRejectsScoreOutsideDomainRange(t *testing.T) {
	for _, score := range []int{-1, math.MaxInt32 + 1} {
		_, err := Task(&api.AdminTaskWrite{
			DefaultScore: score,
		}, uuid.Nil())
		var validation *domain.ValidationError
		if !errors.As(err, &validation) || validation.Message != "Invalid default score" {
			t.Fatalf("score %v: expected validation error, got %v", score, err)
		}
	}
}

func TestTaskDefaultsAndExplicitInactive(t *testing.T) {
	id := uuid.MustParse("01912345-6789-7000-8000-000000000001")
	req := &api.AdminTaskWrite{
		DefaultScore: math.MaxInt32,
	}
	task, err := Task(req, id)
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != id || !task.Active || task.EnvPath != "" || task.DefaultScore != math.MaxInt32 {
		t.Fatalf("unexpected task defaults: %+v", task)
	}
	req.Active = api.NewOptBool(false)
	task, err = Task(req, id)
	if err != nil || task.Active {
		t.Fatalf("explicit active=false was lost: %+v, %v", task, err)
	}
}

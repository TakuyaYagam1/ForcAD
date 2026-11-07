package response

import (
	"math"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/internal/usecase"
)

func TestCtftimeStandingsRejectsOutOfRangePosition(t *testing.T) {
	for _, pos := range []int{0, -1, math.MaxInt32 + 1} {
		result, err := CtftimeStandings([]usecase.Standing{{
			Pos: pos,
		}})
		if err == nil || result != nil {
			t.Fatalf("position %d was not rejected: %+v, %v", pos, result, err)
		}
	}
	result, err := CtftimeStandings([]usecase.Standing{{
		Pos:   math.MaxInt32,
		Team:  "last",
		Score: 1.5,
	}})
	if err != nil || result == nil || len(*result) != 1 || (*result)[0].Pos != math.MaxInt32 {
		t.Fatalf("last supported position was lost: %+v, %v", result, err)
	}
}

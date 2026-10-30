package checker

import (
	"strings"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

func validStatus(status processchecker.Status) bool {
	switch status {
	case processchecker.StatusUp, processchecker.StatusCorrupt, processchecker.StatusMumble,
		processchecker.StatusDown, processchecker.StatusCheckFailed:
		return true
	default:
		return false
	}
}

func statusCode(status processchecker.Status) int32 {
	switch status {
	case processchecker.StatusUp:
		return int32(processchecker.StatusUp)
	case processchecker.StatusCorrupt:
		return int32(processchecker.StatusCorrupt)
	case processchecker.StatusMumble:
		return int32(processchecker.StatusMumble)
	case processchecker.StatusDown:
		return int32(processchecker.StatusDown)
	case processchecker.StatusCheckFailed:
		return int32(processchecker.StatusCheckFailed)
	default:
		return checkFailedStatus
	}
}

func actionName(action processchecker.Action) string { return strings.ToUpper(string(action)) }

func failedResult(round int32, private string) domain.CheckerResult {
	return domain.CheckerResult{Round: round, Status: int32(processchecker.StatusCheckFailed), PublicMessage: "Checker failed", PrivateMessage: private}
}

type verdict struct {
	action  processchecker.Action
	status  processchecker.Status
	public  string
	private string
	command string
}

func selectVerdict(verdicts []verdict) *verdict {
	if len(verdicts) == 0 {
		return nil
	}
	for index := range verdicts {
		if verdicts[index].status != processchecker.StatusUp {
			return &verdicts[index]
		}
	}
	return &verdicts[0]
}

func orderBranches(branches []verdict) []verdict {
	puts := make([]verdict, 0, len(branches))
	gets := make([]verdict, 0, len(branches))
	for _, item := range branches {
		switch item.action {
		case processchecker.ActionPut:
			puts = append(puts, item)
		case processchecker.ActionGet:
			gets = append(gets, item)
		case processchecker.ActionCheck:
			// CHECK verdicts are handled before branch ordering.
		}
	}
	return append(puts, gets...)
}

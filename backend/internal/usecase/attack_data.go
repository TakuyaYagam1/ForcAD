package usecase

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"golang.org/x/sync/singleflight"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

const attackDataLoadTimeout = 5 * time.Second

type attackDataSnapshot map[uuid.UUID]map[string][]string

type groupedAttackData struct {
	groups attackDataSnapshot
	rows   []domain.AttackDatum
}

func (g *Game) AttackData(ctx context.Context, service *string) (map[string]map[string][]string, error) {
	const location = "usecase.Game.AttackData"
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s - read public attack data: %w", location, err)
	}
	if err := g.runtimeCtx.Err(); err != nil {
		return nil, fmt.Errorf("%s - read public attack data: %w", location, err)
	}
	life, err := g.Lifecycle(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s - read game lifecycle: %w", location, err)
	}
	tasks, err := g.repo.Tasks(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("%s - list active tasks: %w", location, err)
	}
	ids := make([]uuid.UUID, 0, len(tasks))
	names := make(map[uuid.UUID]string, len(tasks))
	for _, task := range tasks {
		if !hasTag(task.CheckerType, "pfr") || service != nil && task.Name != *service {
			continue
		}
		ids = append(ids, task.ID)
		names[task.ID] = task.Name
	}
	if len(ids) == 0 {
		return map[string]map[string][]string{}, nil
	}
	groups, err := g.attackDataGroups(ctx, life, ids)
	if err != nil {
		return nil, fmt.Errorf("%s - load public attack data: %w", location, err)
	}
	// The flight snapshot has no task names and is read-only. Each caller uses
	// its own metadata and owns every returned map and string slice.
	return cloneAttackDataGroups(groups, names), nil
}

func (g *Game) attackDataGroups(ctx context.Context, life domain.Lifecycle, ids []uuid.UUID) (*groupedAttackData, error) {
	const location = "usecase.Game.attackDataGroups"
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s - wait for public attack data: %w", location, err)
	}
	taskIDs := slices.Clone(ids)
	slices.SortFunc(taskIDs, func(a, b uuid.UUID) int {
		return a.Compare(b)
	})
	key := attackDataFlightKey(life, taskIDs)
	//nolint:contextcheck // The shared query is owned by Game.Close and its own timeout, not the first caller.
	resultCh := g.attackFlights.DoChan(key, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(g.runtimeCtx, attackDataLoadTimeout)
		defer cancel()
		if err := loadCtx.Err(); err != nil {
			return nil, fmt.Errorf("%s - start shared public attack data query: %w", location, err)
		}
		rows, err := g.flags.AttackData(loadCtx, life.Config.RealRound, taskIDs)
		if err != nil {
			return nil, fmt.Errorf("%s - query shared public attack data: %w", location, err)
		}
		groups, err := groupAttackData(loadCtx, rows)
		if err != nil {
			return nil, fmt.Errorf("%s - group shared public attack data: %w", location, err)
		}
		return &groupedAttackData{
			groups: groups,
			rows:   rows,
		}, nil
	})
	var result singleflight.Result
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%s - wait for public attack data: %w", location, ctx.Err())
	case result = <-resultCh:
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s - read shared public attack data: %w", location, err)
	}
	if result.Err != nil {
		return nil, fmt.Errorf("%s - read shared public attack data: %w", location, result.Err)
	}
	groups, ok := result.Val.(*groupedAttackData)
	if !ok {
		return nil, fmt.Errorf("%s - invalid shared public attack data result type %T", location, result.Val)
	}
	return groups, nil
}

func groupAttackData(ctx context.Context, rows []domain.AttackDatum) (attackDataSnapshot, error) {
	const location = "usecase.groupAttackData"
	groups := make(attackDataSnapshot)
	for i, row := range rows {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("%s - group public attack data: %w", location, err)
			}
		}
		teams := groups[row.TaskID]
		if teams == nil {
			teams = make(map[string][]string)
			groups[row.TaskID] = teams
		}
		teams[row.TeamIP] = append(teams[row.TeamIP], row.PublicData)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s - finish grouping public attack data: %w", location, err)
	}
	return groups, nil
}

func cloneAttackDataGroups(snapshot *groupedAttackData, names map[uuid.UUID]string) map[string]map[string][]string {
	clone := make(map[string]map[string][]string, len(names))
	aliases := false
	for id, name := range names {
		if _, exists := clone[name]; exists {
			aliases = true
		}
		clone[name] = make(map[string][]string, len(snapshot.groups[id]))
	}
	if aliases {
		// Task names are not unique. Preserve the repository's exact row order
		// when multiple tasks contribute to the same public service name.
		for _, row := range snapshot.rows {
			teams := clone[names[row.TaskID]]
			teams[row.TeamIP] = append(teams[row.TeamIP], row.PublicData)
		}
		return clone
	}
	for id, teams := range snapshot.groups {
		for team, hints := range teams {
			clone[names[id]][team] = slices.Clone(hints)
		}
	}
	return clone
}

func attackDataFlightKey(life domain.Lifecycle, sortedIDs []uuid.UUID) string {
	var key strings.Builder
	key.WriteString(strconv.FormatInt(life.Session.Generation, 10))
	key.WriteByte(':')
	key.WriteString(strconv.FormatInt(int64(life.Config.RealRound), 10))
	key.WriteByte(':')
	key.WriteString(strconv.FormatInt(int64(life.Config.FlagLifetime), 10))
	for _, id := range sortedIDs {
		key.WriteByte(':')
		key.WriteString(id.String())
	}
	return key.String()
}

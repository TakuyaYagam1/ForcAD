package usecase

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

// BenchmarkAttackDataGrouping measures only representation construction: one
// cohort shares one SQL result. SQL, metadata, and goroutine scheduling are
// deliberately excluded; cohort=1 includes the cost without any sharing benefit.
func BenchmarkAttackDataGrouping(b *testing.B) {
	rows, names := benchmarkAttackDataRows()
	for _, callers := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("cohort-%d/old-group-per-caller", callers), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for range callers {
					runtime.KeepAlive(benchmarkOldAttackDataGrouping(rows, names))
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*callers), "ns/caller")
		})
		b.Run(fmt.Sprintf("cohort-%d/shared-group-and-clone", callers), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				groups, err := groupAttackData(b.Context(), rows)
				if err != nil {
					b.Fatal(err)
				}
				snapshot := &groupedAttackData{
					groups: groups,
					rows:   rows,
				}
				for range callers {
					runtime.KeepAlive(cloneAttackDataGroups(snapshot, names))
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*callers), "ns/caller")
		})
	}
	groups, err := groupAttackData(context.Background(), rows)
	if err != nil {
		b.Fatal(err)
	}
	snapshot := &groupedAttackData{
		groups: groups,
		rows:   rows,
	}
	b.Run("clone-only", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			runtime.KeepAlive(cloneAttackDataGroups(snapshot, names))
		}
	})
}

func benchmarkAttackDataRows() ([]domain.AttackDatum, map[uuid.UUID]string) {
	rows := make([]domain.AttackDatum, 0, 6720)
	names := make(map[uuid.UUID]string, 3)
	for task, puts := range []int{14, 13, 13} {
		id := uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", task+1))
		names[id] = fmt.Sprintf("service-%d", task+1)
		for team := 1; team <= 28; team++ {
			for hint := range puts * 6 {
				rows = append(rows, domain.AttackDatum{
					TaskID:     id,
					TeamIP:     fmt.Sprintf("192.0.2.%d", team),
					PublicData: fmt.Sprintf("records/%032x", hint),
				})
			}
		}
	}
	return rows, names
}

func benchmarkOldAttackDataGrouping(rows []domain.AttackDatum, names map[uuid.UUID]string) map[string]map[string][]string {
	data := make(map[string]map[string][]string)
	for _, name := range names {
		data[name] = make(map[string][]string)
	}
	for _, row := range rows {
		teams := data[names[row.TaskID]]
		teams[row.TeamIP] = append(teams[row.TeamIP], row.PublicData)
	}
	return data
}

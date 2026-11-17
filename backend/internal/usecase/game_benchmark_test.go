package usecase

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/domain"
	"github.com/TakuyaYagam1/ForcAD/internal/port/outbound"
)

type runtimeBenchmarkRepository struct {
	outbound.GameRepository

	calls atomic.Int64
}

func (r *runtimeBenchmarkRepository) Lifecycle(ctx context.Context) (domain.Lifecycle, error) {
	r.calls.Add(1)
	timer := time.NewTimer(100 * time.Microsecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return domain.Lifecycle{}, ctx.Err()
	case <-timer.C:
		return domain.Lifecycle{}, nil
	}
}

func BenchmarkRuntimeConcurrent(b *testing.B) {
	repo := &runtimeBenchmarkRepository{}
	service := NewGame(repo, nil, nil, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	b.SetParallelism(4)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := service.Runtime(ctx); err != nil {
				b.Error(err)
				return
			}
		}
	})
	b.StopTimer()
	b.ReportMetric(float64(repo.calls.Load())/float64(b.N), "repo_calls/op")
}

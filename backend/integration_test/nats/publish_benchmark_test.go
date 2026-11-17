package nats_test

import (
	"context"
	"fmt"
	"testing"
	"time"
	"uuid"
)

func BenchmarkPublishBatch(b *testing.B) {
	for _, size := range []int{1, 8, 84, 256} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			f := newFixture(b, 1)
			ids := make([]uuid.UUID, size)
			for i := range ids {
				ids[i] = job(i + 1)
			}
			stream, err := f.js.Stream(b.Context(), f.config.Stream)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				confirmed, err := f.broker.Publish(b.Context(), ids)
				if err != nil || len(confirmed) != size {
					b.Fatalf("confirmed=%d err=%v", len(confirmed), err)
				}
				b.StopTimer()
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err = stream.Purge(cleanupCtx)
				cancel()
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}

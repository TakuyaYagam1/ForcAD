package load_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// Keep the original selector as a reference, including ordering, wrap arithmetic
// and formatting of invalid positions. It intentionally has the old allocations.
func referenceCatalogBatch(flags []storedFlag, team int, turn uint64) flagRequest {
	request := flagRequest{
		team:   team,
		values: make([]string, batchSize),
		flags:  make([]storedFlag, batchSize),
	}
	var foreign, own []storedFlag
	for _, flag := range flags {
		if flag.Owner == team {
			own = append(own, flag)
		} else {
			foreign = append(foreign, flag)
		}
	}
	for i := range batchSize {
		switch {
		case i < 70 && len(foreign) > 0:
			request.flags[i] = foreign[(int(turn)*70+i)%len(foreign)]
		case i >= 70 && i < 80 && len(own) > 0:
			request.flags[i] = own[(i-70)%len(own)]
		}
		if request.flags[i].ID != "" {
			request.values[i] = request.flags[i].Value
		} else {
			request.values[i] = "Z" + fmt.Sprintf("%030d=", i)
		}
	}
	return request
}

func catalogFixture(count, owners int, prefix string) []storedFlag {
	flags := make([]storedFlag, count)
	for i := range flags {
		flags[i] = storedFlag{
			ID:    fmt.Sprintf("%s-id-%d", prefix, i),
			Value: fmt.Sprintf("%s-value-%d", prefix, i),
			Owner: i%owners + 1,
		}
	}
	return flags
}

func TestCatalogIndexedBatchesMatchReferenceAcrossRefresh(t *testing.T) {
	c := &catalog{}
	f := newFarm("http://127.0.0.1", c)
	for _, flags := range [][]storedFlag{
		nil, catalogFixture(1, 1, "single"), catalogFixture(27, 3, "mixed"),
		catalogFixture(3360, 28, "full"), {{
			Owner: 99,
			Value: "missing-id",
		}},
	} {
		c.replace(flags)
		for _, team := range []int{0, 1, 2, 7, 28, 99} {
			for _, turn := range []uint64{0, 1, 2, 33, 999, 1 << 32} {
				got, want := f.batch(team, turn), referenceCatalogBatch(flags, team, turn)
				if got.team != want.team || !slices.Equal(got.flags, want.flags) || !slices.Equal(got.values, want.values) {
					t.Fatalf("catalog=%d team=%d turn=%d changed reference selection", len(flags), team, turn)
				}
			}
		}
	}
	literal := &catalog{
		flags: catalogFixture(6, 2, "literal"),
	}
	got := newFarm("http://127.0.0.1", literal).batch(1, 2)
	if want := referenceCatalogBatch(literal.flags, 1, 2); !slices.Equal(got.flags, want.flags) {
		t.Fatal("literal catalog lazy initialization changed selection")
	}
}

func TestCatalogRefreshKeepsPendingRequestsIndependent(t *testing.T) {
	c := &catalog{}
	c.replace(catalogFixture(120, 3, "old"))
	f := newFarm("http://127.0.0.1", c)
	var target vegeta.Target
	if err := f.target(&target); err != nil {
		t.Fatal(err)
	}
	request := f.pending[target.URL]
	oldSnapshot := c.snapshot()
	c.replace(catalogFixture(120, 3, "new"))
	_ = f.batch(1, 20)
	if !slices.Equal(request.flags, referenceCatalogBatch(oldSnapshot.flags, 1, 0).flags) {
		t.Fatal("refresh or another batch mutated the pending request")
	}
	rows := make([]map[string]string, batchSize)
	seen := make(map[string]bool)
	for i, flag := range request.flags {
		message := "Flag is invalid"
		if flag.ID != "" {
			switch {
			case flag.Owner == request.team:
				message = "Flag is your own"
			case seen[flag.ID]:
				message = "Flag already stolen"
			default:
				message = "Flag accepted!"
				seen[flag.ID] = true
			}
		}
		rows[i] = map[string]string{
			"flag": request.values[i],
			"msg":  message,
		}
	}
	body, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.validate(&vegeta.Result{
		URL:  target.URL,
		Code: http.StatusOK,
		Body: body,
	}); err != nil {
		t.Fatalf("refresh changed pending verdict validation: %v", err)
	}
	if len(f.pending) != 0 || f.accepted != int64(len(seen)) || len(f.expected[request.team]) != len(seen) {
		t.Fatal("pending removal, accepted count or expected IDs changed")
	}
}

func TestCatalogIndexPublicationIsAtomicDuringConcurrentRefresh(t *testing.T) {
	a, b := catalogFixture(120, 3, "a"), catalogFixture(64, 2, "b")
	c := &catalog{}
	c.replace(a)
	f := newFarm("http://127.0.0.1", c)
	var wait sync.WaitGroup
	errors := make(chan string, 8)
	wait.Go(func() {
		for i := range 100 {
			if i%2 == 0 {
				c.replace(a)
			} else {
				c.replace(b)
			}
		}
	})
	for range 8 {
		wait.Go(func() {
			for turn := range uint64(64) {
				got := f.batch(1, turn)
				first, second := referenceCatalogBatch(a, 1, turn), referenceCatalogBatch(b, 1, turn)
				if !slices.Equal(got.flags, first.flags) && !slices.Equal(got.flags, second.flags) {
					errors <- "batch mixed catalog generations"
					return
				}
			}
		})
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

var catalogBatchSink flagRequest

func TestCatalogIndexReducesSteadyBatchAllocations(t *testing.T) {
	flags := catalogFixture(3360, 28, "allocation")
	c := &catalog{}
	c.replace(flags)
	f := newFarm("http://127.0.0.1", c)
	indexed := testing.AllocsPerRun(10, func() {
		catalogBatchSink = f.batch(1, 10)
	})
	reference := testing.AllocsPerRun(10, func() {
		catalogBatchSink = referenceCatalogBatch(flags, 1, 10)
	})
	if indexed >= reference {
		t.Fatalf("indexed allocations=%g, reference=%g", indexed, reference)
	}
	t.Logf("allocations/batch: indexed=%g reference=%g", indexed, reference)
}

func BenchmarkCatalogBatch(b *testing.B) {
	flags := catalogFixture(3360, 28, "benchmark")
	c := &catalog{}
	c.replace(flags)
	f := newFarm("http://127.0.0.1", c)
	b.Run("reference", func(b *testing.B) {
		b.ReportAllocs()
		for i := range b.N {
			catalogBatchSink = referenceCatalogBatch(flags, 1, uint64(i))
		}
	})
	b.Run("indexed", func(b *testing.B) {
		b.ReportAllocs()
		for i := range b.N {
			catalogBatchSink = f.batch(1, uint64(i))
		}
	})
}

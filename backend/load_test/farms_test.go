package load_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

type storedFlag struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Owner int    `json:"owner"`
	NPC   bool   `json:"npc"`
}

type catalog struct {
	mu    sync.RWMutex
	flags []storedFlag
	index *catalogIndex
}

type catalogIndex struct {
	flags   []storedFlag
	all     []int
	own     map[int][]int
	foreign map[int][]int
}

var invalidFlagValues = func() [batchSize]string {
	var values [batchSize]string
	for i := range batchSize {
		values[i] = "Z" + fmt.Sprintf("%030d=", i)
	}
	return values
}()

func indexCatalog(flags []storedFlag) *catalogIndex {
	index := &catalogIndex{
		flags:   flags,
		all:     make([]int, len(flags)),
		own:     make(map[int][]int),
		foreign: make(map[int][]int),
	}
	for i, flag := range flags {
		index.all[i] = i
		index.own[flag.Owner] = append(index.own[flag.Owner], i)
	}
	for owner, own := range index.own {
		foreign := make([]int, 0, len(flags)-len(own))
		for i, flag := range flags {
			if flag.Owner != owner {
				foreign = append(foreign, i)
			}
		}
		index.foreign[owner] = foreign
	}
	return index
}

func (c *catalog) replace(flags []storedFlag) {
	index := indexCatalog(flags)
	c.mu.Lock()
	c.flags, c.index = flags, index
	c.mu.Unlock()
}

func (c *catalog) snapshot() *catalogIndex {
	c.mu.RLock()
	index := c.index
	c.mu.RUnlock()
	if index != nil {
		return index
	}
	// Support literal catalogs in local fixtures; refreshed catalogs are indexed
	// before publication, so measured requests do not take this initialization path.
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.index == nil {
		c.index = indexCatalog(c.flags)
	}
	return c.index
}

func (c *catalog) refresh(ctx context.Context, d *deployment) error {
	data, err := d.query(ctx, `SELECT COALESCE(json_agg(f), '[]') FROM (
SELECT f.id, f.flag AS value, split_part(t.ip,'.',4)::integer AS owner, t.kind='npc' AS npc
FROM Flags f JOIN Teams t ON t.id=f.team_id
WHERE f.round >= (SELECT greatest(1,real_round-2) FROM GameConfig)
ORDER BY f.round DESC,f.id) f`)
	if err != nil {
		return err
	}
	var flags []storedFlag
	if err := json.Unmarshal(data, &flags); err != nil {
		return fmt.Errorf("load.catalog.refresh - decode flags: %w", err)
	}
	c.replace(flags)
	return nil
}

func (c *catalog) wait(ctx context.Context, d *deployment) error {
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if err := c.refresh(ctx, d); err != nil {
			return err
		}
		count, err := d.number(ctx, `SELECT count(*) FROM TeamTasks tt JOIN Teams t ON t.id=tt.team_id WHERE tt.status=101 AND t.kind<>'npc'`)
		if err != nil {
			return err
		}
		playerFlags := 0
		for _, flag := range c.snapshot().flags {
			if !flag.NPC {
				playerFlags++
			}
		}
		if count == int64(teamCount*3) && playerFlags >= teamCount*3 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			statuses, statusErr := d.query(ctx, `SELECT status, count(*) FROM TeamTasks GROUP BY status ORDER BY status`)
			actions, actionErr := d.query(ctx, `SELECT action, status, timed_out, count(*) FROM CheckerActionLogs GROUP BY action, status, timed_out ORDER BY action, status, timed_out`)
			failure := fmt.Errorf("load.catalog.wait - all %d checker workflows must reach UP: cells=%q actions=%q", teamCount*3, strings.TrimSpace(string(statuses)), strings.TrimSpace(string(actions)))
			return errors.Join(failure, statusErr, actionErr)
		case <-tick.C:
		}
	}
}

type flagRequest struct {
	team   int
	flags  []storedFlag
	values []string
}

type farm struct {
	origin         string
	catalog        *catalog
	mu             sync.Mutex
	next           uint64
	pending        map[string]flagRequest
	expected       map[int]map[string]bool
	accepted       int64
	committed      int64
	allowRateLimit bool
}

func newFarm(origin string, flags *catalog) *farm {
	return &farm{
		origin:   origin,
		catalog:  flags,
		pending:  make(map[string]flagRequest),
		expected: make(map[int]map[string]bool),
	}
}

func (f *farm) target(target *vegeta.Target) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.next
	f.next++
	team := int(n%uint64(teamCount)) + 1
	request := f.batch(team, n/uint64(teamCount))
	body, err := json.Marshal(request.values)
	if err != nil {
		return fmt.Errorf("load.farm.target - encode flags: %w", err)
	}
	address := f.origin + "/flags?load_request=" + strconv.FormatUint(n, 10)
	f.pending[address] = request
	*target = vegeta.Target{
		Method: http.MethodPut,
		URL:    address,
		Body:   body,
		Header: http.Header{
			"Content-Type": {"application/json"},
			"X-Team-Token": {fmt.Sprintf("%016x", team)},
		},
	}
	return nil
}

func (f *farm) batch(team int, turn uint64) flagRequest {
	index := f.catalog.snapshot()
	request := flagRequest{
		team:   team,
		values: make([]string, batchSize),
		flags:  make([]storedFlag, batchSize),
	}
	foreign, own := index.foreign[team], index.own[team]
	if own == nil {
		foreign = index.all
	}
	for i := range batchSize {
		switch {
		case i < 70 && len(foreign) > 0:
			request.flags[i] = index.flags[foreign[(int(turn)*70+i)%len(foreign)]]
		case i >= 70 && i < 80 && len(own) > 0:
			request.flags[i] = index.flags[own[(i-70)%len(own)]]
		}
		if request.flags[i].ID != "" {
			request.values[i] = request.flags[i].Value
		} else {
			request.values[i] = invalidFlagValues[i]
		}
	}
	return request
}

func (f *farm) validate(result *vegeta.Result) error {
	const location = "load.farm.validate"
	f.mu.Lock()
	defer f.mu.Unlock()
	request, ok := f.pending[result.URL]
	delete(f.pending, result.URL)
	if !ok {
		return fmt.Errorf("%s - missing request correlation", location)
	}
	if result.Code == http.StatusTooManyRequests && f.allowRateLimit {
		if result.Headers.Get("Retry-After") == "" {
			return fmt.Errorf("%s - rate limit missing Retry-After", location)
		}
		return nil
	}
	if result.Code != http.StatusOK {
		return fmt.Errorf("%s - team %d returned HTTP %d: %s", location, request.team, result.Code, result.Error)
	}
	var rows []struct {
		Flag    string `json:"flag"`
		Message string `json:"msg"`
	}
	if err := json.Unmarshal(result.Body, &rows); err != nil {
		return fmt.Errorf("%s - decode response: %w", location, err)
	}
	if len(rows) != batchSize {
		return fmt.Errorf("%s - incomplete batch", location)
	}
	for i, row := range rows {
		if row.Flag != request.values[i] {
			return fmt.Errorf("%s - result order changed", location)
		}
		flag := request.flags[i]
		switch {
		case flag.ID == "":
			if !strings.Contains(row.Message, "Flag is invalid") {
				return fmt.Errorf("%s - invalid value accepted", location)
			}
		case flag.Owner == request.team:
			if !strings.Contains(row.Message, "Flag is your own") {
				return fmt.Errorf("%s - own flag verdict changed", location)
			}
		default:
			if err := f.recordFlag(request.team, flag, row.Message); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *farm) record(team int, id, message string) error {
	return f.recordFlag(team, storedFlag{
		ID: id,
	}, message)
}

func (f *farm) recordFlag(team int, flag storedFlag, message string) error {
	accepted := strings.Contains(message, "Flag accepted!")
	if flag.NPC {
		accepted = strings.Contains(message, "Flag accepted (NPC test). No points awarded.")
	}
	if !accepted && !strings.Contains(message, "Flag already stolen") {
		_, verdict, found := strings.Cut(message, "] ")
		if !found {
			verdict = message
		}
		return fmt.Errorf("load.farm.record - live valid flag rejected: %s", verdict)
	}
	if f.expected[team] == nil {
		f.expected[team] = make(map[string]bool)
	}
	f.expected[team][flag.ID] = true
	if accepted {
		f.accepted++
	}
	return nil
}

func (f *farm) reconcile(ctx context.Context, d *deployment, before int64) error {
	const location = "load.farm.reconcile"
	after, err := d.number(ctx, `SELECT count(*) FROM StolenFlags`)
	if err != nil {
		return err
	}
	f.committed = after - before
	var discrepancies []error
	if f.committed != f.accepted {
		discrepancies = append(discrepancies, fmt.Errorf("%s - accepted responses=%d, committed=%d", location, f.accepted, f.committed))
	}
	for team, ids := range f.expected {
		quoted := make([]string, 0, len(ids))
		for id := range ids {
			quoted = append(quoted, sqlString(id))
		}
		count, err := d.number(ctx, fmt.Sprintf(`SELECT count(*) FROM StolenFlags s JOIN Teams t ON t.id=s.attacker_id WHERE t.token='%016x' AND s.flag_id IN (%s)`, team, strings.Join(quoted, ",")))
		if err != nil {
			return errors.Join(append(discrepancies, err)...)
		}
		if count != int64(len(ids)) {
			discrepancies = append(discrepancies, fmt.Errorf("%s - team %d missing %d valid submissions", location, team, int64(len(ids))-count))
		}
	}
	bad, err := d.number(ctx, `SELECT count(*) FROM TeamTasks t WHERE t.stolen<>(SELECT count(*) FROM StolenFlags s JOIN Flags f ON f.id=s.flag_id JOIN Teams victim ON victim.id=f.team_id WHERE s.attacker_id=t.team_id AND f.task_id=t.task_id AND victim.kind<>'npc') OR t.lost<>(SELECT count(*) FROM StolenFlags s JOIN Flags f ON f.id=s.flag_id JOIN Teams victim ON victim.id=f.team_id WHERE f.team_id=t.team_id AND f.task_id=t.task_id AND victim.kind<>'npc') OR t.score<0 OR t.score='NaN'::float8`)
	if err != nil {
		return errors.Join(append(discrepancies, err)...)
	}
	if bad != 0 {
		discrepancies = append(discrepancies, fmt.Errorf("%s - inconsistent team scores or counters", location))
	}
	return errors.Join(discrepancies...)
}

func readTarget(origin, path string) vegeta.Targeter {
	var mu sync.Mutex
	index := 0
	return func(target *vegeta.Target) error {
		mu.Lock()
		defer mu.Unlock()
		selected := path
		if selected == "" {
			selected = readPaths[index%len(readPaths)]
		}
		*target = vegeta.Target{
			Method: http.MethodGet,
			URL:    origin + selected,
		}
		if clientBucketsEnabled() {
			clients, err := spectatorCount()
			if err != nil {
				return err
			}
			target.Header = http.Header{
				"X-Forcad-Load-Client": {fmt.Sprintf("client-%d", index%clients)},
			}
		}
		index++
		return nil
	}
}

func TestFarmRecordsNPCWithoutScore(t *testing.T) {
	f := newFarm("http://127.0.0.1", &catalog{})
	flag := storedFlag{
		ID:  "npc-flag",
		NPC: true,
	}
	if err := f.recordFlag(1, flag, "[npc] Flag accepted (NPC test). No points awarded."); err != nil {
		t.Fatal(err)
	}
	if err := f.recordFlag(1, flag, "[npc] Flag accepted! Earned 2.5 flag points!"); err == nil {
		t.Fatal("NPC flag accepted a scoring verdict")
	}
	if f.accepted != 1 || len(f.expected[1]) != 1 {
		t.Fatalf("NPC persistence accounting changed: accepted=%d expected=%d", f.accepted, len(f.expected[1]))
	}
}

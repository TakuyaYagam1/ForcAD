package v1

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/go-faster/jx"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/domain"
)

type scoreboardGame struct {
	GameService

	board domain.Scoreboard
	err   error
}

func (g *scoreboardGame) Scoreboard(context.Context) (domain.Scoreboard, error) {
	return g.board, g.err
}

func TestScoreboardResponsePreservesJSONAcrossPooledWrites(t *testing.T) {
	game := &scoreboardGame{}
	handler := NewHandler(game, nil, nil, nil)
	server, err := api.NewServer(handler, handler, handler)
	if err != nil {
		t.Fatal(err)
	}
	large := scoreboardFixture(20, 3)
	large.State.TeamTasks[0].Message = "quoted \"value\"\n<script> Привет"
	small := scoreboardFixture(0, 0)
	oversized := scoreboardFixture(1, 1)
	oversized.Teams[0].Name = strings.Repeat("large board ", 100000)
	// Alternate sizes and include an oversized response: pooled bytes and encoder
	// nesting state must never leak into the next response.
	for _, board := range []domain.Scoreboard{large, small, large, oversized, small, large} {
		game.board = board
		var expected jx.Encoder
		scoreboard(board).Encode(&expected)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/client/scoreboard/", nil))
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatalf("response status=%d, headers=%v", response.Code, response.Header())
		}
		if !bytes.Equal(response.Body.Bytes(), expected.Bytes()) {
			t.Fatalf("pooled response changed the generated JSON: got %d bytes, want %d", response.Body.Len(), len(expected.Bytes()))
		}
		if !jx.Valid(response.Body.Bytes()) {
			t.Fatal("scoreboard response is not valid JSON")
		}
	}
}

func TestScoreboardServiceFailureUsesErrorHandler(t *testing.T) {
	want := errors.New("scoreboard unavailable")
	game := &scoreboardGame{err: want}
	handler := NewHandler(game, nil, nil, nil)
	var handled error
	server, err := api.NewServer(handler, handler, handler, api.WithErrorHandler(func(_ context.Context, w http.ResponseWriter, _ *http.Request, err error) {
		handled = err
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"Service temporarily unavailable"}`))
	}))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/client/scoreboard/", nil))
	if !errors.Is(handled, want) || response.Code != http.StatusServiceUnavailable || response.Body.String() != `{"error":"Service temporarily unavailable"}` {
		t.Fatalf("error response changed: handled=%v, status=%d, body=%s", handled, response.Code, response.Body.String())
	}
}

type scoreboardWriter struct {
	header http.Header
	err    error
	status int
	writes int
}

func (w *scoreboardWriter) Header() http.Header    { return w.header }
func (w *scoreboardWriter) WriteHeader(status int) { w.status = status }
func (w *scoreboardWriter) Write(body []byte) (int, error) {
	w.writes++
	if w.err != nil {
		return 0, w.err
	}
	return len(body), nil
}

func TestScoreboardWriteFailureDoesNotPoisonNextResponse(t *testing.T) {
	want := errors.New("client disconnected")
	game := &scoreboardGame{board: scoreboardFixture(20, 3)}
	handler := &scoreboardHandler{game: game}
	failing := &scoreboardWriter{header: make(http.Header), err: want}
	for range 10 {
		if err := handler.GetScoreboard(t.Context(), failing); !errors.Is(err, want) {
			t.Fatalf("write failure was not propagated: %v", err)
		}
	}
	if failing.writes != 10 || failing.status != http.StatusOK {
		t.Fatalf("expected one buffered write per response, got %d writes and status %d", failing.writes, failing.status)
	}
	game.board = scoreboardFixture(0, 0)
	response := httptest.NewRecorder()
	if err := handler.GetScoreboard(t.Context(), response); err != nil {
		t.Fatal(err)
	}
	var expected jx.Encoder
	scoreboard(game.board).Encode(&expected)
	if !bytes.Equal(response.Body.Bytes(), expected.Bytes()) {
		t.Fatal("an unsuccessful pooled write contaminated a later response")
	}
}

func BenchmarkScoreboardResponse(b *testing.B) {
	game := &scoreboardGame{board: scoreboardFixture(20, 3)}
	handler := &scoreboardHandler{game: game}
	var payload jx.Encoder
	scoreboard(game.board).Encode(&payload)
	for _, test := range []struct {
		name  string
		write func(context.Context, http.ResponseWriter) error
	}{
		{"fresh_encoder", func(ctx context.Context, w http.ResponseWriter) error {
			board, err := game.Scoreboard(ctx)
			if err != nil {
				return err
			}
			e := new(jx.Encoder)
			scoreboard(board).Encode(e)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, err = e.WriteTo(w)
			return err
		}},
		{"pooled_encoder", handler.GetScoreboard},
	} {
		b.Run(test.name, func(b *testing.B) {
			writer := &scoreboardWriter{header: make(http.Header)}
			b.ReportAllocs()
			b.SetBytes(int64(len(payload.Bytes())))
			for b.Loop() {
				if err := test.write(b.Context(), writer); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func scoreboardFixture(teams, tasks int) domain.Scoreboard {
	started := time.Unix(1700000000, 0).UTC()
	board := domain.Scoreboard{
		Config: domain.GameConfig{ID: 1, FlagLifetime: 5, GameHardness: 10,
			RoundTime: 10, Mode: "classic", Timezone: "UTC", StartTime: started,
			RealRound: 7, GameRunning: true},
		Runtime: domain.RuntimeStatus{Phase: "running", Generation: 1,
			ScheduledStart: started, Round: 7, RoundStart: new(started.Unix()), RoundTime: 10},
		State: domain.GameState{Round: 7, RoundStart: started.Unix()},
	}
	for index := range tasks {
		id := uuid.UUID{15: byte(index + 1)}
		board.Tasks = append(board.Tasks, domain.PublicTask{ID: id, Name: fmt.Sprintf("service-%d", index+1)})
	}
	for index := range teams {
		id := uuid.UUID{14: 1, 15: byte(index + 1)}
		board.Teams = append(board.Teams, domain.PublicTeam{ID: id, Name: fmt.Sprintf("team-%d", index+1),
			IP: fmt.Sprintf("192.0.2.%d", index+1), Active: true})
		for _, task := range board.Tasks {
			board.State.TeamTasks = append(board.State.TeamTasks, domain.PublicCell{
				TeamID: id, TaskID: task.ID, Status: 101, Stolen: 2, Lost: 1,
				Score: 3000.125, Checks: 7, ChecksPassed: 7, Message: "UP",
			})
		}
	}
	return board
}

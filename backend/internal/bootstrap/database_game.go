package bootstrap

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
)

func changeGamePause(ctx context.Context, store *postgres.GameStore, action string, out io.Writer) error {
	const location = "bootstrap.changeGamePause"
	if err := store.ChangeState(ctx, action, nil, time.Now()); err != nil {
		return fmt.Errorf("%s - change game state: %w", location, err)
	}
	if _, err := fmt.Fprintf(out, "Game %sd\n", action); err != nil {
		return fmt.Errorf("%s - write game state result: %w", location, err)
	}
	return nil
}

func changeRoundLimit(ctx context.Context, store *postgres.GameStore, limit int, out io.Writer) error {
	const location = "bootstrap.changeRoundLimit"
	if limit < 0 || limit > math.MaxInt32 {
		return fmt.Errorf("%s - round limit outside PostgreSQL integer range", location)
	}
	var rounds *int32
	if limit != 0 {
		rounds = new(int32(limit))
	}
	if err := store.SetRoundLimit(ctx, rounds); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Round limit updated; game state preserved"); err != nil {
		return fmt.Errorf("%s - write round limit result: %w", location, err)
	}
	return nil
}

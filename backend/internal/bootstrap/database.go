package bootstrap

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/db"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	localoperator "github.com/TakuyaYagam1/ForcAD/internal/operator"
)

const databaseCommandTimeout = 2 * time.Minute

func RunDatabaseCommand(ctx context.Context, command string, args []string, out io.Writer) error {
	const location = "bootstrap.RunDatabaseCommand"
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - database command canceled: %w", location, err)
	}
	options, err := parseDatabaseOptions(command, args, out)
	if err != nil {
		return fmt.Errorf("%s - parse database command: %w", location, err)
	}
	if command == "validate-config" {
		return validateConfigFile(options.path, options.full, out)
	}
	cfg, err := commandConfig(options.path)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, databaseCommandTimeout)
	defer cancel()
	if command == "migrate" || command == "init" {
		if err := db.Migrate(bounded, cfg.DatabaseURL); err != nil {
			return err
		}
		if command == "migrate" {
			if _, err := fmt.Fprintln(out, "Database migrations applied"); err != nil {
				return fmt.Errorf("%s - write migration result: %w", location, err)
			}
			return nil
		}
	}
	pool, closePool, err := postgres.NewPool(bounded, cfg)
	if err != nil {
		return err
	}
	defer closePool()
	return options.run(bounded, postgres.NewGameStore(pool), out)
}

type databaseOptions struct {
	command    string
	path       string
	roundLimit int
	full       bool
}

func parseDatabaseOptions(command string, args []string, out io.Writer) (databaseOptions, error) {
	const location = "bootstrap.parseDatabaseOptions"
	options := flag.NewFlagSet(command, flag.ContinueOnError)
	options.SetOutput(out)
	defaultPath := ""
	if command == "init" || command == "validate-config" {
		defaultPath = os.Getenv("FORCAD_CONFIG")
		if defaultPath == "" {
			defaultPath = "/run/forcad-config/config.yml"
		}
	}
	path := options.String("config", defaultPath, "game configuration file")
	full := false
	if command == "validate-config" {
		options.BoolVar(&full, "full", false, "also validate admin credentials and storage configuration")
	}
	roundLimit := -1
	if command == "set-round-limit" {
		options.IntVar(&roundLimit, "rounds", -1, "maximum playable rounds; 0 removes the limit")
	}
	if err := options.Parse(args); err != nil {
		return databaseOptions{}, fmt.Errorf("%s - parse database command: %w", location, err)
	}
	if options.NArg() != 0 {
		return databaseOptions{}, fmt.Errorf("%s - unexpected command arguments", location)
	}
	if command == "set-round-limit" && (roundLimit < 0 || int64(roundLimit) > math.MaxInt32) {
		return databaseOptions{}, fmt.Errorf("%s - set-round-limit requires --rounds between 0 and 2147483647", location)
	}
	return databaseOptions{command: command, path: *path, roundLimit: roundLimit, full: full}, nil
}

func (options databaseOptions) run(ctx context.Context, store *postgres.GameStore, out io.Writer) error {
	const location = "bootstrap.databaseOptions.run"
	switch options.command {
	case "pause", "resume":
		return changeGamePause(ctx, store, options.command, out)
	case "set-round-limit":
		return changeRoundLimit(ctx, store, options.roundLimit, out)
	case "init":
		return initializeGame(ctx, store, options.path, out)
	case "status":
		life, err := store.Lifecycle(ctx)
		if err != nil {
			return err
		}
		if err := json.NewEncoder(out).Encode(life.Runtime(time.Now())); err != nil {
			return fmt.Errorf("%s - encode game status: %w", location, err)
		}
		return nil
	case "tokens":
		teams, err := store.Teams(ctx, true)
		if err != nil {
			return err
		}
		for _, team := range teams {
			if _, err := fmt.Fprintf(out, "%s\t%s\t%s\n", team.ID, team.Name, team.Token); err != nil {
				return fmt.Errorf("%s - write team tokens: %w", location, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("%s - unknown database command", location)
	}
}

func initializeGame(ctx context.Context, store *postgres.GameStore, path string, out io.Writer) error {
	const location = "bootstrap.initializeGame"
	document, err := config.LoadDocument(path)
	if err != nil {
		return err
	}
	created, err := store.Initialize(ctx, document)
	if err != nil {
		return err
	}
	if created {
		_, err = fmt.Fprintln(out, "Game initialized")
	} else {
		_, err = fmt.Fprintln(out, "Game is already initialized; existing data preserved")
	}
	if err != nil {
		return fmt.Errorf("%s - write initialization result: %w", location, err)
	}
	return nil
}

func validateConfigFile(path string, full bool, out io.Writer) error {
	const location = "bootstrap.validateConfigFile"
	if err := localoperator.ValidateConfig(path, full); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Configuration is valid"); err != nil {
		return fmt.Errorf("%s - write validation result: %w", location, err)
	}
	return nil
}

package bootstrap

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/postgres"
	redisadapter "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/redis"
	localoperator "github.com/TakuyaYagam1/ForcAD/internal/operator"
)

const maintenanceTimeout = 5 * time.Minute
const maintenanceStorageTimeout = 2 * time.Minute

type maintenanceResetter func(context.Context, config.Config) error

// RunMaintenanceCommand runs a host reset/clean or the in-container reset-data command.
func RunMaintenanceCommand(ctx context.Context, command string, args []string, out io.Writer) error {
	return runMaintenanceCommand(ctx, command, args, out, execMaintenanceCommand, os.Getwd, resetConfiguredStores)
}

func runMaintenanceCommand(
	ctx context.Context,
	command string,
	args []string,
	out io.Writer,
	runner localoperator.MaintenanceRunner,
	workingDirectory func() (string, error),
	resetter maintenanceResetter,
) error {
	const location = "bootstrap.runMaintenanceCommand"
	if ctx == nil {
		return fmt.Errorf("%s - maintenance command context must not be nil", location)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - maintenance command canceled: %w", location, err)
	}
	if out == nil {
		out = io.Discard
	}
	if command == "reset-data" {
		return runResetDataCommand(ctx, args, out, resetter)
	}
	if command != "reset" && command != "clean" {
		return fmt.Errorf("%s - unknown maintenance command %q", location, command)
	}
	if runner == nil {
		return fmt.Errorf("%s - Docker command runner is required", location)
	}
	if workingDirectory == nil {
		return fmt.Errorf("%s - working directory resolver is required", location)
	}

	projectRoot, options, err := parseMaintenanceOptions(command, args, out)
	if err != nil {
		return err
	}
	root, err := resolveProjectRoot(projectRoot, workingDirectory)
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("%s - could not resolve project root", location)
	}
	composeArgs, err := localoperator.ComposeArgs(root, false)
	if err != nil {
		return err
	}

	bounded, cancel := context.WithTimeout(ctx, maintenanceTimeout)
	defer cancel()
	resetData := func(resetCtx context.Context) error {
		args := append(append([]string(nil), composeArgs[1:]...), "run", "--rm", "--no-deps", "--entrypoint", "forcad", "initializer", "reset-data", "--confirm")
		if err := runner(resetCtx, composeArgs[0], args, io.Discard, io.Discard); err != nil {
			return fmt.Errorf("%s - run the bounded in-container game data reset", location)
		}
		return nil
	}
	return localoperator.RunMaintenance(bounded, root, options, out, runner, resetData)
}

func runResetDataCommand(ctx context.Context, args []string, out io.Writer, resetter maintenanceResetter) error {
	const location = "bootstrap.runResetDataCommand"
	if ctx == nil {
		return fmt.Errorf("%s - maintenance command context must not be nil", location)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - reset-data command canceled: %w", location, err)
	}
	if resetter == nil {
		return fmt.Errorf("%s - game data reset operation is required", location)
	}
	if out == nil {
		out = io.Discard
	}
	options := flag.NewFlagSet("reset-data", flag.ContinueOnError)
	options.SetOutput(out)
	confirm := options.Bool("confirm", false, "confirm the game data reset")
	if err := options.Parse(args); err != nil {
		return fmt.Errorf("%s - parse reset-data command: %w", location, err)
	}
	if options.NArg() != 0 {
		return fmt.Errorf("%s - unexpected reset-data arguments", location)
	}
	if !*confirm {
		return fmt.Errorf("%s - reset-data requires explicit --confirm", location)
	}
	cfg, err := commandConfig("")
	if err != nil {
		return fmt.Errorf("%s - load in-container storage environment: %w", location, err)
	}
	bounded, cancel := context.WithTimeout(ctx, maintenanceStorageTimeout)
	defer cancel()
	if err := resetter(bounded, cfg); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s - reset-data command canceled: %w", location, ctx.Err())
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%s - reset in-container game data: %w", location, err)
		}
		return fmt.Errorf("%s - reset in-container game data", location)
	}
	_, err = fmt.Fprintln(out, "Game state reset")
	if err != nil {
		return fmt.Errorf("%s - write reset result: %w", location, err)
	}
	return nil
}

func resetConfiguredStores(ctx context.Context, cfg config.Config) error {
	const location = "bootstrap.resetConfiguredStores"
	bounded, cancel := context.WithTimeout(ctx, maintenanceStorageTimeout)
	defer cancel()

	pool, closePool, err := postgres.NewPool(bounded, cfg)
	if err != nil {
		return fmt.Errorf("%s - connect to PostgreSQL for reset", location)
	}
	defer closePool()

	cache, closeCache, err := redisadapter.New(bounded, cfg)
	if err != nil {
		return fmt.Errorf("%s - connect to Redis for reset", location)
	}
	defer closeCache()

	if err := postgres.NewGameStore(pool).ResetGameData(bounded); err != nil {
		return fmt.Errorf("%s - reset PostgreSQL game data", location)
	}
	if err := cache.FlushDB(bounded); err != nil {
		return fmt.Errorf("%s - flush the configured Redis database", location)
	}
	return nil
}

func execMaintenanceCommand(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func parseMaintenanceOptions(command string, args []string, out io.Writer) (string, localoperator.MaintenanceOptions, error) {
	const location = "bootstrap.parseMaintenanceOptions"
	options := flag.NewFlagSet(command, flag.ContinueOnError)
	options.SetOutput(out)
	projectRoot := options.String("project-root", "", "ForcAD project root (defaults to the current directory)")
	confirm := options.Bool("confirm", false, "confirm destructive local maintenance")
	full := false
	if command == "reset" {
		options.BoolVar(&full, "full", false, "remove verified local PostgreSQL data")
	}
	if err := options.Parse(args); err != nil {
		return "", localoperator.MaintenanceOptions{}, fmt.Errorf("%s - parse maintenance command: %w", location, err)
	}
	if options.NArg() != 0 {
		return "", localoperator.MaintenanceOptions{}, fmt.Errorf("%s - unexpected maintenance arguments", location)
	}
	if !*confirm {
		return "", localoperator.MaintenanceOptions{}, fmt.Errorf("%s - reset and clean require explicit --confirm", location)
	}
	if command == "clean" && full {
		return "", localoperator.MaintenanceOptions{}, fmt.Errorf("%s - clean already performs a full reset", location)
	}
	return *projectRoot, localoperator.MaintenanceOptions{
		Confirm: *confirm,
		Full:    full,
		Clean:   command == "clean",
	}, nil
}

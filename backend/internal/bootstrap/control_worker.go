package bootstrap

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	localoperator "github.com/TakuyaYagam1/ForcAD/internal/operator"
)

func runWorkerCommand(ctx context.Context, root string, args []string, out io.Writer, runner operatorCommandRunner) error {
	const location = "bootstrap.runWorkerCommand"
	settings, workers, err := parseWorkerOptions(args, out)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - worker command canceled: %w", location, err)
	}
	// Separate worker hosts must reuse existing credentials, never generate new ones.
	if err := localoperator.ValidateConfig(filepath.Join(root, "config", "config.yml"), true); err != nil {
		return fmt.Errorf("%s - copy the initialized deployment configuration before adding workers: %w", location, err)
	}
	if err := localoperator.Setup(root, settings); err != nil {
		return err
	}
	compose := []string{"--project-root", root, "up", "-d", "--build", "--no-deps", "--scale", "worker=" + strconv.Itoa(workers), "pgbouncer", "worker"}
	return runOperatorCommand(ctx, "compose", compose, out, runner, nil)
}

func parseWorkerOptions(args []string, out io.Writer) (localoperator.SetupOptions, int, error) {
	const location = "bootstrap.parseWorkerOptions"
	options := flag.NewFlagSet("worker", flag.ContinueOnError)
	options.SetOutput(out)
	var settings localoperator.SetupOptions
	var workers int
	options.StringVar(&settings.Redis, "redis", "", "external Redis host or host:port (required)")
	options.StringVar(&settings.Database, "database", "", "external PostgreSQL host or host:port (required)")
	options.StringVar(&settings.RabbitMQ, "rabbitmq", "", "external RabbitMQ host or host:port (required)")
	options.IntVar(&workers, "workers", 1, "worker replica count")
	options.IntVar(&workers, "w", 1, "worker replica count")
	options.Bool("fast", false, "deprecated compatibility flag; has no effect")
	if err := options.Parse(args); err != nil {
		return settings, 0, fmt.Errorf("%s - parse worker options: %w", location, err)
	}
	if options.NArg() != 0 {
		return settings, 0, fmt.Errorf("%s - unexpected worker arguments", location)
	}
	if strings.TrimSpace(settings.Redis) == "" || strings.TrimSpace(settings.Database) == "" || strings.TrimSpace(settings.RabbitMQ) == "" {
		return settings, 0, fmt.Errorf("%s - --redis, --database and --rabbitmq are required", location)
	}
	if _, err := replicaCount(strconv.Itoa(workers)); err != nil {
		return settings, 0, err
	}
	return settings, workers, nil
}

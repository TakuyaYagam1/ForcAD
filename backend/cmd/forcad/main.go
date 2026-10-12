package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/bootstrap"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	const location = "main.run"
	if len(args) > 0 {
		return runCommand(args)
	}
	l, err := bootstrap.NewLogger(os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Logger initialization failed: %v\n", fmt.Errorf("%s - initialize logger: %w", location, err))
		return 1
	}
	defer func() {
		if err := l.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Logger shutdown failed: %v\n", err)
		}
	}()
	cfg, err := config.New()
	if err != nil {
		l.Error("Config initialization failed", "error", err)
		return 1
	}
	l.Info("Configuration loaded successfully")
	if err := bootstrap.RunApplication(cfg, l); err != nil {
		l.Error("Application exited with error", "error", err)
		return 1
	}
	return 0
}

func runCommand(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch args[0] {
	case "network-config":
		err = bootstrap.RunNetworkConfigCommand(ctx, args[1:], os.Stdout)
	case "network-rules":
		err = bootstrap.RunNetworkRulesCommand(ctx, args[1:], os.Stdout)
	case "network-metrics":
		err = bootstrap.RunNetworkMetricsCommand(ctx, args[1:], os.Stdout)
	case "control":
		err = bootstrap.RunControlCommand(ctx, args[1:], os.Stdout)
	case "reset", "clean", "reset-data":
		err = bootstrap.RunMaintenanceCommand(ctx, args[0], args[1:], os.Stdout)
	case "setup", "compose":
		err = bootstrap.RunOperatorCommand(ctx, args[0], args[1:], os.Stdout)
	case "health":
		err = bootstrap.RunHealthCommand(ctx, args[1:])
	case "drain":
		err = bootstrap.RunDrainCommand(ctx, args[1:], os.Stdout)
	case "runtime-capacity":
		err = bootstrap.RunRuntimeCapacityCommand(ctx, args[1:])
	case "init", "migrate", "validate-config", "status", "tokens", "set-round-limit", "pause", "resume":
		err = bootstrap.RunDatabaseCommand(ctx, args[0], args[1:], os.Stdout)
	default:
		fmt.Fprintln(os.Stderr, "Unknown command; use FORCAD_ROLE to select api, receiver, worker, ticker, or all")
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Command failed: %v\n", err)
		return 1
	}
	return 0
}

package bootstrap

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// RunControlCommand provides host shortcuts for the existing operator commands.
func RunControlCommand(ctx context.Context, args []string, out io.Writer) error {
	const location = "bootstrap.RunControlCommand"
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - control command canceled: %w", location, err)
	}
	root, args, err := extractLeadingProjectRoot(args)
	if err != nil {
		return err
	}
	if handled, err := handleControlHelp(args, out); handled {
		return err
	}
	root, err = resolveProjectRoot(root, os.Getwd)
	if err != nil {
		return err
	}
	if args[0] == "worker" {
		return runWorkerCommand(ctx, root, args[1:], out, execOperatorCommand)
	}
	switch args[0] {
	case "network-config":
		return RunNetworkConfigCommand(ctx, append([]string{"--config", filepath.Join(root, "config", "config.yml")}, args[1:]...), out)
	case "network-rules":
		return RunNetworkRulesCommand(ctx, args[1:], out)
	case "network-metrics":
		return RunNetworkMetricsCommand(ctx, args[1:], out)
	}
	command, commandArgs, err := controlCommand(root, args)
	if err != nil {
		return err
	}
	switch command {
	case "reset", "clean":
		return RunMaintenanceCommand(ctx, command, commandArgs, out)
	case "validate-config":
		return RunDatabaseCommand(ctx, command, commandArgs, out)
	default:
		if canonicalControlCommand(args[0]) == "start" {
			return runStartCommand(ctx, commandArgs, out, execOperatorCommand, os.Getwd)
		}
		return RunOperatorCommand(ctx, command, commandArgs, out)
	}
}

func controlCommand(root string, args []string) (string, []string, error) {
	const location = "bootstrap.controlCommand"
	command, options := canonicalControlCommand(args[0]), args[1:]
	projectArgs := []string{"--project-root", root}
	switch command {
	case "setup", "reset", "clean":
		if command != "setup" {
			options = withoutFastOption(options)
		}
		return command, append(projectArgs, options...), nil
	case "validate":
		return "validate-config", append([]string{"--config", filepath.Join(root, "config", "config.yml")}, options...), nil
	case "compose":
		// Only a leading compatibility flag belongs to control; keep command payloads intact.
		if len(options) > 0 && options[0] == "--fast" {
			options = options[1:]
		}
		return "compose", append(projectArgs, options...), nil
	case "monitoring":
		projectArgs = append(projectArgs, "-f", filepath.Join(root, "deployment", "docker", "compose.monitoring.yml"), "--profile", "monitoring")
		return "compose", append(projectArgs, options...), nil
	case "start", "scale":
		var err error
		options, err = controlDeploymentArgs(command, options)
		if err != nil {
			return "", nil, err
		}
	case "build":
		projectArgs = append(projectArgs, command)
		options = withoutFastOption(options)
	case "stop", "down", "restart", "ps", "logs":
		projectArgs = append(projectArgs, command)
	case "status", "tokens", "pause", "resume", "set-round-limit", "runtime-capacity":
		projectArgs = append(projectArgs, "exec", "-T", "api", "forcad", command)
	default:
		return "", nil, fmt.Errorf("%s - unknown command %q; run ./control.sh --help", location, command)
	}
	return "compose", append(projectArgs, options...), nil
}

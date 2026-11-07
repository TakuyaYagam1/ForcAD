package bootstrap

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// runStartCommand keeps the pooler's replica budget aligned with this start's
// explicit scale options. Raw Compose forwarding and persistent settings are
// unchanged; only this Docker subprocess receives the overrides.
func runStartCommand(
	ctx context.Context,
	args []string,
	out io.Writer,
	runner operatorCommandRunner,
	workingDirectory func() (string, error),
) error {
	const location = "bootstrap.runStartCommand"

	_, commandArgs, err := extractLeadingProjectRoot(args)
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	targets, err := composeScaleTargets(commandArgs)
	if err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	if len(targets) > 0 && composeUpOption(commandArgs, "--no-deps") {
		return fmt.Errorf("%s - scaled start with --no-deps cannot update PgBouncer budgets; use control scale with the deployed pooler, or start with dependencies", location)
	}
	if err := runOperatorCommand(ctx, "compose", args, out, withReplicaEnvironment(runner, targets), workingDirectory); err != nil {
		return fmt.Errorf("%s - start scaled deployment: %w", location, err)
	}
	return nil
}

func withReplicaEnvironment(runner operatorCommandRunner, targets map[string]int) operatorCommandRunner {
	if runner == nil || len(targets) == 0 {
		return runner
	}
	var overrides []string
	for _, role := range []string{"api", "receiver", "worker", "ticker"} {
		if count, ok := targets[role]; ok {
			overrides = append(overrides, "FORCAD_"+strings.ToUpper(role)+"_REPLICAS="+strconv.Itoa(count))
		}
	}
	return func(
		ctx context.Context,
		name string,
		args, environment []string,
		stdin io.Reader,
		stdout, stderr io.Writer,
	) error {
		const location = "bootstrap.withReplicaEnvironment"
		if err := runner(ctx, name, args, append(slices.Clone(environment), overrides...), stdin, stdout, stderr); err != nil {
			return fmt.Errorf("%s - execute command with replica budgets: %w", location, err)
		}
		return nil
	}
}

func composeScaleTargets(args []string) (map[string]int, error) {
	const location = "bootstrap.composeScaleTargets"

	if len(args) == 0 || args[0] != "up" {
		return nil, nil
	}
	targets := make(map[string]int)
	for i := 1; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		var value string
		switch {
		case args[i] == "--scale":
			i++
			if i == len(args) {
				return nil, fmt.Errorf("%s - --scale requires SERVICE=COUNT", location)
			}
			value = args[i]
		case strings.HasPrefix(args[i], "--scale="):
			value = strings.TrimPrefix(args[i], "--scale=")
		default:
			continue
		}
		role, raw, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("%s - --scale requires SERVICE=COUNT", location)
		}
		if !capacityRole(role) {
			continue
		}
		count, err := replicaCount(raw)
		if err != nil {
			return nil, fmt.Errorf("%s - %w", location, err)
		}
		// Docker Compose uses the last value for repeated --scale options.
		targets[role] = count
	}
	return targets, nil
}

func composeUpOption(args []string, option string) bool {
	if len(args) == 0 || args[0] != "up" {
		return false
	}
	enabled := false
	for _, arg := range args[1:] {
		if arg == "--" {
			break
		}
		switch arg {
		case option, option + "=true":
			enabled = true
		case option + "=false":
			enabled = false
		}
	}
	return enabled
}

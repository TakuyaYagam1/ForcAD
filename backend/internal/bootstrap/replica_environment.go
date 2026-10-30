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
func runStartCommand(ctx context.Context, args []string, out io.Writer, runner operatorCommandRunner, workingDirectory func() (string, error)) error {
	_, commandArgs, err := extractLeadingProjectRoot(args)
	if err != nil {
		return err
	}
	targets, err := composeScaleTargets(commandArgs)
	if err != nil {
		return err
	}
	if len(targets) > 0 && composeUpOption(commandArgs, "--no-deps") {
		return fmt.Errorf("bootstrap.runStartCommand - scaled start with --no-deps cannot update PgBouncer budgets; use control scale with the deployed pooler, or start with dependencies")
	}
	return runOperatorCommand(ctx, "compose", args, out, withReplicaEnvironment(runner, targets), workingDirectory)
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
	return func(ctx context.Context, name string, args, environment []string, stdin io.Reader, stdout, stderr io.Writer) error {
		return runner(ctx, name, args, append(slices.Clone(environment), overrides...), stdin, stdout, stderr)
	}
}

func composeScaleTargets(args []string) (map[string]int, error) {
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
				return nil, fmt.Errorf("bootstrap.composeScaleTargets - --scale requires SERVICE=COUNT")
			}
			value = args[i]
		case strings.HasPrefix(args[i], "--scale="):
			value = strings.TrimPrefix(args[i], "--scale=")
		default:
			continue
		}
		role, raw, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("bootstrap.composeScaleTargets - --scale requires SERVICE=COUNT")
		}
		if !capacityRole(role) {
			continue
		}
		count, err := replicaCount(raw)
		if err != nil {
			return nil, err
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

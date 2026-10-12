package bootstrap

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	localoperator "github.com/TakuyaYagam1/ForcAD/internal/operator"
)

type operatorCommandRunner func(context.Context, string, []string, io.Reader, io.Writer, io.Writer) error

// RunOperatorCommand runs local setup or forwards a Compose command to Docker.
func RunOperatorCommand(ctx context.Context, command string, args []string, out io.Writer) error {
	return runOperatorCommand(ctx, command, args, out, execOperatorCommand, os.Getwd)
}

func runOperatorCommand(
	ctx context.Context,
	command string,
	args []string,
	out io.Writer,
	runner operatorCommandRunner,
	workingDirectory func() (string, error),
) error {
	const location = "bootstrap.runOperatorCommand"
	if ctx == nil {
		return fmt.Errorf("%s - operator command context must not be nil", location)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s - operator command canceled: %w", location, err)
	}
	if out == nil {
		out = os.Stdout
	}
	if command != "setup" && command != "compose" {
		return fmt.Errorf("%s - unknown operator command %q", location, command)
	}

	if command == "setup" {
		return runSetupCommand(args, out, workingDirectory)
	}

	root, commandArgs, err := extractLeadingProjectRoot(args)
	if err != nil {
		return err
	}
	root, err = resolveProjectRoot(root, workingDirectory)
	if err != nil {
		return err
	}
	composeArgs, err := localoperator.ComposeArgs(root, os.Getenv("TEST") != "")
	if err != nil {
		return err
	}
	composeArgs = append(composeArgs, commandArgs...)
	if runner == nil {
		return fmt.Errorf("%s - Docker Compose command runner is required", location)
	}
	if err := runner(ctx, composeArgs[0], composeArgs[1:], os.Stdin, out, os.Stderr); err != nil {
		return fmt.Errorf("%s - run Docker Compose command: %w", location, err)
	}
	return nil
}

func extractLeadingProjectRoot(args []string) (string, []string, error) {
	const location = "bootstrap.extractLeadingProjectRoot"
	if len(args) == 0 {
		return "", args, nil
	}
	arg := args[0]
	var root string
	switch {
	case arg == "--project-root" || arg == "-project-root":
		if len(args) < 2 {
			return "", nil, fmt.Errorf("%s - project-root requires a path", location)
		}
		root = args[1]
		args = args[2:]
	case strings.HasPrefix(arg, "--project-root="):
		root = strings.TrimPrefix(arg, "--project-root=")
		args = args[1:]
	case strings.HasPrefix(arg, "-project-root="):
		root = strings.TrimPrefix(arg, "-project-root=")
		args = args[1:]
	default:
		return "", args, nil
	}
	if strings.TrimSpace(root) == "" {
		return "", nil, fmt.Errorf("%s - project-root must not be empty", location)
	}
	return root, args, nil
}

func resolveProjectRoot(root string, workingDirectory func() (string, error)) (string, error) {
	const location = "bootstrap.resolveProjectRoot"
	if strings.TrimSpace(root) != "" {
		return root, nil
	}
	root, err := workingDirectory()
	if err != nil {
		return "", fmt.Errorf("%s - resolve current directory: %w", location, err)
	}
	return root, nil
}

func execOperatorCommand(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	// #nosec G702 -- This local CLI deliberately forwards operator arguments to Docker without a shell.
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func runSetupCommand(args []string, out io.Writer, workingDirectory func() (string, error)) error {
	const location = "bootstrap.runSetupCommand"
	options := flag.NewFlagSet("setup", flag.ContinueOnError)
	options.SetOutput(out)
	projectRoot := options.String("project-root", "", "ForcAD project root (defaults to the current directory)")
	redis := options.String("redis", "", "external Redis host or host:port")
	database := options.String("database", "", "external database host or host:port")
	rabbitMQ := options.String("rabbitmq", "", "external RabbitMQ host or host:port")
	if err := options.Parse(args); err != nil {
		return fmt.Errorf("%s - parse setup command: %w", location, err)
	}
	if options.NArg() != 0 {
		return fmt.Errorf("%s - unexpected setup arguments", location)
	}
	root, err := resolveProjectRoot(*projectRoot, workingDirectory)
	if err != nil {
		return err
	}
	if err := localoperator.Setup(root, localoperator.SetupOptions{
		Redis: *redis, Database: *database, RabbitMQ: *rabbitMQ,
	}); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "Setup complete")
	if err != nil {
		return fmt.Errorf("%s - write setup result: %w", location, err)
	}
	return nil
}

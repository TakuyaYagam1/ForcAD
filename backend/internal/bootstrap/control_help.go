package bootstrap

import (
	"flag"
	"fmt"
	"io"
)

func handleControlHelp(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		return true, writeControlHelp(out, args)
	}
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		if err := writeControlHelp(out, []string{"help", args[0]}); err != nil {
			return true, err
		}
		return true, flag.ErrHelp
	}
	return false, nil
}

const controlHelp = `Usage: ./control.sh COMMAND [OPTIONS]

Deployment:
  setup                 Prepare configuration and private credentials
  build                 Build container images
  start, up             Build and start containers; -w/--workers N sets worker replicas
  worker                Prepare external storage and start workers with their pooler
  scale                 Change replicas: -s/--service SERVICE COUNT (repeatable)
  stop                  Stop containers, preserving game data
  down                  Remove containers and the project network
  restart               Restart containers
  ps                    Show container status
  logs                  Show service logs
  compose, rd           Forward arguments to the configured Docker Compose project
  monitoring            Compose commands with the optional monitoring overlay
  network-config        Export public team networks for the router (no credentials)
  network-rules         Render nftables counter rules from the exported networks
  network-metrics       Serve router counters as Prometheus metrics

Game:
  pause, resume         Pause or resume rounds and flag acceptance
  status                Print game status
  tokens, print_tokens  Print team tokens (print-tokens is also accepted)
  set-round-limit       Change the round limit; requires --rounds N
  runtime-capacity      Report configured limits; accepts replica counts
  validate              Validate config/config.yml; --full also checks deployment settings
  reset                 Reset game data; requires --confirm; --full removes local PostgreSQL
  clean                 Remove game data and generated credentials; requires --confirm

Run ./control.sh COMMAND --help for options. See docs/CONTROL.md for examples.
Start and finish game sessions in the admin panel. start only starts containers.
The deprecated --fast flag is accepted without changing deployment behavior.
`

func canonicalControlCommand(command string) string {
	switch command {
	case "up":
		return "start"
	case "rd":
		return "compose"
	case "print_tokens", "print-tokens":
		return "tokens"
	case "validate-config":
		return "validate"
	default:
		return command
	}
}

func writeControlHelp(out io.Writer, args []string) error {
	const location = "bootstrap.writeControlHelp"
	text := controlHelp
	if len(args) > 1 {
		usage, ok := controlUsage(canonicalControlCommand(args[1]))
		if !ok || args[0] != "help" || len(args) != 2 {
			return fmt.Errorf("%s - use help [COMMAND] with a known command", location)
		}
		text = "Usage: ./control.sh " + usage + "\nSee docs/CONTROL.md for behavior and examples.\n"
	}
	if _, err := io.WriteString(out, text); err != nil {
		return fmt.Errorf("%s - write command help: %w", location, err)
	}
	return nil
}

func controlUsage(command string) (string, bool) {
	usages := map[string]string{
		"setup":            "setup [--redis ADDR] [--database ADDR] [--rabbitmq ADDR]",
		"build":            "build [--fast] [COMPOSE_BUILD_OPTIONS] [SERVICE...]",
		"start":            "start [-w N | --workers N] [--fast] [COMPOSE_UP_OPTIONS] [SERVICE...]",
		"worker":           "worker --redis ADDR --database ADDR --rabbitmq ADDR [-w N | --workers N] [--fast]",
		"scale":            "scale -s SERVICE COUNT [-s SERVICE COUNT...] (also --service; celery maps to worker)",
		"stop":             "stop [--timeout SECONDS] [SERVICE...]",
		"down":             "down [COMPOSE_DOWN_OPTIONS]",
		"restart":          "restart [--timeout SECONDS] [SERVICE...]",
		"ps":               "ps [COMPOSE_PS_OPTIONS] [SERVICE...]",
		"logs":             "logs [--follow] [--tail N] [SERVICE...]",
		"compose":          "compose [--fast] COMPOSE_ARGUMENTS... (arguments after --fast pass through unchanged)",
		"monitoring":       "monitoring COMPOSE_ARGUMENTS... (enables the monitoring profile)",
		"network-config":   "network-config [--config PATH]",
		"network-rules":    "network-rules --config PATH",
		"network-metrics":  "network-metrics --config PATH [--listen ADDR]",
		"pause":            "pause",
		"resume":           "resume",
		"status":           "status",
		"tokens":           "tokens",
		"set-round-limit":  "set-round-limit --rounds N (0 removes the limit)",
		"runtime-capacity": "runtime-capacity [--api-replicas N] [--receiver-replicas N] [--worker-replicas N] [--ticker-replicas N]",
		"validate":         "validate [--full] [--config PATH]",
		"reset":            "reset --confirm [--full] [--fast]",
		"clean":            "clean --confirm [--fast]",
	}
	usage, ok := usages[command]
	return usage, ok
}

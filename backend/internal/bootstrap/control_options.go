package bootstrap

import (
	"fmt"
	"strconv"
	"strings"
)

func withoutFastOption(args []string) []string {
	result := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "--" {
			return append(result, args[i:]...)
		}
		if arg != "--fast" {
			result = append(result, arg)
		}
	}
	return result
}

func controlDeploymentArgs(command string, args []string) ([]string, error) {
	if command == "scale" {
		return scaleArguments(args)
	}
	return startArguments(args)
}

func startArguments(args []string) ([]string, error) {
	const location = "bootstrap.startArguments"
	result := []string{"up", "-d", "--build"}
	args = withoutFastOption(args)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append(result, args[i:]...), nil
		}
		value, isWorkerOption := workerOptionValue(arg)
		if !isWorkerOption {
			result = append(result, arg)
			continue
		}
		if arg == "-w" || arg == "--workers" {
			i++
			if i == len(args) {
				return nil, fmt.Errorf("%s - %s requires a replica count", location, arg)
			}
			value = args[i]
		}
		count, err := replicaCount(value)
		if err != nil {
			return nil, err
		}
		result = append(result, "--scale", "worker="+strconv.Itoa(count))
	}
	return result, nil
}

func workerOptionValue(arg string) (string, bool) {
	switch {
	case arg == "-w" || arg == "--workers":
		return "", true
	case strings.HasPrefix(arg, "--workers="):
		return strings.TrimPrefix(arg, "--workers="), true
	case strings.HasPrefix(arg, "-w") && len(arg) > 2:
		return strings.TrimPrefix(arg[2:], "="), true
	default:
		return "", false
	}
}

func replicaCount(value string) (int, error) {
	const location = "bootstrap.replicaCount"
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 || count > maxCapacityReplicas {
		return 0, fmt.Errorf("%s - replica count must be an integer between 0 and %d", location, maxCapacityReplicas)
	}
	return count, nil
}

func scaleArguments(args []string) ([]string, error) {
	const location = "bootstrap.scaleArguments"
	result := []string{"up", "-d", "--no-recreate", "--no-deps"}
	var services []string
	seen := make(map[string]bool)
	for len(args) > 0 {
		if len(args) < 3 || (args[0] != "-s" && args[0] != "--service") {
			return nil, fmt.Errorf("%s - use scale -s SERVICE COUNT [-s SERVICE COUNT...]", location)
		}
		service, err := scaleService(args[1])
		if err != nil {
			return nil, err
		}
		if seen[service] {
			return nil, fmt.Errorf("%s - service %q specified more than once", location, service)
		}
		count, err := replicaCount(args[2])
		if err != nil {
			return nil, err
		}
		seen[service] = true
		services = append(services, service)
		result = append(result, "--scale", service+"="+strconv.Itoa(count))
		args = args[3:]
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("%s - at least one -s SERVICE COUNT is required", location)
	}
	return append(result, services...), nil
}

func scaleService(name string) (string, error) {
	const location = "bootstrap.scaleService"
	switch name {
	case "celery":
		name = "worker"
	case "http-receiver":
		name = "receiver"
	case "client-api", "admin-api", "events":
		name = "api"
	}
	for _, character := range name {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '-' {
			continue
		}
		return "", fmt.Errorf("%s - invalid Compose service name", location)
	}
	if name == "" || strings.HasPrefix(name, "-") {
		return "", fmt.Errorf("%s - service name is required and must not be an option", location)
	}
	return name, nil
}

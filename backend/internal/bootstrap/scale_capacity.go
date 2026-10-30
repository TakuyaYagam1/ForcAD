package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
)

const scaleCapacityTimeout = 20 * time.Second

// Only numeric budget fields leave the container; config credentials and the
// inspect environment are never printed by these read-only Docker commands.
const poolerCapacityAWK = `$1 ~ /^forcad_(api|receiver|worker|ticker)$/ { role=$1; sub(/^forcad_/,"",role); for(i=1;i<=NF;i++) if($i ~ /^max_db_client_connections=[0-9]+$/) { split($i,a,"="); print role,a[2] } } $1 == "max_client_conn" && $2 == "=" && $3 ~ /^[0-9]+$/ { print "global",$3 }`

const applicationCapacityTemplate = `{{$role := index .Config.Labels "com.docker.compose.service"}}{{if or (eq $role "api") (eq $role "receiver") (eq $role "worker") (eq $role "ticker")}}{{$role}} {{.State.Running}} {{if or (eq (index .Config.Labels "com.docker.compose.oneoff") "True") (eq (index .Config.Labels "com.docker.compose.oneoff") "true")}}oneoff{{else}}service{{end}}{{range .Config.Env}}{{$key := index (split . "=") 0}}{{if or (eq $key "FORCAD_ROLE") (eq $key "DATABASE_MAX_CONNS") (eq $key "FORCAD_API_DATABASE_MAX_CONNS") (eq $key "FORCAD_RECEIVER_DATABASE_MAX_CONNS") (eq $key "FORCAD_WORKER_DATABASE_MAX_CONNS") (eq $key "FORCAD_TICKER_DATABASE_MAX_CONNS")}} {{.}}{{end}}{{end}}{{end}}`

type scaleRoleCapacity struct {
	replicas int
	pool     int
}

func checkScaleCapacity(ctx context.Context, compose, args []string, runner operatorCommandRunner) error {
	targets, err := capacityScaleTargets(args)
	if err != nil || len(targets) == 0 {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, scaleCapacityTimeout)
	defer cancel()
	read := func(tail ...string) ([]byte, error) {
		command := append(append([]string(nil), compose[1:]...), tail...)
		return readCapacityCommand(ctx, runner, command)
	}
	capsData, err := read("exec", "-T", "pgbouncer", "awk", poolerCapacityAWK, "/run/pgbouncer/pgbouncer.ini")
	if err != nil {
		return scaleCapacityUnavailable("read deployed PgBouncer budgets", err)
	}
	caps, err := parsePoolerCapacity(capsData)
	if err != nil {
		return err
	}
	configData, err := read("config", "--format", "json")
	if err != nil {
		return scaleCapacityUnavailable("read proposed application pools", err)
	}
	roles, err := configuredScalePools(configData)
	if err != nil {
		return err
	}
	if err := validateScaleTargets(roles, targets); err != nil {
		return err
	}
	// Inventory the whole project so removed-but-still-running app containers
	// remain in the aggregate budget. The inspect template omits infrastructure.
	ids, err := read("ps", "--all", "--quiet")
	if err != nil {
		return scaleCapacityUnavailable("list deployed application replicas", err)
	}
	if containers := strings.Fields(string(ids)); len(containers) > 0 {
		for _, id := range containers {
			if !containerID(id) {
				return scaleCapacityUnavailable("read container IDs", nil)
			}
		}
		inspection, err := readCapacityCommand(ctx, runner, append([]string{"inspect", "--format", applicationCapacityTemplate}, containers...))
		if err != nil {
			return scaleCapacityUnavailable("read deployed application pools", err)
		}
		if err := addDeployedScalePools(roles, inspection); err != nil {
			return err
		}
	}
	for role, count := range targets {
		capacity := roles[role]
		capacity.replicas = count
		roles[role] = capacity
	}
	return validateScaleCapacity(roles, caps)
}

func validateScaleTargets(roles map[string]scaleRoleCapacity, targets map[string]int) error {
	for role := range targets {
		if _, configured := roles[role]; !configured {
			return scaleCapacityUnavailable("find requested application service "+role, nil)
		}
	}
	return nil
}

func capacityScaleTargets(args []string) (map[string]int, error) {
	// Raw Compose forwarding remains unchanged. This protects the control
	// shortcut that explicitly promises not to recreate the existing pooler.
	if !composeUpOption(args, "--no-recreate") {
		return nil, nil
	}
	return composeScaleTargets(args)
}

func readCapacityCommand(ctx context.Context, runner operatorCommandRunner, args []string) ([]byte, error) {
	var out bytes.Buffer
	// Rendered Compose config can contain secrets. Capture it privately and
	// discard Docker stderr rather than copying either stream to CLI output.
	if err := runner(ctx, "docker", args, nil, nil, &out, io.Discard); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func scaleCapacityUnavailable(operation string, err error) error {
	if err != nil {
		return fmt.Errorf("bootstrap.checkScaleCapacity - cannot %s; refusing scale without verified deployed client budgets: %w", operation, err)
	}
	return fmt.Errorf("bootstrap.checkScaleCapacity - cannot %s; refusing scale without verified deployed client budgets", operation)
}

func parsePoolerCapacity(data []byte) (map[string]int, error) {
	caps := make(map[string]int)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || (!capacityRole(fields[0]) && fields[0] != "global") {
			return nil, scaleCapacityUnavailable("parse deployed PgBouncer budgets", nil)
		}
		value, err := strconv.Atoi(fields[1])
		if err != nil || value < 1 || value > 65535 || caps[fields[0]] != 0 {
			return nil, scaleCapacityUnavailable("parse deployed PgBouncer budgets", nil)
		}
		caps[fields[0]] = value
	}
	if len(caps) != 5 {
		return nil, scaleCapacityUnavailable("verify all deployed PgBouncer budgets", nil)
	}
	return caps, nil
}

func configuredScalePools(data []byte) (map[string]scaleRoleCapacity, error) {
	var document struct {
		Services map[string]struct {
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, scaleCapacityUnavailable("decode proposed application pools", err)
	}
	roles := make(map[string]scaleRoleCapacity, 4)
	for _, role := range []string{"api", "receiver", "worker", "ticker"} {
		service, ok := document.Services[role]
		if !ok {
			continue // Worker-only deployments have zero local replicas of absent roles.
		}
		pool, err := config.DatabasePoolSize(role, func(key string) (string, bool) { value, ok := service.Environment[key]; return value, ok })
		if err != nil {
			return nil, scaleCapacityUnavailable("decode proposed pool for "+role, err)
		}
		roles[role] = scaleRoleCapacity{pool: int(pool)}
	}
	return roles, nil
}

func addDeployedScalePools(roles map[string]scaleRoleCapacity, data []byte) error {
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		} // Non-application containers produce no inspection fields.
		if len(fields) < 3 || !capacityRole(fields[0]) || (fields[1] != "true" && fields[1] != "false") {
			return scaleCapacityUnavailable("parse deployed application pools", nil)
		}
		if fields[2] != "service" {
			return scaleCapacityUnavailable("account for one-off application containers; finish those commands before scaling", nil)
		}
		role := fields[0]
		environment := make(map[string]string)
		for _, pair := range fields[3:] {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				return scaleCapacityUnavailable("parse deployed pool for "+role, nil)
			}
			environment[key] = value
		}
		if environment["FORCAD_ROLE"] != role {
			return scaleCapacityUnavailable("verify deployed role "+role, nil)
		}
		pool, err := config.DatabasePoolSize(role, func(key string) (string, bool) { value, ok := environment[key]; return value, ok })
		if err != nil {
			return scaleCapacityUnavailable("decode deployed pool for "+role, err)
		}
		capacity := roles[role]
		capacity.pool = max(capacity.pool, int(pool)) // --no-recreate can reuse an older container.
		if fields[1] == "true" {
			capacity.replicas++
		}
		roles[role] = capacity
	}
	return nil
}

func validateScaleCapacity(roles map[string]scaleRoleCapacity, caps map[string]int) error {
	total := 0
	for _, role := range []string{"api", "receiver", "worker", "ticker"} {
		capacity := roles[role]
		required := capacity.replicas*capacity.pool + 8
		if required > caps[role] {
			return fmt.Errorf("bootstrap.checkScaleCapacity - %s needs %d clients (%d replicas x %d + 8 reserve), deployed PgBouncer permits %d; set FORCAD_%s_REPLICAS and PGBOUNCER_%s_CLIENTS, then recreate the pooler in planned maintenance before scaling", role, required, capacity.replicas, capacity.pool, caps[role], strings.ToUpper(role), strings.ToUpper(role))
		}
		total += required
	}
	if total > caps["global"] {
		return fmt.Errorf("bootstrap.checkScaleCapacity - application pools need %d clients, deployed PgBouncer permits %d; update PGBOUNCER_MAX_CLIENTS and recreate the pooler in planned maintenance before scaling", total, caps["global"])
	}
	return nil
}

func capacityRole(role string) bool {
	return role == "api" || role == "receiver" || role == "worker" || role == "ticker"
}

func containerID(id string) bool {
	if len(id) < 12 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

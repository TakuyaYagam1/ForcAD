package checker

import "strings"

// filterCheckerEnvironment avoids accidentally passing the platform's connection
// credentials to trusted checker programs. This is not a sandbox: the checker
// still runs as the worker's identity. Keep arbitrary checker-specific variables
// and operating-system settings intact.
func filterCheckerEnvironment(source []string) []string {
	env := make([]string, 0, len(source))
	for _, entry := range source {
		key, _, _ := strings.Cut(entry, "=")
		if !platformEnvironmentKey(key) {
			env = append(env, entry)
		}
	}
	return env
}

func platformEnvironmentKey(key string) bool {
	// Windows environment names are case-insensitive. Treat these reserved
	// platform names consistently on every host, including duplicate entries.
	key = strings.ToUpper(key)
	for _, prefix := range []string{"DATABASE_", "POSTGRES_", "PGBOUNCER_", "RABBITMQ_", "FORCAD_RUNTIME_DB_"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	switch key {
	case "AMQP_URL", "REDIS_URL", "REDIS_PASSWORD", "REDIS_PASSWORD_FILE",
		"ADMIN_USERNAME", "ADMIN_PASSWORD", "CONTROL_TOKEN", "FORCAD_CONFIG",
		"PGPASSWORD", "PGPASSFILE", "PGSERVICE", "PGSERVICEFILE":
		return true
	default:
		return false
	}
}

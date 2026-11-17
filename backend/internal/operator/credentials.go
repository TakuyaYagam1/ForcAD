package operator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/TakuyaYagam1/ForcAD/config"
	"go.yaml.in/yaml/v3"
)

const (
	postgresPort           = 5432
	natsPort               = 4222
	redisPort              = 6379
	maxTCPPort             = 65535
	maxRedisDatabaseIndex  = 1024
	minAdminPasswordLength = 16
	secretBytes            = 32
)

func completeCredentials(document *config.Document, tree *yaml.Node) error {
	const location = "operator.completeCredentials"
	if document.Admin == nil {
		password, err := randomSecret()
		if err != nil {
			return fmt.Errorf("%s - generate admin password: %w", location, err)
		}
		document.Admin = &config.Credentials{
			Username: "forcad",
			Password: password,
		}
	} else if !hasPath(tree, "admin", "username") || !hasPath(tree, "admin", "password") {
		return fmt.Errorf("%s - admin configuration requires username and password", location)
	}

	if document.Storages == nil {
		databasePassword, err := randomSecret()
		if err != nil {
			return fmt.Errorf("%s - generate database password: %w", location, err)
		}
		natsPassword, err := randomSecret()
		if err != nil {
			return fmt.Errorf("%s - generate NATS password: %w", location, err)
		}
		redisPassword, err := randomSecret()
		if err != nil {
			return fmt.Errorf("%s - generate Redis password: %w", location, err)
		}
		username := document.Admin.Username
		document.Storages = &config.Storages{
			DB: config.Database{
				User:     username,
				Password: databasePassword,
				Host:     "postgres",
				Port:     postgresPort,
				DBName:   "forcad",
			},
			NATS: config.NATS{
				User:     username,
				Password: natsPassword,
				Host:     "nats",
				Port:     natsPort,
			},
			Redis: config.Redis{
				Password: redisPassword,
				Host:     "redis",
				Port:     redisPort,
			},
		}
	} else {
		for _, path := range [][]string{{"storages", "db", "user"}, {"storages", "db", "password"}, {"storages", "nats", "user"}, {"storages", "nats", "password"}, {"storages", "redis", "password"}} {
			if !hasPath(tree, path...) {
				return fmt.Errorf("%s - configuration requires %s", location, strings.Join(path, "."))
			}
		}
		setStorageDefaults(document.Storages, tree)
	}
	if document.Storages.DB.Runtime == nil {
		password, err := randomSecret()
		if err != nil {
			return fmt.Errorf("%s - %w", location, err)
		}
		suffix, err := randomSecret()
		if err != nil {
			return fmt.Errorf("%s - %w", location, err)
		}
		username := "forcad_runtime_" + suffix[:12]
		document.Storages.DB.Runtime = &config.Credentials{
			Username: username,
			Password: password,
		}
	}
	if err := validateCompletedCredentials(*document); err != nil {
		return fmt.Errorf("%s - validate completed credentials: %w", location, err)
	}
	return nil
}

func validateCompletedCredentials(document config.Document) error {
	const location = "operator.validateCompletedCredentials"
	if document.Admin == nil || strings.TrimSpace(document.Admin.Username) == "" {
		return fmt.Errorf("%s - admin username must not be empty", location)
	}
	if len(document.Admin.Password) < minAdminPasswordLength {
		return fmt.Errorf("%s - admin password must contain at least 16 characters", location)
	}
	if document.Storages == nil {
		return fmt.Errorf("%s - storage settings are required", location)
	}
	storage := document.Storages
	if err := validateRuntimeCredentials(storage.DB); err != nil {
		return fmt.Errorf("%s - %w", location, err)
	}
	for _, item := range []struct {
		name string
		host string
		port int
	}{
		{
			name: "database",
			host: storage.DB.Host,
			port: storage.DB.Port,
		},
		{
			name: "NATS",
			host: storage.NATS.Host,
			port: storage.NATS.Port,
		},
		{
			name: "Redis",
			host: storage.Redis.Host,
			port: storage.Redis.Port,
		},
	} {
		if strings.TrimSpace(item.host) == "" {
			return fmt.Errorf("%s - %s host must not be empty", location, item.name)
		}
		if item.port < 1 || item.port > maxTCPPort {
			return fmt.Errorf("%s - %s port must be between 1 and 65535", location, item.name)
		}
	}
	for _, item := range []struct {
		name  string
		value string
	}{
		{
			name:  "database user",
			value: storage.DB.User,
		},
		{
			name:  "database name",
			value: storage.DB.DBName,
		},
		{
			name:  "NATS user",
			value: storage.NATS.User,
		},
		{
			name:  "NATS password",
			value: storage.NATS.Password,
		},
	} {
		if strings.TrimSpace(item.value) == "" {
			return fmt.Errorf("%s - %s must not be empty", location, item.name)
		}
	}
	if storage.Redis.DB < 0 || storage.Redis.DB > maxRedisDatabaseIndex {
		return fmt.Errorf("%s - Redis database index must be between 0 and 1024", location)
	}
	return nil
}

func validateRuntimeCredentials(database config.Database) error {
	const location = "operator.validateRuntimeCredentials"
	runtime := database.Runtime
	if runtime == nil {
		return nil // Legacy documents can be upgraded by setup.
	}
	if strings.TrimSpace(runtime.Username) == "" || len(runtime.Username) > 63 || runtime.Username == database.User || strings.ContainsAny(runtime.Username, "\r\n\x00") || len(runtime.Password) < minAdminPasswordLength || runtime.Password == database.Password {
		return fmt.Errorf("%s - database runtime requires a distinct username (1-63 bytes) and password (at least 16 characters)", location)
	}
	return nil
}

func setStorageDefaults(storages *config.Storages, tree *yaml.Node) {
	if !hasPath(tree, "storages", "db", "host") {
		storages.DB.Host = "postgres"
	}
	if !hasPath(tree, "storages", "db", "port") {
		storages.DB.Port = postgresPort
	}
	if !hasPath(tree, "storages", "db", "dbname") {
		storages.DB.DBName = "forcad"
	}
	if !hasPath(tree, "storages", "nats", "host") {
		storages.NATS.Host = "nats"
	}
	if !hasPath(tree, "storages", "nats", "port") {
		storages.NATS.Port = natsPort
	}
	if !hasPath(tree, "storages", "redis", "host") {
		storages.Redis.Host = "redis"
	}
	if !hasPath(tree, "storages", "redis", "port") {
		storages.Redis.Port = redisPort
	}
}

func applyExternalAddresses(document *config.Document, options SetupOptions) error {
	const location = "operator.applyExternalAddresses"
	addresses := []struct {
		name        string
		value       string
		defaultPort int
		targetHost  *string
		targetPort  *int
	}{
		{"Redis", options.Redis, redisPort, &document.Storages.Redis.Host, &document.Storages.Redis.Port},
		{"database", options.Database, postgresPort, &document.Storages.DB.Host, &document.Storages.DB.Port},
		{"NATS", options.NATS, natsPort, &document.Storages.NATS.Host, &document.Storages.NATS.Port},
	}
	for _, item := range addresses {
		if item.value == "" {
			continue
		}
		host, port, err := parseHostAddress(item.value, item.defaultPort)
		if err != nil {
			return fmt.Errorf("%s - invalid %s address: %w", location, item.name, err)
		}
		*item.targetHost, *item.targetPort = host, port
	}
	return nil
}

func parseHostAddress(value string, defaultPort int) (string, int, error) {
	const location = "operator.parseHostAddress"
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, fmt.Errorf("%s - host is required", location)
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.String(), defaultPort, nil
	}
	host, port := value, defaultPort
	switch {
	case strings.HasPrefix(value, "["):
		parsedHost, parsedPort, err := net.SplitHostPort(value)
		if err != nil {
			return "", 0, fmt.Errorf("%s - expected HOST or HOST:PORT", location)
		}
		host = parsedHost
		port, err = strconv.Atoi(parsedPort)
		if err != nil {
			return "", 0, fmt.Errorf("%s - port must be an integer", location)
		}
	case strings.Count(value, ":") == 1:
		var parsedPort string
		host, parsedPort, _ = strings.Cut(value, ":")
		var err error
		port, err = strconv.Atoi(parsedPort)
		if err != nil {
			return "", 0, fmt.Errorf("%s - port must be an integer", location)
		}
	case strings.Contains(value, ":"):
		return "", 0, fmt.Errorf("%s - IPv6 addresses with a port must use [HOST]:PORT", location)
	}
	if strings.TrimSpace(host) == "" || strings.ContainsAny(host, "\r\n\x00") {
		return "", 0, fmt.Errorf("%s - host is required", location)
	}
	if port < 1 || port > maxTCPPort {
		return "", 0, fmt.Errorf("%s - port must be between 1 and 65535", location)
	}
	return host, port, nil
}

func randomSecret() (string, error) {
	const location = "operator.randomSecret"
	var secret [secretBytes]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", fmt.Errorf("%s - generate secret bytes: %w", location, err)
	}
	return hex.EncodeToString(secret[:]), nil
}

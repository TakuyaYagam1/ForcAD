package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Keep named game timezones available in minimal containers.
)

// Config contains process settings. Game settings are stored in PostgreSQL
// after initialization and are changed through organizer controls.
type Config struct {
	Role                   string
	HTTPAddr               string
	HTTPMaxInflight        int
	PublicOrigin           string
	TrustedProxyCIDRs      []string
	MetricsAddr            string
	DatabaseURL            string
	DatabasePoolerAddr     string
	DatabaseMaxConns       int32
	DatabaseTimeout        time.Duration
	RedisAddr              string
	RedisPassword          string
	RedisDB                int
	BrokerURL              string
	CheckerQueue           string
	AdminUsername          string
	AdminPassword          string
	ControlToken           string
	ConfigPath             string
	ShutdownTimeout        time.Duration
	RequestTimeout         time.Duration
	WorkerConcurrency      int
	CheckerMemoryMB        int
	CheckerOutputBytes     int
	CheckerWorkflowTimeout time.Duration
	JobGrace               time.Duration
	FlagRate               int
	FlagBurst              int
}

type LookupEnv func(string) (string, bool)

const (
	maxQueueNameLength     = 128
	minControlTokenLength  = 32
	minAdminPasswordLength = 16
	maxTCPPort             = 65535
	maxDatabaseMaxConns    = 1000
)

func New() (Config, error) { return Load(os.LookupEnv) }

func Load(lookup LookupEnv) (Config, error) {
	const location = "config.Load"
	value := func(key, fallback string) string {
		if v, ok := lookup(key); ok {
			return v
		}
		return fallback
	}
	c := Config{
		Role:               value("FORCAD_ROLE", "api"),
		HTTPAddr:           value("HTTP_ADDR", ":5000"),
		PublicOrigin:       strings.TrimRight(value("PUBLIC_ORIGIN", ""), "/"),
		MetricsAddr:        value("METRICS_ADDR", ":9090"),
		DatabaseURL:        value("DATABASE_URL", ""),
		DatabasePoolerAddr: value("DATABASE_POOLER_ADDR", ""),
		RedisAddr:          value("REDIS_ADDR", net.JoinHostPort(value("REDIS_HOST", "redis"), value("REDIS_PORT", "6379"))),
		RedisPassword:      value("REDIS_PASSWORD", ""),
		BrokerURL:          value("AMQP_URL", ""),
		CheckerQueue:       value("CHECKER_QUEUE", "forcad.checker.jobs"),
		AdminUsername:      value("ADMIN_USERNAME", ""),
		AdminPassword:      value("ADMIN_PASSWORD", ""),
		ControlToken:       value("CONTROL_TOKEN", ""),
		ConfigPath:         value("FORCAD_CONFIG", "/run/forcad-config/config.yml"),
	}
	if c.DatabaseURL == "" {
		u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(value("POSTGRES_HOST", "postgres"), value("POSTGRES_PORT", "5432")), Path: "/" + value("POSTGRES_DB", "forcad")}
		u.User = url.UserPassword(value("POSTGRES_USER", "forcad"), value("POSTGRES_PASSWORD", ""))
		q := u.Query()
		q.Set("sslmode", value("POSTGRES_SSLMODE", "prefer"))
		u.RawQuery = q.Encode()
		c.DatabaseURL = u.String()
	}
	if c.BrokerURL == "" {
		u := url.URL{Scheme: "amqp", Host: net.JoinHostPort(value("RABBITMQ_HOST", "rabbitmq"), value("RABBITMQ_PORT", "5672")), Path: "/" + value("RABBITMQ_VHOST", value("RABBITMQ_DEFAULT_VHOST", "forcad"))}
		u.User = url.UserPassword(value("RABBITMQ_USER", value("RABBITMQ_DEFAULT_USER", "forcad")), value("RABBITMQ_PASSWORD", value("RABBITMQ_DEFAULT_PASS", "")))
		c.BrokerURL = u.String()
	}
	var errs []error
	for _, cidr := range strings.Split(value("TRUSTED_PROXY_CIDRS", ""), ",") {
		if cidr = strings.TrimSpace(cidr); cidr != "" {
			c.TrustedProxyCIDRs = append(c.TrustedProxyCIDRs, cidr)
		}
	}
	integer := func(key string, fallback, minimum, maximum int) int {
		v, err := strconv.Atoi(value(key, strconv.Itoa(fallback)))
		if err != nil || v < minimum || v > maximum {
			errs = append(errs, fmt.Errorf("%s - %s must be an integer between %d and %d", location, key, minimum, maximum))
		}
		return v
	}
	duration := func(key string, fallback time.Duration) time.Duration {
		v, err := time.ParseDuration(value(key, fallback.String()))
		if err != nil || v <= 0 {
			errs = append(errs, fmt.Errorf("%s - %s must be a positive duration", location, key))
		}
		return v
	}
	poolSize, poolErr := DatabasePoolSize(c.Role, lookup)
	c.DatabaseMaxConns = poolSize
	if poolErr != nil {
		errs = append(errs, poolErr)
	}
	c.HTTPMaxInflight = integer("HTTP_MAX_INFLIGHT", 128, 1, 4096)
	c.DatabaseTimeout = duration("DATABASE_TIMEOUT", 5*time.Second)
	c.RedisDB = integer("REDIS_DB", 0, 0, 1024)
	c.WorkerConcurrency = integer("WORKER_CONCURRENCY", 8, 1, 1024)
	c.CheckerMemoryMB = integer("FORCAD_CHECKER_MEMORY_LIMIT_MB", 1024, 0, 1048576)
	c.CheckerOutputBytes = integer("FORCAD_CHECKER_OUTPUT_LIMIT_BYTES", 1<<20, 1024, 16<<20)
	c.CheckerWorkflowTimeout = duration("CHECKER_WORKFLOW_TIMEOUT", 30*time.Minute)
	c.JobGrace = duration("CHECKER_JOB_GRACE", 30*time.Second)
	c.ShutdownTimeout = duration("SHUTDOWN_TIMEOUT", 30*time.Second)
	c.RequestTimeout = duration("REQUEST_TIMEOUT", 10*time.Second)
	c.FlagRate = integer("FLAG_RATE", 200, 1, 1000000)
	c.FlagBurst = integer("FLAG_BURST", 400, 1, 1000000)
	if err := c.Validate(); err != nil {
		errs = append(errs, err)
	}
	if c.DatabasePoolerAddr != "" {
		if err := c.usePooler(); err != nil {
			errs = append(errs, err)
		}
	}
	return c, errors.Join(errs...)
}

func defaultDatabaseMaxConns(role string) int32 {
	switch role {
	case "receiver":
		return 32
	case "ticker":
		return 8
	default:
		return 16
	}
}

func DatabasePoolSize(role string, lookup LookupEnv) (int32, error) {
	const location = "config.DatabasePoolSize"
	for _, key := range []string{"FORCAD_" + strings.ToUpper(role) + "_DATABASE_MAX_CONNS", "DATABASE_MAX_CONNS"} {
		if raw, ok := lookup(key); ok {
			size, err := strconv.ParseInt(raw, 10, 32)
			if err != nil || size < 1 || size > maxDatabaseMaxConns {
				return 0, fmt.Errorf("%s - %s must be an integer between 1 and %d", location, key, maxDatabaseMaxConns)
			}
			return int32(size), nil
		}
	}
	return defaultDatabaseMaxConns(role), nil
}

func (c *Config) usePooler() error {
	const location = "config.Config.usePooler"
	host, port, err := net.SplitHostPort(c.DatabasePoolerAddr)
	n, numberErr := strconv.Atoi(port)
	if err != nil || host == "" || numberErr != nil || n < 1 || n > maxTCPPort {
		return fmt.Errorf("%s - DATABASE_POOLER_ADDR must contain a host and port", location)
	}
	address, err := url.Parse(c.DatabaseURL)
	if err != nil {
		return fmt.Errorf("%s - invalid PostgreSQL connection settings", location)
	}
	role := c.Role
	if role == "all" {
		role = "api"
	}
	address.Host, address.Path = c.DatabasePoolerAddr, "/forcad_"+role
	c.DatabaseURL = address.String()
	return nil
}

func (c Config) Validate() error {
	for _, validate := range []func() error{c.validateQueue, c.validateHTTP, c.validateRole, c.validateEndpoints, c.validateAdmin} {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c Config) validateQueue() error {
	const location = "config.Config.validateQueue"
	if len(c.CheckerQueue) == 0 || len(c.CheckerQueue) > maxQueueNameLength {
		return fmt.Errorf("%s - CHECKER_QUEUE must contain 1 to 128 characters", location)
	}
	for _, char := range c.CheckerQueue {
		if !validQueueCharacter(char) {
			return fmt.Errorf("%s - CHECKER_QUEUE contains an unsupported character", location)
		}
	}
	return nil
}

func validQueueCharacter(char rune) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' || char == '.' || char == '_' || char == '-'
}

func (c Config) validateHTTP() error {
	const location = "config.Config.validateHTTP"
	if c.ControlToken != "" && len(c.ControlToken) < minControlTokenLength {
		return fmt.Errorf("%s - CONTROL_TOKEN must contain at least 32 characters", location)
	}
	for _, cidr := range c.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("%s - TRUSTED_PROXY_CIDRS must contain comma-separated CIDR networks", location)
		}
	}
	if c.PublicOrigin != "" {
		u, err := url.Parse(c.PublicOrigin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return fmt.Errorf("%s - PUBLIC_ORIGIN must be an HTTP or HTTPS origin without a path", location)
		}
	}
	return nil
}

func (c Config) validateRole() error {
	const location = "config.Config.validateRole"
	switch c.Role {
	case "api", "receiver", "worker", "ticker", "all":
	default:
		return fmt.Errorf("%s - FORCAD_ROLE must be api, receiver, worker, ticker, or all", location)
	}
	return nil
}

func (c Config) validateEndpoints() error {
	const location = "config.Config.validateEndpoints"
	for _, addr := range []string{c.HTTPAddr, c.MetricsAddr, c.RedisAddr} {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("%s - listener and Redis addresses must contain a host and port", location)
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > maxTCPPort {
			return fmt.Errorf("%s - invalid port in process configuration", location)
		}
	}
	db, err := url.Parse(c.DatabaseURL)
	if err != nil || (db.Scheme != "postgres" && db.Scheme != "postgresql") || db.Hostname() == "" {
		return fmt.Errorf("%s - DATABASE_URL must be a PostgreSQL URL", location)
	}
	broker, err := url.Parse(c.BrokerURL)
	if err != nil || (broker.Scheme != "amqp" && broker.Scheme != "amqps") || broker.Hostname() == "" {
		return fmt.Errorf("%s - AMQP_URL must be an AMQP URL", location)
	}
	return nil
}

func (c Config) validateAdmin() error {
	const location = "config.Config.validateAdmin"
	if (c.Role == "api" || c.Role == "all") && (strings.TrimSpace(c.AdminUsername) == "" || len(c.AdminPassword) < minAdminPasswordLength) {
		return fmt.Errorf("%s - ADMIN_USERNAME and ADMIN_PASSWORD (at least 16 characters) are required for the admin API", location)
	}
	return nil
}

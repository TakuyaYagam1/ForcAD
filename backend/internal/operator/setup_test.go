package operator

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/config"
	"go.yaml.in/yaml/v3"
)

const basicConfig = `game:
  round_time: 60
  flag_lifetime: 5
  start_time: 2027-01-01 12:00:00
  timezone: UTC
tasks:
  - name: example
    checker: example/checker.py
teams:
  - ip: 192.0.2.10
    name: Example
    token: "0123456789abcdef"
`

const composeConfig = `name: forcad
x-storage-dependencies: &storage-dependencies
  postgres: {condition: service_healthy}
  postgres-data: {condition: service_completed_successfully}
  redis: {condition: service_healthy}
  rabbitmq: {condition: service_healthy}
x-runtime: &runtime
  depends_on:
    <<: *storage-dependencies
    initializer: {condition: service_completed_successfully}
services:
  postgres: {}
  postgres-data: {}
  redis: {}
  rabbitmq: {}
  config-loader: {}
  initializer:
    depends_on:
      postgres: {condition: service_healthy}
      postgres-data: {condition: service_completed_successfully}
      config-loader: {condition: service_completed_successfully}
  api:
    <<: *runtime
  receiver:
    <<: *runtime
  ticker:
    depends_on: [postgres, redis, rabbitmq]
`

func TestSetupWrapsMissingConfigurationCause(t *testing.T) {
	err := Setup(t.TempDir(), SetupOptions{})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Setup error = %v, want os.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), "operator.Setup - read configuration") {
		t.Fatalf("Setup error has no function context: %v", err)
	}
}

func newProject(t *testing.T, document string) string {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	deploymentDir := filepath.Join(root, "deployment", "docker")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deploymentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yml"), []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deploymentDir, "compose.yml"), []byte(composeConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deploymentDir, "compose.tests.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSetupWritesConfigRuntimeAndExternalCompose(t *testing.T) {
	root := newProject(t, basicConfig)
	canonicalPath := filepath.Join(root, "deployment", "docker", "compose.yml")
	canonicalBefore, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Setup(root, SetupOptions{
		Redis:    "cache.example:6380",
		Database: "[2001:db8::4]:5433",
		RabbitMQ: "broker.example",
	})
	if err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(root, "config", "config.yml")
	configInfo := assertMode(t, configPath, 0o600)
	if configInfo.Size() == 0 {
		t.Fatal("generated config is empty")
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	document, err := config.DecodeDocument(strings.NewReader(string(configData)))
	if err != nil {
		t.Fatal(err)
	}
	if document.Admin == nil || document.Admin.Username != "forcad" || len(document.Admin.Password) != 64 {
		t.Fatal("setup did not generate admin credentials")
	}
	if document.Storages == nil || len(document.Storages.DB.Password) != 64 || len(document.Storages.Redis.Password) != 64 || len(document.Storages.RabbitMQ.Password) != 64 {
		t.Fatal("setup did not generate storage credentials")
	}
	if document.Storages.DB.Host != "2001:db8::4" || document.Storages.DB.Port != 5433 || document.Storages.Redis.Host != "cache.example" || document.Storages.Redis.Port != 6380 {
		t.Fatal("external storage addresses were not applied")
	}
	if document.Storages.RabbitMQ.Host != "broker.example" || document.Storages.RabbitMQ.Port != 5672 {
		t.Fatal("external RabbitMQ address was not applied")
	}
	if document.Teams[0].Token != "0123456789abcdef" {
		t.Fatal("setup did not preserve the configured team token")
	}

	runtimeDir := filepath.Join(root, "config", "runtime")
	assertMode(t, runtimeDir, 0o700)
	for _, name := range []string{"admin.env", "postgres_environment.env", "rabbitmq_environment.env", "redis_environment.env"} {
		assertMode(t, filepath.Join(runtimeDir, name), 0o600)
	}
	redisEnv, err := os.ReadFile(filepath.Join(runtimeDir, "redis_environment.env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(redisEnv), "REDIS_HOST=cache.example\nREDIS_PORT=6380\nREDIS_PASSWORD=") || !strings.Contains(string(redisEnv), "REDIS_DB=0") {
		t.Fatal("Redis runtime env is missing expected host, port, or database")
	}

	backups, err := filepath.Glob(filepath.Join(root, "config", "config_backup_*.yml"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one config backup, found %v (error %v)", backups, err)
	}
	assertMode(t, backups[0], 0o600)
	backup, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != basicConfig {
		t.Fatal("config backup does not contain the original document")
	}
	canonicalAfter, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonicalAfter) != string(canonicalBefore) {
		t.Fatal("setup changed the canonical Compose file")
	}

	generatedPath := filepath.Join(runtimeDir, "compose.external.yml")
	assertMode(t, generatedPath, 0o600)
	generated, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(generated), generatedComposeMarker+"\n") {
		t.Fatal("generated Compose file is missing its ownership marker")
	}
	var tree yaml.Node
	if err := yaml.Unmarshal([]byte(strings.TrimPrefix(string(generated), generatedComposeMarker+"\n")), &tree); err != nil {
		t.Fatal(err)
	}
	services, ok := mappingValue(tree.Content[0], "services")
	if !ok || mappingValueExists(services, "redis") || mappingValueExists(services, "postgres") || mappingValueExists(services, "postgres-data") || mappingValueExists(services, "rabbitmq") || !mappingValueExists(services, "api") {
		t.Fatal("external Compose override contains the wrong services")
	}
	dependencies, ok := mappingValue(tree.Content[0], "x-storage-dependencies")
	if !ok || mappingValueExists(dependencies, "redis") || mappingValueExists(dependencies, "postgres") || mappingValueExists(dependencies, "postgres-data") || mappingValueExists(dependencies, "rabbitmq") {
		t.Fatal("external Compose override left removed storage dependencies in an anchor")
	}
	ticker, ok := mappingValue(services, "ticker")
	if !ok {
		t.Fatal("external Compose override removed the ticker")
	}
	tickerDependencies, ok := mappingValue(ticker, "depends_on")
	if !ok || tickerDependencies.Kind != yaml.SequenceNode || len(tickerDependencies.Content) != 0 {
		t.Fatal("external Compose override left removed services in a dependency list")
	}
	initializer, ok := mappingValue(services, "initializer")
	if !ok {
		t.Fatal("external Compose override removed the initializer")
	}
	initializerDependencies, ok := mappingValue(initializer, "depends_on")
	if !ok || mappingValueExists(initializerDependencies, "postgres") || mappingValueExists(initializerDependencies, "postgres-data") || !mappingValueExists(initializerDependencies, "config-loader") {
		t.Fatal("external Compose override left a removed service in initializer dependencies")
	}

	effective, err := EffectiveComposePath(root)
	if err != nil || effective != generatedPath {
		t.Fatalf("unexpected effective Compose path %q: %v", effective, err)
	}
	args, err := ComposeArgs(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if args[len(args)-1] != filepath.Join(root, "deployment", "docker", "compose.tests.yml") {
		t.Fatalf("Compose test override missing from args: %v", args)
	}

	if err := Setup(root, SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(generatedPath); !os.IsNotExist(err) {
		t.Fatalf("setup without external storage did not remove generated Compose: %v", err)
	}
}

func TestSetupRejectsLegacyGlobalConfigsWithoutChangingInput(t *testing.T) {
	legacy := strings.Replace(basicConfig, "game:", "global:", 1)
	ambiguous := `global:
  round_time: 60
  flag_lifetime: 5
  start_time: 2027-01-01 12:00:00
  timezone: UTC
` + basicConfig
	for _, test := range []struct {
		name     string
		document string
	}{
		{name: "legacy global key", document: legacy},
		{name: "global and game keys", document: ambiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := newProject(t, test.document)
			configPath := filepath.Join(root, "config", "config.yml")
			before, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := Setup(root, SetupOptions{}); err == nil {
				t.Fatal("setup accepted a removed global config key")
			}
			after, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("setup changed config after rejecting the global key")
			}
			if _, err := os.Stat(filepath.Join(root, "config", "runtime")); !os.IsNotExist(err) {
				t.Fatal("setup created runtime files after rejecting the global key")
			}
			backups, err := filepath.Glob(filepath.Join(root, "config", "config_backup_*.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if len(backups) != 0 {
				t.Fatal("setup created a config backup after rejecting the global key")
			}
		})
	}
}

func TestSetupRejectsMultipleConfigDocumentsWithoutChangingInput(t *testing.T) {
	document := basicConfig + "---\nextra: document\n"
	root := newProject(t, document)
	configPath := filepath.Join(root, "config", "config.yml")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := Setup(root, SetupOptions{}); err == nil {
		t.Fatal("setup accepted multiple YAML documents")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("setup changed config after rejecting multiple YAML documents")
	}
	if _, err := os.Stat(filepath.Join(root, "config", "runtime")); !os.IsNotExist(err) {
		t.Fatal("setup created runtime files after rejecting multiple YAML documents")
	}
	backups, err := filepath.Glob(filepath.Join(root, "config", "config_backup_*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatal("setup created a config backup after rejecting multiple YAML documents")
	}
}

func TestSetupRejectsUnownedComposeWithoutChangingConfig(t *testing.T) {
	root := newProject(t, basicConfig)
	configPath := filepath.Join(root, "config", "config.yml")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(root, "config", "runtime")
	if err := os.Mkdir(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	generatedPath := filepath.Join(runtimeDir, "compose.external.yml")
	if err := os.WriteFile(generatedPath, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Setup(root, SetupOptions{Redis: "cache"}); err == nil {
		t.Fatal("setup accepted an unowned generated Compose file")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("setup changed config after refusing an unowned Compose file")
	}
}

func TestSetupRejectsInvalidExternalAddressBeforeWriting(t *testing.T) {
	root := newProject(t, basicConfig)
	configPath := filepath.Join(root, "config", "config.yml")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := Setup(root, SetupOptions{Redis: "cache:0"}); err == nil {
		t.Fatal("setup accepted an invalid external port")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("setup changed config after rejecting the address")
	}
	if _, err := os.Stat(filepath.Join(root, "config", "runtime")); !os.IsNotExist(err) {
		t.Fatalf("setup created runtime state before validation completed: %v", err)
	}
}

func TestSetupRejectsInvalidAdminCredentialsBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name  string
		admin string
	}{
		{name: "empty username", admin: "admin:\n  username: \" \"\n  password: \"long-enough-password\"\n"},
		{name: "short password", admin: "admin:\n  username: organizer\n  password: short\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := strings.Replace(basicConfig, "game:", test.admin+"game:", 1)
			root := newProject(t, document)
			if err := Setup(root, SetupOptions{}); err == nil {
				t.Fatal("setup accepted invalid admin credentials")
			}
			configData, err := os.ReadFile(filepath.Join(root, "config", "config.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if string(configData) != document {
				t.Fatal("setup changed config after rejecting admin credentials")
			}
			if _, err := os.Stat(filepath.Join(root, "config", "runtime")); !os.IsNotExist(err) {
				t.Fatalf("setup created runtime state before validation completed: %v", err)
			}
		})
	}
}

func TestSetupRejectsInvalidStorageSettingsBeforeWriting(t *testing.T) {
	base := `admin:
  username: organizer
  password: "ops$#password-1234"
storages:
  db:
    user: dbuser
    password: dbpass
    host: postgres
    port: 5432
    dbname: forcad
  rabbitmq:
    user: rabbituser
    password: rabbitpass
    host: rabbitmq
    port: 5672
    vhost: forcad
  redis:
    password: redispass
    host: redis
    port: 6379
    db: 4
` + basicConfig
	for _, test := range []struct {
		name string
		from string
		to   string
	}{
		{name: "database port", from: "    port: 5432", to: "    port: 0"},
		{name: "database user", from: "    user: dbuser", to: "    user: \" \""},
		{name: "database name", from: "    dbname: forcad", to: "    dbname: \"\""},
		{name: "redis database index", from: "    db: 4", to: "    db: 1025"},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := strings.Replace(base, test.from, test.to, 1)
			root := newProject(t, document)
			if err := Setup(root, SetupOptions{}); err == nil {
				t.Fatal("setup accepted invalid storage settings")
			}
			configData, err := os.ReadFile(filepath.Join(root, "config", "config.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if string(configData) != document {
				t.Fatal("setup changed config after rejecting storage settings")
			}
			if _, err := os.Stat(filepath.Join(root, "config", "runtime")); !os.IsNotExist(err) {
				t.Fatalf("setup created runtime state before validation completed: %v", err)
			}
		})
	}
}

func TestSetupRejectsEnvironmentLineBreaks(t *testing.T) {
	withNewline := strings.Replace(basicConfig, "game:", "admin:\n  username: organizer\n  password: |\n    safe\n    ADMIN_USERNAME=unexpected\ngame:", 1)
	root := newProject(t, withNewline)
	if err := Setup(root, SetupOptions{}); err == nil {
		t.Fatal("setup accepted a line break in an environment value")
	}
	if _, err := os.Stat(filepath.Join(root, "config", "runtime")); !os.IsNotExist(err) {
		t.Fatalf("setup created runtime state before env validation: %v", err)
	}
}

func TestSetupWritesRuntimeEnvironmentValuesRaw(t *testing.T) {
	document := `admin:
  username: organizer
  password: "ops$#password-1234"
storages:
  db:
    user: dbuser
    password: "db$#secret"
    host: postgres
    port: 5432
    dbname: forcad
  rabbitmq:
    user: rabbituser
    password: "rabbit$#secret"
    host: rabbitmq
    port: 5672
    vhost: forcad
  redis:
    password: "redis$#secret"
    host: redis
    port: 6379
    db: 4
` + basicConfig
	root := newProject(t, document)
	if err := Setup(root, SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	for name, values := range map[string][]string{
		"admin.env":                {"ADMIN_PASSWORD=ops$#password-1234"},
		"postgres_environment.env": {"POSTGRES_PASSWORD=db$#secret"},
		"rabbitmq_environment.env": {"RABBITMQ_DEFAULT_PASS=rabbit$#secret"},
		"redis_environment.env":    {"REDIS_PASSWORD=redis$#secret", "REDIS_DB=4"},
	} {
		data, err := os.ReadFile(filepath.Join(root, "config", "runtime", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			if !strings.Contains(string(data), value) {
				t.Errorf("%s does not preserve configured values", name)
			}
		}
	}
}

func mappingValueExists(node *yaml.Node, key string) bool {
	_, ok := mappingValue(node, key)
	return ok
}

func assertMode(t *testing.T, path string, mode os.FileMode) os.FileInfo {
	t.Helper()
	return assertPrivateMode(t, path, mode)
}

package operator

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/TakuyaYagam1/ForcAD/config"
	"go.yaml.in/yaml/v3"
)

// SetupOptions selects storage services managed outside the local Compose project.
type SetupOptions struct {
	Redis    string
	Database string
	RabbitMQ string
}

// Setup validates the project config, creates credentials when omitted, writes
// private runtime files, and prepares the external-storage Compose override.
func Setup(root string, options SetupOptions) error {
	const location = "operator.Setup"
	paths, err := pathsFor(root)
	if err != nil {
		return err
	}
	if _, err := effectiveComposePath(paths); err != nil {
		return err
	}

	configData, err := readRegularFile(paths.config)
	if err != nil {
		return fmt.Errorf("%s - read configuration: %w", location, err)
	}
	document, err := prepareSetupDocument(configData, options)
	if err != nil {
		return fmt.Errorf("%s - prepare configuration: %w", location, err)
	}

	encodedConfig, err := yaml.Marshal(&document)
	if err != nil {
		return fmt.Errorf("%s - encode configuration: %w", location, err)
	}
	files, err := runtimeFiles(paths, document)
	if err != nil {
		return err
	}
	var composeData []byte
	if options.Redis != "" || options.Database != "" || options.RabbitMQ != "" {
		composeData, err = externalCompose(paths, options)
		if err != nil {
			return err
		}
	}

	return saveSetupFiles(paths, encodedConfig, files, composeData)
}

func prepareSetupDocument(configData []byte, options SetupOptions) (config.Document, error) {
	const location = "operator.prepareSetupDocument"
	document, sourceTree, err := decodeSetupDocument(configData)
	if err != nil {
		return config.Document{}, fmt.Errorf("%s - invalid configuration: %w", location, err)
	}
	if err := completeCredentials(&document, sourceTree); err != nil {
		return config.Document{}, err
	}
	if err := applyExternalAddresses(&document, options); err != nil {
		return config.Document{}, err
	}
	if err := document.Validate(); err != nil {
		return config.Document{}, fmt.Errorf("%s - invalid configuration: %w", location, err)
	}

	return document, nil
}

func saveSetupFiles(paths projectPaths, encodedConfig []byte, files []runtimeFile, composeData []byte) error {
	const location = "operator.saveSetupFiles"
	if err := ensureRuntimeDirectory(paths); err != nil {
		return err
	}
	targets := make([]string, 0, len(files)+2)
	targets = append(targets, paths.config)
	for _, file := range files {
		targets = append(targets, file.path)
	}
	if composeData != nil {
		targets = append(targets, paths.generatedCompose)
	}
	for _, target := range targets {
		if err := checkRegularOrMissing(target); err != nil {
			return err
		}
	}
	if _, err := backupConfig(paths.config); err != nil {
		return err
	}
	if err := writePrivateFile(paths.config, encodedConfig); err != nil {
		return fmt.Errorf("%s - write configuration: %w", location, err)
	}
	for _, file := range files {
		if err := writePrivateFile(file.path, file.data); err != nil {
			return fmt.Errorf("%s - write runtime file %s: %w", location, file.path, err)
		}
	}
	if composeData == nil {
		return removeGeneratedCompose(paths)
	}
	if err := writePrivateFile(paths.generatedCompose, composeData); err != nil {
		return fmt.Errorf("%s - write generated Compose file: %w", location, err)
	}
	return nil
}

type runtimeFile struct {
	path      string
	data      []byte
	keyValues []string
}

func decodeSetupDocument(data []byte) (config.Document, *yaml.Node, error) {
	const location = "operator.decodeSetupDocument"
	document, err := config.DecodeDocument(bytes.NewReader(data))
	if err != nil {
		return config.Document{}, nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return config.Document{}, nil, fmt.Errorf("%s - invalid YAML configuration", location)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return config.Document{}, nil, fmt.Errorf("%s - configuration must be a mapping", location)
	}
	return document, root.Content[0], nil
}

func runtimeFiles(paths projectPaths, document config.Document) ([]runtimeFile, error) {
	const location = "operator.runtimeFiles"
	storage := document.Storages
	management := url.URL{Scheme: "http", Host: net.JoinHostPort(storage.RabbitMQ.Host, "15672"), Path: "/api/", User: url.UserPassword(storage.RabbitMQ.User, storage.RabbitMQ.Password)}
	contents := []runtimeFile{
		{path: paths.adminEnv, keyValues: []string{"ADMIN_USERNAME", document.Admin.Username, "ADMIN_PASSWORD", document.Admin.Password}},
		{path: paths.postgresEnv, keyValues: []string{"POSTGRES_HOST", storage.DB.Host, "POSTGRES_PORT", strconv.Itoa(storage.DB.Port), "POSTGRES_USER", storage.DB.User, "POSTGRES_PASSWORD", storage.DB.Password, "POSTGRES_DB", storage.DB.DBName}},
		{path: paths.rabbitmqEnv, keyValues: []string{"RABBITMQ_HOST", storage.RabbitMQ.Host, "RABBITMQ_PORT", strconv.Itoa(storage.RabbitMQ.Port), "RABBITMQ_DEFAULT_USER", storage.RabbitMQ.User, "RABBITMQ_DEFAULT_PASS", storage.RabbitMQ.Password, "RABBITMQ_DEFAULT_VHOST", storage.RabbitMQ.VHost, "BROKER_API_URL", management.String()}},
		{path: paths.redisEnv, keyValues: []string{"REDIS_HOST", storage.Redis.Host, "REDIS_PORT", strconv.Itoa(storage.Redis.Port), "REDIS_PASSWORD", storage.Redis.Password, "REDIS_DB", strconv.Itoa(storage.Redis.DB)}},
	}
	for i := range contents {
		data, err := envFile(contents[i].keyValues...)
		if err != nil {
			return nil, fmt.Errorf("%s - invalid value for runtime file %s: %w", location, contents[i].path, err)
		}
		contents[i].data = data
	}
	return contents, nil
}

func envFile(keyValues ...string) ([]byte, error) {
	const location = "operator.envFile"
	lines := []string{"# This file is managed by ForcAD setup."}
	for i := 0; i < len(keyValues); i += 2 {
		if strings.ContainsAny(keyValues[i+1], "\r\n\x00") {
			return nil, fmt.Errorf("%s - environment values cannot contain line breaks or NUL", location)
		}
		lines = append(lines, keyValues[i]+"="+keyValues[i+1])
	}
	return []byte(strings.Join(lines, "\n")), nil
}

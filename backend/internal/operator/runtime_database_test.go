package operator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/config"
	"go.yaml.in/yaml/v3"
)

func TestRuntimeDatabaseCredentialsAndSanitizedGame(t *testing.T) {
	document, err := prepareSetupDocument([]byte(basicConfig), SetupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	db := document.Storages.DB
	if db.Runtime == nil || db.Runtime.Username == db.User || db.Runtime.Password == db.Password {
		t.Fatal("runtime credentials must be distinct")
	}
	encoded, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := prepareSetupDocument(encoded, SetupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if *repeated.Storages.DB.Runtime != *db.Runtime || repeated.Storages.DB.Password != db.Password {
		t.Fatal("setup rotated persisted credentials")
	}
	paths, err := pathsFor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files, err := runtimeFiles(paths, document)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		switch file.path {
		case paths.postgresRuntimeEnv:
			if strings.Contains(string(file.data), db.Password) || !strings.Contains(string(file.data), db.Runtime.Password) {
				t.Fatal("runtime env includes bootstrap credential or omits runtime credential")
			}
		case paths.gameConfig:
			game, err := config.DecodeDocument(strings.NewReader(string(file.data)))
			if err != nil {
				t.Fatal(err)
			}
			if game.Admin != nil || game.Storages != nil || len(game.Teams) != len(document.Teams) {
				t.Fatal("shared game document must preserve gameplay without credentials")
			}
		}
	}
}

func TestComposeRuntimeHasNoBootstrapCredentialMount(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "deployment", "docker", "compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			EnvFile []struct {
				Path string `yaml:"path"`
			} `yaml:"env_file"`
			Volumes []string `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"api", "receiver", "worker", "ticker", "pgbouncer", "config-loader"} {
		service, ok := compose.Services[name]
		if !ok {
			t.Fatalf("missing service %s", name)
		}
		for _, file := range service.EnvFile {
			if strings.HasSuffix(file.Path, "postgres_environment.env") {
				t.Fatalf("%s receives bootstrap environment", name)
			}
		}
		for _, mount := range service.Volumes {
			if strings.Contains(mount, "config/config.yml:") || strings.Contains(mount, "config/runtime:") {
				t.Fatalf("%s mounts privileged config/runtime directory", name)
			}
		}
	}
}

func TestRuntimeDatabaseRejectsSharedCredential(t *testing.T) {
	document, err := prepareSetupDocument([]byte(basicConfig), SetupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	document.Storages.DB.Runtime.Username = document.Storages.DB.User
	if err := validateCompletedCredentials(document); err == nil {
		t.Fatal("accepted bootstrap identity as runtime identity")
	}
}

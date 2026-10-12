package bootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/TakuyaYagam1/ForcAD/config"
)

func TestTrafficConfigurationMustExist(t *testing.T) {
	cfg := config.Config{ConfigPath: filepath.Join(t.TempDir(), "missing.yml")}
	_, err := newTeamTraffic(cfg, nil, nil, nil, nil, nil)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing configuration was ignored: %v", err)
	}
}

//go:build !windows

package operator

import (
	"fmt"

	"errors"
	"os"
)

func ensurePrivateDirectory(path string) error {
	const location = "operator.ensurePrivateDirectory"

	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s - %w", location, err)
	}
	// #nosec G302 -- Its owner needs traversal permission on this private directory.
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("%s - protect private directory: %w", location, err)
	}
	return nil
}

func createPrivateFile(path string) (*os.File, error) {
	const location = "operator.createPrivateFile"

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%s - %w", location, err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("%s - %w", location, err)
	}
	return file, nil
}

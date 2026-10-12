package operator

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func readRegularFile(path string) ([]byte, error) {
	const location = "operator.readRegularFile"
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("%s - inspect file %s: %w", location, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s - not a regular file: %s", location, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s - open file %s: %w", location, path, err)
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("%s - inspect opened file %s: %w", location, path, err)
	}
	currentInfo, err := os.Lstat(path)
	if err != nil || currentInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, openedInfo) || !os.SameFile(openedInfo, currentInfo) {
		return nil, fmt.Errorf("%s - file changed while opening: %s", location, path)
	}
	const maxFileSize = 16 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("%s - read file %s: %w", location, path, err)
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("%s - file exceeds %d bytes: %s", location, maxFileSize, path)
	}
	return data, nil
}

func ensureRuntimeDirectory(paths projectPaths) error {
	const location = "operator.ensureRuntimeDirectory"
	configInfo, err := os.Lstat(paths.configDir)
	if err != nil {
		return fmt.Errorf("%s - inspect config directory: %w", location, err)
	}
	if configInfo.Mode()&os.ModeSymlink != 0 || !configInfo.IsDir() {
		return fmt.Errorf("%s - config path is not a regular directory: %s", location, paths.configDir)
	}
	info, err := os.Lstat(paths.runtime)
	if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return fmt.Errorf("%s - runtime path is not a directory: %s", location, paths.runtime)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s - inspect runtime directory: %w", location, err)
	}
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(paths.runtime, 0o700); err != nil {
			return fmt.Errorf("%s - create runtime directory: %w", location, err)
		}
	}
	// #nosec G302 -- This is a private directory; its owner needs traversal permission.
	if err := os.Chmod(paths.runtime, 0o700); err != nil {
		return fmt.Errorf("%s - secure runtime directory: %w", location, err)
	}
	return nil
}

func backupConfig(path string) (string, error) {
	const location = "operator.backupConfig"
	data, err := readRegularFile(path)
	if err != nil {
		return "", fmt.Errorf("%s - read config backup source: %w", location, err)
	}
	directory := filepath.Dir(path)
	base := "config_backup_" + strconv.FormatInt(time.Now().Unix(), 10)
	for suffix := 0; suffix < 1000; suffix++ {
		name := base + ".yml"
		if suffix != 0 {
			name = base + "_" + strconv.Itoa(suffix) + ".yml"
		}
		backup := filepath.Join(directory, name)
		file, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("%s - create config backup: %w", location, err)
		}
		if err := file.Chmod(0o600); err != nil {
			_ = file.Close()
			_ = os.Remove(backup)
			return "", fmt.Errorf("%s - secure config backup: %w", location, err)
		}
		written, err := file.Write(data)
		if err != nil || written != len(data) {
			_ = file.Close()
			_ = os.Remove(backup)
			if err == nil {
				err = io.ErrShortWrite
			}
			return "", fmt.Errorf("%s - write config backup: %w", location, err)
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			_ = os.Remove(backup)
			return "", fmt.Errorf("%s - sync config backup: %w", location, err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(backup)
			return "", fmt.Errorf("%s - close config backup: %w", location, err)
		}
		return backup, nil
	}
	return "", fmt.Errorf("%s - could not find an unused config backup name", location)
}

func writePrivateFile(path string, data []byte) error {
	const location = "operator.writePrivateFile"
	if err := checkRegularOrMissing(path); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".forcad-setup-*")
	if err != nil {
		return fmt.Errorf("%s - create temporary file: %w", location, err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%s - secure temporary file: %w", location, err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%s - write temporary file: %w", location, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%s - sync temporary file: %w", location, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("%s - close temporary file: %w", location, err)
	}
	if err := checkRegularOrMissing(path); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("%s - replace file: %w", location, err)
	}
	return nil
}

func checkRegularOrMissing(path string) error {
	const location = "operator.checkRegularOrMissing"
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s - inspect path %s: %w", location, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s - refusing to write through a non-regular path: %s", location, path)
	}
	return nil
}

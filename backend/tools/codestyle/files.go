package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func goFiles(paths []string) ([]string, error) {
	const location = "codestyle.goFiles"
	files := make(map[string]bool)
	for _, path := range paths {
		if err := collectGoFiles(path, files); err != nil {
			return nil, fmt.Errorf("%s - collect Go files in %s: %w", location, path, err)
		}
	}
	result := make([]string, 0, len(files))
	for path := range files {
		result = append(result, path)
	}
	slices.Sort(result)
	return result, nil
}

func collectGoFiles(path string, files map[string]bool) error {
	const location = "codestyle.collectGoFiles"
	err := filepath.WalkDir(path, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if name == path {
				return fmt.Errorf("%s - refusing a symlink path: %s", location, name)
			}
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".cache", ".codex", ".agents", "docker_volumes", "node_modules", "vendor", "reports":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if strings.HasSuffix(name, ".go") {
			absolute, err := filepath.Abs(name)
			if err != nil {
				return fmt.Errorf("%s - resolve source path: %w", location, err)
			}
			files[absolute] = true
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s - walk source tree: %w", location, err)
	}
	return nil
}

func replaceFile(path string, data []byte) error {
	const location = "codestyle.replaceFile"
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%s - inspect source file: %w", location, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s - refusing a non-regular Go source: %s", location, path)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".codestyle-*")
	if err != nil {
		return fmt.Errorf("%s - create replacement file: %w", location, err)
	}
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}()
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("%s - preserve source permissions: %w", location, err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("%s - write replacement source: %w", location, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("%s - close replacement source: %w", location, err)
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("%s - replace source file: %w", location, err)
	}
	return nil
}

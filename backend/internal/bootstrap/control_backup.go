package bootstrap

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/TakuyaYagam1/ForcAD/config"
	"github.com/TakuyaYagam1/ForcAD/internal/backup"
	"github.com/TakuyaYagam1/ForcAD/internal/operator"
)

const teamLogosDirectory = "/run/forcad-team-logos"

func controlBackup(
	ctx context.Context,
	root, command string,
	args []string,
	out io.Writer,
) error {
	const location = "bootstrap.controlBackup"
	compose, err := operator.ComposeArgs(root, os.Getenv("TEST") != "")
	if err != nil {
		return err
	}
	compose = append(compose, "exec", "-T", "api", "forcad", command)
	if command == "restore-backup" {
		if len(args) != 2 || args[0] != "--confirm" {
			return fmt.Errorf("%s - usage: ./control.sh restore-backup --confirm ARCHIVE.tar.gz", location)
		}
		file, err := os.Open(filepath.Clean(args[1]))
		if err != nil {
			return fmt.Errorf("%s - open archive: %w", location, err)
		}
		defer func() {
			_ = file.Close()
		}()
		return execOperatorCommand(ctx, compose[0], compose[1:], nil, file, out, os.Stderr)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("name", "event", "event name used in the archive")
	output := flags.String("output", "", "output .tar.gz file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !backup.ValidName(*name) {
		return fmt.Errorf("%s - usage: ./control.sh backup [--name EVENT] [--output ARCHIVE.tar.gz]", location)
	}
	path := *output
	if path == "" {
		path = (backup.Manifest{
			Name:      *name,
			CreatedAt: time.Now(),
		}).Filename()
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".forcad-backup-")
	if err != nil {
		return fmt.Errorf("%s - stage archive: %w", location, err)
	}
	defer func() {
		_ = os.Remove(file.Name())
	}()
	compose = append(compose, *name)
	runErr := execOperatorCommand(ctx, compose[0], compose[1:], nil, nil, file, os.Stderr)
	if err := errors.Join(runErr, file.Close()); err != nil {
		return fmt.Errorf("%s - write archive: %w", location, err)
	}
	// Link publishes the completed private file without overwriting a backup.
	if err := os.Link(file.Name(), path); err != nil {
		return fmt.Errorf("%s - publish archive: %w", location, err)
	}
	_, err = fmt.Fprintln(out, path)
	return err
}

// RunBackupCommand is the container-side transport for control.sh.
func RunBackupCommand(
	ctx context.Context,
	command string,
	args []string,
	in io.Reader,
	out io.Writer,
) error {
	const location = "bootstrap.RunBackupCommand"
	cfg, err := config.New()
	if err != nil {
		return err
	}
	pool, closePool, err := newPostgresPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer closePool()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	store := backup.New(pool, teamLogosDirectory)
	if command == "backup" && len(args) == 1 {
		_, err := store.Export(ctx, out, args[0])
		return err
	}
	if command == "restore-backup" && len(args) == 0 {
		manifest, err := store.Restore(ctx, in)
		if errors.Is(err, backup.ErrInvalid) {
			return backup.ErrInvalid
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "Restored %s at round %d\n", manifest.Name, manifest.Round)
		return err
	}
	return fmt.Errorf("%s - use ./control.sh backup or ./control.sh restore-backup", location)
}

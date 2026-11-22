package backup

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

const logoPrefix = "/api/v1/client/team-logos/"
const localLogoPrefix = "/team-logos/"

func logoName(path string) string {
	if name, ok := strings.CutPrefix(path, logoPrefix); ok {
		return name
	}
	return strings.TrimPrefix(path, localLogoPrefix)
}

func selectLogos(ctx context.Context, tx pgx.Tx) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT logo_path FROM teams
WHERE starts_with(logo_path, $1) OR starts_with(logo_path, $2) ORDER BY logo_path`, localLogoPrefix, logoPrefix)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (s *Store) exportLogos(ctx context.Context, tx pgx.Tx, b *bundle) error {
	const location = "backup.Store.exportLogos"
	paths, err := selectLogos(ctx, tx)
	if err != nil {
		return fmt.Errorf("%s - read team logo paths: %w", location, err)
	}
	if len(paths) > maxLogos {
		return fmt.Errorf("%s - too many team logos: %w", location, ErrInvalid)
	}
	for _, path := range paths {
		name := logoName(path)
		if !logoPattern.MatchString(name) {
			return fmt.Errorf("%s - unsupported team logo path: %w", location, ErrInvalid)
		}
		nameInArchive := "team-logos/" + name
		if b.files[nameInArchive] != "" {
			return fmt.Errorf("%s - duplicate team logo name: %w", location, ErrInvalid)
		}
		content, err := s.readLogo(ctx, tx, path, name)
		if err != nil {
			return fmt.Errorf("%s - read team logo %s: %w", location, name, err)
		}
		if _, err := logoContentType(name, content); err != nil {
			return err
		}
		if err := b.add(nameInArchive, func(w io.Writer) error {
			_, err := w.Write(content)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) readLogo(
	ctx context.Context,
	tx pgx.Tx,
	path, name string,
) ([]byte, error) {
	const location = "backup.Store.readLogo"
	if strings.HasPrefix(path, logoPrefix) {
		var content []byte
		err := tx.QueryRow(ctx, "SELECT content FROM backup_team_logos WHERE name = $1", name).Scan(&content)
		return content, err
	}
	root, err := os.OpenRoot(s.logoDir)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = root.Close()
	}()
	info, err := root.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxLogoBytes {
		return nil, fmt.Errorf("%s - unsupported team logo file: %w", location, ErrInvalid)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()
	return io.ReadAll(io.LimitReader(file, maxLogoBytes+1))
}

func importLogos(ctx context.Context, tx pgx.Tx, b *bundle) error {
	const location = "backup.importLogos"
	paths, err := selectLogos(ctx, tx)
	if err != nil {
		return err
	}
	used := make(map[string]bool, len(paths))
	for _, path := range paths {
		name := logoName(path)
		file := b.files["team-logos/"+name]
		if file == "" {
			return fmt.Errorf("%s - referenced team logo is missing: %w", location, ErrInvalid)
		}
		if !used[name] {
			content, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			contentType, err := logoContentType(name, content)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO backup_team_logos(name, content_type, content) VALUES ($1, $2, $3)", name, contentType, content); err != nil {
				return err
			}
			used[name] = true
		}
		if path != logoPrefix+name {
			if _, err := tx.Exec(ctx, "UPDATE teams SET logo_path = $1 WHERE logo_path = $2", logoPrefix+name, path); err != nil {
				return err
			}
		}
	}
	for name := range b.files {
		if logo, ok := strings.CutPrefix(name, "team-logos/"); ok && !used[logo] {
			return fmt.Errorf("%s - unreferenced team logo: %w", location, ErrInvalid)
		}
	}
	return nil
}

func logoContentType(name string, data []byte) (string, error) {
	const location = "backup.logoContentType"
	if len(data) == 0 || len(data) > maxLogoBytes {
		return "", fmt.Errorf("%s - invalid team logo size: %w", location, ErrInvalid)
	}
	var want string
	switch filepath.Ext(name) {
	case ".png":
		want = "image/png"
	case ".jpg", ".jpeg":
		want = "image/jpeg"
	case ".gif":
		want = "image/gif"
	case ".webp":
		want = "image/webp"
	case ".svg":
		decoder := xml.NewDecoder(bytes.NewReader(data))
		for {
			tok, err := decoder.Token()
			if err != nil {
				break
			}
			if root, ok := tok.(xml.StartElement); ok {
				if root.Name.Local == "svg" && root.Name.Space == "http://www.w3.org/2000/svg" {
					return "image/svg+xml", nil
				}
				break
			}
		}
	}
	if want == "" || http.DetectContentType(data) != want {
		return "", fmt.Errorf("%s - unsupported team logo type: %w", location, ErrInvalid)
	}
	return want, nil
}

func (s *Store) Logo(ctx context.Context, name string) (string, []byte, error) {
	if !logoPattern.MatchString(name) {
		return "", nil, fs.ErrNotExist
	}
	var contentType string
	var content []byte
	err := s.pool.QueryRow(ctx, "SELECT content_type, content FROM backup_team_logos WHERE name = $1", name).Scan(&contentType, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, fs.ErrNotExist
	}
	return contentType, content, err
}

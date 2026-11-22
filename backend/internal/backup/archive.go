package backup

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	formatVersion    = 1
	MaxUploadBytes   = 256 << 20
	maxContentBytes  = 2 << 30
	maxManifestBytes = 1 << 20
	maxLogoBytes     = 4 << 20
	maxLogos         = 1024
	maxRowBytes      = 64 << 20
	manifestName     = "manifest.json"
)

var (
	ErrInvalid  = errors.New("invalid or incompatible backup")
	ErrSource   = errors.New("backup requires a finished game with no pending results")
	ErrTarget   = errors.New("restore requires a board that has not hosted a game and has no announcements")
	ErrBusy     = errors.New("another backup operation is in progress")
	namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	logoPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,190}\.(svg|png|jpg|jpeg|webp|gif)$`)
)

type Manifest struct {
	Version   int              `json:"format_version"`
	ForcAD    string           `json:"forcad_version"`
	Name      string           `json:"name"`
	CreatedAt time.Time        `json:"created_at"`
	Round     int32            `json:"round"`
	Files     map[string]entry `json:"files"`
}

type entry struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func (m Manifest) Filename() string {
	return fmt.Sprintf("forcad-%s-%s.tar.gz", m.Name, m.CreatedAt.UTC().Format("20060102T150405Z"))
}

func ValidName(name string) bool {
	return namePattern.MatchString(name)
}

type bundle struct {
	dir      string
	manifest Manifest
	files    map[string]string
	total    int64
}

func newBundle() (*bundle, error) {
	const location = "backup.newBundle"
	dir, err := os.MkdirTemp("", "forcad-backup-")
	if err != nil {
		return nil, fmt.Errorf("%s - create backup workspace: %w", location, err)
	}
	return &bundle{
		dir:   dir,
		files: make(map[string]string),
		manifest: Manifest{
			Files: make(map[string]entry),
		},
	}, nil
}

func (b *bundle) close() {
	_ = os.RemoveAll(b.dir)
}

func (b *bundle) add(name string, write func(io.Writer) error) error {
	const location = "backup.bundle.add"
	file, err := os.CreateTemp(b.dir, "entry-")
	if err != nil {
		return fmt.Errorf("%s - create staged backup entry: %w", location, err)
	}
	hash := sha256.New()
	limit := maxContentBytes - b.total
	if strings.HasPrefix(name, "team-logos/") {
		limit = min(limit, maxLogoBytes)
	} else if name == manifestName {
		limit = min(limit, maxManifestBytes)
	}
	writer := &boundedWriter{
		writer: io.MultiWriter(file, hash),
		left:   limit,
	}
	if err := errors.Join(write(writer), file.Close()); err != nil {
		return fmt.Errorf("%s - stage %s: %w", location, name, err)
	}
	size := limit - writer.left
	b.total += size
	b.manifest.Files[name] = entry{size, hex.EncodeToString(hash.Sum(nil))}
	b.files[name] = file.Name()
	return nil
}

type boundedWriter struct {
	writer io.Writer
	left   int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	const location = "backup.boundedWriter.Write"
	if int64(len(p)) > w.left {
		return 0, fmt.Errorf("%s - size limit exceeded: %w", location, ErrInvalid)
	}
	n, err := w.writer.Write(p)
	w.left -= int64(n)
	return n, err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func readArchive(ctx context.Context, input io.Reader) (_ *bundle, resultErr error) {
	const location = "backup.readArchive"
	b, err := newBundle()
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			b.close()
		}
	}()
	limited := &io.LimitedReader{
		R: contextReader{ctx, input},
		N: MaxUploadBytes + 1,
	}
	compressed := bufio.NewReader(limited)
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("%s - expected gzip: %w", location, ErrInvalid)
	}
	defer func() {
		_ = gz.Close()
	}()
	gz.Multistream(false)
	// Bound tar framing as well as entry payloads, including extended tar headers.
	plain := &io.LimitedReader{
		R: gz,
		N: maxContentBytes + (8 << 20),
	}
	if err := b.readMembers(tar.NewReader(plain)); err != nil {
		return nil, err
	}
	// Read the gzip footer and reject concatenated members or trailing data.
	var tail [1]byte
	if n, err := io.ReadFull(plain, tail[:]); n != 0 || !errors.Is(err, io.EOF) || plain.N == 0 {
		return nil, fmt.Errorf("%s - invalid gzip footer: %w", location, ErrInvalid)
	}
	if _, err := compressed.Peek(1); !errors.Is(err, io.EOF) || limited.N == 0 {
		return nil, fmt.Errorf("%s - trailing or oversized gzip data: %w", location, ErrInvalid)
	}
	if err := b.checkManifest(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *bundle) readMembers(reader *tar.Reader) error {
	const location = "backup.bundle.readMembers"
	logos := 0
	for {
		h, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s - read tar header: %w", location, ErrInvalid)
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 || len(h.PAXRecords) != 0 || !allowedEntry(h.Name) || b.files[h.Name] != "" {
			return fmt.Errorf("%s - unexpected tar member: %w", location, ErrInvalid)
		}
		if strings.HasPrefix(h.Name, "team-logos/") {
			logos++
		}
		if logos > maxLogos || h.Size > maxContentBytes-b.total {
			return fmt.Errorf("%s - archive is too large: %w", location, ErrInvalid)
		}
		if err := b.add(h.Name, func(w io.Writer) error {
			if _, err := io.CopyN(w, reader, h.Size); err != nil {
				return fmt.Errorf("%s - read tar member: %w: %w", location, ErrInvalid, err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
}

func allowedEntry(name string) bool {
	if name == manifestName {
		return true
	}
	if logo, ok := strings.CutPrefix(name, "team-logos/"); ok {
		return logoPattern.MatchString(logo)
	}
	for _, table := range tables {
		if name == table.member() {
			return true
		}
	}
	return false
}

func (b *bundle) checkManifest() error {
	const location = "backup.bundle.checkManifest"
	if b.files[manifestName] == "" {
		return fmt.Errorf("%s - no manifest: %w", location, ErrInvalid)
	}
	data, err := os.ReadFile(b.files[manifestName])
	if err != nil {
		return fmt.Errorf("%s - read staged manifest: %w", location, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil || m.Version != formatVersion || m.ForcAD == "" ||
		!ValidName(m.Name) || m.CreatedAt.IsZero() || m.Round < 1 || len(m.Files) != len(b.files)-1 {
		return fmt.Errorf("%s - incompatible manifest: %w", location, ErrInvalid)
	}
	delete(b.manifest.Files, manifestName)
	for name, actual := range b.manifest.Files {
		if expected, ok := m.Files[name]; !ok || expected != actual {
			return fmt.Errorf("%s - checksum or size mismatch: %w", location, ErrInvalid)
		}
	}
	for _, table := range tables {
		if b.files[table.member()] == "" {
			return fmt.Errorf("%s - missing %s: %w", location, table.member(), ErrInvalid)
		}
	}
	b.manifest = m
	return nil
}

func (b *bundle) writeArchive(ctx context.Context, output io.Writer) (resultErr error) {
	const location = "backup.bundle.writeArchive"
	gz := gzip.NewWriter(&boundedWriter{
		writer: output,
		left:   MaxUploadBytes,
	})
	defer func() {
		resultErr = errors.Join(resultErr, gz.Close())
	}()
	tw := tar.NewWriter(gz)
	defer func() {
		resultErr = errors.Join(resultErr, tw.Close())
	}()
	data, err := json.Marshal(b.manifest)
	if err != nil {
		return fmt.Errorf("%s - encode manifest: %w", location, err)
	}
	if len(data) > maxManifestBytes {
		return fmt.Errorf("%s - manifest is too large: %w", location, ErrInvalid)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:   manifestName,
		Size:   int64(len(data)),
		Mode:   0o600,
		Format: tar.FormatUSTAR,
	}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	for name, item := range b.manifest.Files {
		if err := tw.WriteHeader(&tar.Header{
			Name:   name,
			Size:   item.Size,
			Mode:   0o600,
			Format: tar.FormatGNU,
		}); err != nil {
			return err
		}
		if err := copyEntry(ctx, tw, b.files[name]); err != nil {
			return err
		}
	}
	return nil
}

func copyEntry(ctx context.Context, w io.Writer, path string) error {
	const location = "backup.copyEntry"
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("%s - open staged backup entry: %w", location, err)
	}
	_, copyErr := io.Copy(w, contextReader{ctx, file})
	return errors.Join(copyErr, file.Close())
}

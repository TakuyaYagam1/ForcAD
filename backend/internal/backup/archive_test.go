package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"testing"
)

func TestReadRejectsUnexpectedEntries(t *testing.T) {
	for _, name := range []string{"../flags.ndjson", "data/unexpected.ndjson", "team-logos/../../logo.png"} {
		t.Run(name, func(t *testing.T) {
			var archive bytes.Buffer
			gz := gzip.NewWriter(&archive)
			tw := tar.NewWriter(gz)
			if err := tw.WriteHeader(&tar.Header{
				Name:     name,
				Mode:     0o600,
				Typeflag: tar.TypeReg,
			}); err != nil {
				t.Fatal(err)
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := readArchive(t.Context(), &archive); !errors.Is(err, ErrInvalid) {
				t.Fatalf("readArchive = %v, want invalid archive", err)
			}
		})
	}
}

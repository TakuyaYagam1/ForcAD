package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
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

func TestManifestRequiresNonnegativeRound(t *testing.T) {
	for _, tc := range []struct {
		name  string
		round json.RawMessage
		valid bool
	}{
		{"waiting", json.RawMessage("0"), true},
		{"played", json.RawMessage("12"), true},
		{"missing", nil, false},
		{"null", json.RawMessage("null"), false},
		{"negative", json.RawMessage("-1"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := newBundle()
			if err != nil {
				t.Fatal(err)
			}
			defer b.close()
			for _, table := range tables {
				if err := b.add(table.member(), func(io.Writer) error {
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			manifest := map[string]any{
				"format_version": formatVersion,
				"forcad_version": "test",
				"name":           "event",
				"created_at":     time.Now().UTC(),
				"files":          b.manifest.Files,
			}
			if tc.round != nil {
				manifest["round"] = tc.round
			}
			if err := b.add(manifestName, func(w io.Writer) error {
				return json.NewEncoder(w).Encode(manifest)
			}); err != nil {
				t.Fatal(err)
			}
			err = b.checkManifest()
			if tc.valid && err != nil || !tc.valid && !errors.Is(err, ErrInvalid) {
				t.Fatalf("checkManifest: %v", err)
			}
		})
	}
}

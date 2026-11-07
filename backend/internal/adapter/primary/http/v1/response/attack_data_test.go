package response

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"

	"github.com/go-faster/jx"
)

func TestAttackDataMatchesJSON(t *testing.T) {
	cases := []map[string]map[string][]string{
		nil,
		{},
		{
			"z-service": {
				"z-team": nil,
				"a-team": {},
			},
			"a-service": nil,
		},
		{
			"<service>&\"": {
				"<team>&\"": {"<hint>&\"", "\u2028\u2029", "\xff\xfe", "\x00\n\r\t"},
			},
		},
		{
			"service\b": {
				"team": {"hint"},
			},
		},
		{
			"service": {
				"team\f": {"hint"},
			},
		},
		{
			"service": {
				"team": {"\b\f", `\u0008`, `\u000c`},
			},
		},
	}
	e := jx.GetEncoder()
	defer jx.PutEncoder(e)
	for i, data := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			assertAttackDataJSON(t, e, data)
		})
	}
}

func FuzzAttackDataMatchesJSON(f *testing.F) {
	f.Add("service", "team", "hint")
	f.Add("<>&\"", "\xff\u2028", "\b\f\u2029")
	f.Fuzz(func(
		t *testing.T,
		service, team, hint string,
	) {
		if len(service)+len(team)+len(hint) > 1<<16 {
			t.Skip()
		}
		e := jx.GetEncoder()
		defer jx.PutEncoder(e)
		assertAttackDataJSON(t, e, map[string]map[string][]string{
			service: {
				team: {hint, "", hint},
			},
			"other-service": {
				"empty": {},
				"null":  nil,
			},
		})
	})
}

func assertAttackDataJSON(t *testing.T, e *jx.Encoder, data map[string]map[string][]string) {
	t.Helper()
	reference := data
	if reference == nil {
		reference = map[string]map[string][]string{}
	}
	want, err := json.Marshal(reference)
	if err != nil {
		t.Fatal(err)
	}
	if err := AttackData(e, data); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("encoded %q, want %q", e.Bytes(), want)
	}
}

func BenchmarkAttackDataEncoding(b *testing.B) {
	data := make(map[string]map[string][]string)
	for service, puts := range map[string]int{
		"notes": 14,
		"vault": 13,
		"queue": 13,
	} {
		teams := make(map[string][]string)
		for team := 1; team <= 28; team++ {
			hints := make([]string, puts*6)
			for i := range hints {
				hints[i] = fmt.Sprintf("records/%032x", i)
			}
			teams[fmt.Sprintf("192.0.2.%d", team)] = hints
		}
		data[service] = teams
	}
	b.Run("encoding-json-and-etag", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			body, err := json.Marshal(data)
			if err != nil {
				b.Fatal(err)
			}
			runtime.KeepAlive(sha256.Sum256(body))
		}
	})
	b.Run("pooled-jx-and-etag", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			e := jx.GetEncoder()
			if err := AttackData(e, data); err != nil {
				b.Fatal(err)
			}
			runtime.KeepAlive(sha256.Sum256(e.Bytes()))
			jx.PutEncoder(e)
		}
	})
}

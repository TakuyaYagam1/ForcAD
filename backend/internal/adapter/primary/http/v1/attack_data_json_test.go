package v1

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAttackDataJSONBytesAndETag(t *testing.T) {
	game := &attackDataGame{data: map[string]map[string][]string{
		"z-service": {
			"z-ip": nil,
			"a-ip": {`<flag>&"`},
		},
		"a-service": nil,
	}}
	response := httptest.NewRecorder()
	if err := writeAttackData(t.Context(), game, nil, "", response); err != nil {
		t.Fatal(err)
	}

	const wantBody = `{"a-service":null,"z-service":{"a-ip":["\u003cflag\u003e\u0026\""],"z-ip":null}}`
	if response.Code != http.StatusOK || response.Body.String() != wantBody {
		t.Fatalf("response status=%d body=%s, want %s", response.Code, response.Body.String(), wantBody)
	}
	if got, want := response.Header().Get("ETag"), `"d1920174d70bc63d040639f63cf82d45fbc2cf3e4bcb5367f4b1b7954a73ad44"`; got != want {
		t.Fatalf("ETag = %q, want %q", got, want)
	}
}

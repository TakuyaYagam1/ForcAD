package checker_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	processchecker "github.com/TakuyaYagam1/ForcAD/internal/adapter/secondary/checker"
)

func TestExampleServiceFailure(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply exampleReply
		want  processchecker.Status
	}{
		{"invalid JSON", exampleReply{status: 201, body: "not JSON"}, processchecker.StatusMumble},
		{"wrong JSON shape", exampleReply{status: 201, body: "[]"}, processchecker.StatusMumble},
		{"wrong note ID", exampleReply{status: 201, body: `{"id":"wrong"}`}, processchecker.StatusMumble},
		{"HTML", exampleReply{status: 201, body: "<html></html>", contentType: "text/html"}, processchecker.StatusMumble},
		{"large response", exampleReply{status: 201, body: strings.Repeat("x", 65537)}, processchecker.StatusMumble},
		{"redirect", exampleReply{status: 302}, processchecker.StatusMumble},
		{"bad request", exampleReply{status: 400}, processchecker.StatusMumble},
		{"overload", exampleReply{status: 429}, processchecker.StatusDown},
		{"server error", exampleReply{status: 503}, processchecker.StatusDown},
		{"timeout", exampleReply{status: 201, delay: 2500 * time.Millisecond}, processchecker.StatusDown},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, host := newExampleService(t)
			test.reply.method = http.MethodPut
			service.setReply(test.reply)
			result := runExample(t, "hackerdom", host, processchecker.ActionPut, exampleIdentifier, 1)
			requireExampleStatus(t, result, test.want)
			if !strings.Contains(string(result.Stderr), " - ") {
				t.Fatal("diagnostic must identify the failure location")
			}
			if _, calls := service.counts(); calls != 1 {
				t.Fatal("failed request was retried or redirect was followed")
			}
		})
	}
}

func TestExampleFlagFailure(t *testing.T) {
	for _, mode := range []string{"hackerdom", "pfr"} {
		t.Run(mode, func(t *testing.T) {
			service, host := newExampleService(t)
			put := runExample(t, mode, host, processchecker.ActionPut, exampleIdentifier, 2)
			requireExampleStatus(t, put, processchecker.StatusUp)
			private, state := examplePrivateData(t, mode, put)
			service.changeStoredNote()
			corrupt := runExample(t, mode, host, processchecker.ActionGet, private, 2)
			requireExampleStatus(t, corrupt, processchecker.StatusCorrupt)
			if strings.Contains(string(corrupt.Stdout)+string(corrupt.Stderr), state.Token) {
				t.Fatal("failed GET exposed credentials")
			}
			for _, status := range []int{401, 403, 404, 410} {
				service.setReply(exampleReply{method: http.MethodGet, status: status})
				requireExampleStatus(t, runExample(t, mode, host, processchecker.ActionGet, private, 2), processchecker.StatusCorrupt)
			}
			service.setReply(exampleReply{method: http.MethodGet, status: 200, body: `{"body":null}`})
			requireExampleStatus(t, runExample(t, mode, host, processchecker.ActionGet, private, 2), processchecker.StatusMumble)
		})
	}
}

func TestExamplePutRequiresReadback(t *testing.T) {
	service, host := newExampleService(t)
	service.changeStoredNote()
	requireExampleStatus(t, runExample(t, "hackerdom", host, processchecker.ActionPut, exampleIdentifier, 1), processchecker.StatusMumble)
	requireExampleStatus(t, runExample(t, "hackerdom", host, processchecker.ActionCheck, "", 0), processchecker.StatusMumble)
}

func TestExampleInvalidPrivateData(t *testing.T) {
	service, host := newExampleService(t)
	for _, private := range []string{"broken", "null", "[]", "{}", strings.Repeat("x", 4097)} {
		requireExampleStatus(t, runExample(t, "hackerdom", host, processchecker.ActionGet, private, 1), processchecker.StatusCheckFailed)
	}
	state := exampleState{Version: 1, ID: exampleIdentifier, Token: strings.Repeat("a", 32), Place: 1}
	private, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	requireExampleStatus(t, runExample(t, "hackerdom", host, processchecker.ActionGet, string(private), 2), processchecker.StatusCheckFailed)
	requireExampleStatus(t, runExample(t, "hackerdom", host, processchecker.ActionPut, exampleIdentifier, 3), processchecker.StatusCheckFailed)
	if _, calls := service.counts(); calls != 0 {
		t.Fatal("invalid checker input reached the service")
	}
}

func TestExampleConnectionFailure(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	host := strings.TrimPrefix(server.URL, "http://")
	server.Close()
	requireExampleStatus(t, runExample(t, "hackerdom", host, processchecker.ActionCheck, "", 0), processchecker.StatusDown)
}

func TestExampleUnexpectedErrorIsPrivate(t *testing.T) {
	const source = `import checker
import sys

private_flag = sys.argv[1]

def broken_check(self):
    raise RuntimeError("private-token " + private_flag)

checker.Checker.check = broken_check
sys.argv = ["checker.py", "check", "127.0.0.1"]
checker.run()
`
	program := exampleProgram(t, "hackerdom")
	command := exec.CommandContext(t.Context(), "python3", "-B", "-c", source, exampleFlag)
	command.Dir = filepath.Dir(program)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != int(processchecker.StatusCheckFailed) {
		t.Fatalf("unexpected checker failure must exit 110: %v", err)
	}
	if stdout.String() != "Checker failed\n" || stderr.String() != "checker.main - unexpected RuntimeError\n" {
		t.Fatal("unexpected error output contains traceback or private exception text")
	}
}

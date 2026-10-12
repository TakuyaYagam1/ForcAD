package checker_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type exampleNote struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Token string `json:"-"`
}

type exampleReply struct {
	method      string
	status      int
	body        string
	contentType string
	delay       time.Duration
}

type exampleService struct {
	mu      sync.Mutex
	notes   map[string]exampleNote
	reply   *exampleReply
	corrupt bool
	calls   int
}

func newExampleService(t *testing.T) (*exampleService, string) {
	t.Helper()
	service := &exampleService{notes: make(map[string]exampleNote)}
	server := httptest.NewServer(http.HandlerFunc(service.serveHTTP))
	t.Cleanup(server.Close)
	return service, strings.TrimPrefix(server.URL, "http://")
}

func (service *exampleService) serveHTTP(w http.ResponseWriter, r *http.Request) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.calls++
	w.Header().Set("Content-Type", "application/json")
	if reply := service.reply; reply != nil && reply.method == r.Method {
		if reply.delay > 0 {
			time.Sleep(reply.delay)
		}
		if reply.contentType != "" {
			w.Header().Set("Content-Type", reply.contentType)
		}
		w.Header().Set("Location", "/redirected")
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/notes/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/notes/")
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodPut {
		service.create(w, r, id, token)
		return
	}
	note, exists := service.notes[id]
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if note.Token != token {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if service.corrupt {
			note.Title, note.Body = "changed", "changed"
		}
		_ = json.NewEncoder(w).Encode(note)
	case http.MethodDelete:
		delete(service.notes, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (service *exampleService) create(w http.ResponseWriter, r *http.Request, id, token string) {
	var note exampleNote
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&note); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if _, exists := service.notes[id]; exists {
		w.WriteHeader(http.StatusConflict)
		return
	}
	note.ID, note.Token = id, token
	service.notes[id] = note
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (service *exampleService) setReply(reply exampleReply) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.reply = &reply
}

func (service *exampleService) changeStoredNote() {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.corrupt = true
}

func (service *exampleService) counts() (int, int) {
	service.mu.Lock()
	defer service.mu.Unlock()
	return len(service.notes), service.calls
}

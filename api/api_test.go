package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"potatoeye-cutseed-sprout-gate/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewServer(st)
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health status = %d; want 200", rr.Code)
	}
}

func TestLockAndGetTask(t *testing.T) {
	srv := newTestServer(t)
	body := `{"operation_id":"op-lock-1","plot":"P-01","variety":"V-G2","cellar":"C-01","disinfect":"D-01",` +
		`"batch":"B-1","basket_seals":["seal-1","seal-2"],"blind_codes":["code-1","code-2"],` +
		`"healing_shed":"shed-1","probe_window":"probe-1","sprout_slot":"slot-1",` +
		`"test_wells":["well-1"],"points":["p-1","p-2"],"reviewers":["reviewer-1","reviewer-2"]}`
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/tasks/lock", strings.NewReader(body)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("lock status = %d; want 201: %s", rr.Code, rr.Body.String())
	}
	var lock store.LockResult
	if err := json.NewDecoder(rr.Body).Decode(&lock); err != nil {
		t.Fatalf("decode lock: %v", err)
	}
	if lock.TaskID == "" || lock.Generation != 1 {
		t.Fatalf("lock result = %+v; want task id and generation 1", lock)
	}

	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/tasks/"+string(lock.TaskID), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get task status = %d; want 200", rr.Code)
	}
	var detail store.TaskDetail
	if err := json.NewDecoder(rr.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Task.ID != lock.TaskID {
		t.Fatalf("detail task id = %q; want %q", detail.Task.ID, lock.TaskID)
	}
}

func TestGetMissingTask(t *testing.T) {
	srv := newTestServer(t)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/tasks/missing", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing task status = %d; want 404", rr.Code)
	}
	var e ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&e); err != nil || e.Code != store.CodeNotFound {
		t.Fatalf("error = %+v, %v", e, err)
	}
}

func TestLockCatalogMismatch(t *testing.T) {
	srv := newTestServer(t)
	body := `{"operation_id":"op-lock-2","plot":"P-01","variety":"V-G9","cellar":"C-01","disinfect":"D-01",` +
		`"batch":"B-2","basket_seals":["seal-1"],"blind_codes":["code-1"],` +
		`"healing_shed":"shed-1","probe_window":"probe-1","sprout_slot":"slot-1",` +
		`"test_wells":["well-1"],"points":["p-1"],"reviewers":["reviewer-1"]}`
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/tasks/lock", strings.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("mismatch status = %d; want 400: %s", rr.Code, rr.Body.String())
	}
}
